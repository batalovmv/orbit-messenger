package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
)

type JWTConfig struct {
	AuthServiceURL string
	Redis          *redis.Client
	CacheTTL       time.Duration
}

type cachedUser struct {
	ID   string `json:"id"`
	Role string `json:"role"`
}

// extractJTI parses the JWT claims WITHOUT verifying the signature. The token
// has already been validated upstream (Redis blacklist + auth /me); this is
// purely a metadata read for downstream propagation. Returns "" on any
// parse error so callers can keep the token alive without jti.
func extractJTI(token string) string {
	parser := jwt.NewParser()
	parsed, _, err := parser.ParseUnverified(token, jwt.MapClaims{})
	if err != nil {
		return ""
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return ""
	}
	jti, _ := claims["jti"].(string)
	return jti
}

// extractIAT parses the JWT iat (issued-at) claim without verifying the
// signature. Returns 0 + false on any parse error or missing claim. Callers
// must treat 0 as "older than any threshold" — same semantics as
// auth.ValidateAccessToken which uses int64(iat) <= threshold and so
// rejects tokens with missing iat against a non-zero threshold.
func extractIAT(token string) (int64, bool) {
	parser := jwt.NewParser()
	parsed, _, err := parser.ParseUnverified(token, jwt.MapClaims{})
	if err != nil {
		return 0, false
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return 0, false
	}
	iat, ok := claims["iat"].(float64)
	if !ok {
		return 0, false
	}
	return int64(iat), true
}

// checkTokenInvalidatedByReset reports whether the token's iat is older than
// the per-user "tokens invalid before" threshold written by auth.ResetAdmin
// (and any future flow that wants to revoke ALL access tokens of a user).
//
// auth.ValidateAccessToken already enforces this on every /auth/me call, but
// the gateway JWT cache (CacheTTL = 30s) short-circuits cache-hits without
// round-tripping to auth — so a cache entry created seconds before a password
// reset would keep authenticating for the rest of the TTL window. Mirroring
// the per-jti blacklist pattern (Day 5.2), this check runs alongside on every
// gateway auth path: cache-hit, cache-miss, and the WS revalidation tick.
//
// Fail-closed on Redis error. Unparseable threshold value → fail-open (treat
// as no-op) to avoid bricking auth on a typo'd Redis key, mirroring auth's
// own Sscanf-ignores-error behaviour. Missing JWT iat → reject (same as auth).
func checkTokenInvalidatedByReset(ctx context.Context, rdb *redis.Client, userID, token string) (bool, error) {
	invalidateKey := "user_tokens_invalid_before:" + userID
	val, err := rdb.Get(ctx, invalidateKey).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	var threshold int64
	if _, scanErr := fmt.Sscanf(val, "%d", &threshold); scanErr != nil {
		return false, nil
	}
	iat, _ := extractIAT(token)
	return iat <= threshold, nil
}

// JWTMiddleware validates JWT by calling the auth service /auth/me and caching the result.
func JWTMiddleware(cfg JWTConfig) fiber.Handler {
	client := &http.Client{Timeout: 10 * time.Second}

	return func(c *fiber.Ctx) error {
		// Skip JWT validation for auth routes (login, register, bootstrap, etc.)
		if strings.HasPrefix(c.Path(), "/api/v1/auth") {
			return c.Next()
		}

		token := extractBearerToken(c)
		if token == "" {
			return response.Error(c, apperror.Unauthorized("Missing authorization"))
		}

		tokenHash := hashToken(token)
		cacheKey := "jwt_cache:" + tokenHash
		blacklistKey := "jwt_blacklist:" + tokenHash

		// Check blacklist first (logout invalidation) — fail-closed on Redis error
		blacklisted, blErr := cfg.Redis.Exists(c.Context(), blacklistKey).Result()
		if blErr != nil {
			slog.Error("JWT blacklist Redis check failed, rejecting token", "error", blErr)
			return response.Error(c, apperror.Internal("Token validation temporarily unavailable"))
		}
		if blacklisted > 0 {
			if err := cfg.Redis.Del(c.Context(), cacheKey).Err(); err != nil {
				slog.Error("JWT cache del failed after blacklist hit", "error", err)
			}
			return response.Error(c, apperror.Unauthorized("Token revoked"))
		}

		// Per-jti blacklist: written by auth on admin session revoke. Must be
		// checked alongside the per-token-hash blacklist because the cache
		// short-circuit below would otherwise honour a revoked session for
		// the full CacheTTL window. Fail-closed on Redis error.
		if jti := extractJTI(token); jti != "" {
			jtiKey := "jwt_blacklist:jti:" + jti
			jtiBlacklisted, jtiErr := cfg.Redis.Exists(c.Context(), jtiKey).Result()
			if jtiErr != nil {
				slog.Error("JWT jti blacklist Redis check failed, rejecting token", "error", jtiErr)
				return response.Error(c, apperror.Internal("Token validation temporarily unavailable"))
			}
			if jtiBlacklisted > 0 {
				if err := cfg.Redis.Del(c.Context(), cacheKey).Err(); err != nil {
					slog.Error("JWT cache del failed after jti blacklist hit", "error", err)
				}
				return response.Error(c, apperror.Unauthorized("Session revoked"))
			}
		}

		// Check cache
		cached, err := cfg.Redis.Get(c.Context(), cacheKey).Result()
		if err == nil {
			var u cachedUser
			if json.Unmarshal([]byte(cached), &u) == nil {
				// Check per-user blacklist (deactivated account) — fail-closed
				blacklisted, blUserErr := checkUserBlacklist(c.Context(), cfg.Redis, u.ID)
				if blUserErr != nil {
					slog.Error("JWT user blacklist Redis check failed, rejecting token", "error", blUserErr)
					return response.Error(c, apperror.Internal("Token validation temporarily unavailable"))
				}
				if blacklisted {
					if err := cfg.Redis.Del(c.Context(), cacheKey).Err(); err != nil {
						slog.Error("JWT cache del failed after user blacklist hit", "error", err)
					}
					return response.Error(c, apperror.Unauthorized("Account deactivated"))
				}
				// Per-user tokens-invalid-before threshold (set by auth.ResetAdmin).
				// Mirrors the per-jti pattern: gateway cache otherwise honours a
				// stale token for up to CacheTTL after password reset.
				resetInvalid, riErr := checkTokenInvalidatedByReset(c.Context(), cfg.Redis, u.ID, token)
				if riErr != nil {
					slog.Error("JWT tokens-invalid-before Redis check failed, rejecting token", "error", riErr)
					return response.Error(c, apperror.Internal("Token validation temporarily unavailable"))
				}
				if resetInvalid {
					if err := cfg.Redis.Del(c.Context(), cacheKey).Err(); err != nil {
						slog.Error("JWT cache del failed after tokens-invalid-before hit", "error", err)
					}
					return response.Error(c, apperror.Unauthorized("Token has been revoked"))
				}
				c.Locals("userID", u.ID) // for rate limiter — cannot be spoofed by client
				c.Request().Header.Set("X-User-ID", u.ID)
				c.Request().Header.Set("X-User-Role", u.Role)
				// Re-extract jti from the raw token rather than reading it
				// from the cache record. The WS auth path shares this same
				// jwt_cache:* key but writes a {id, role}-only shape — so a
				// cache record populated by a WS connection would arrive
				// here with empty JTI. Local parse is cheap and authoritative.
				if jti := extractJTI(token); jti != "" {
					c.Request().Header.Set("X-User-Session-ID", jti)
				}
				return c.Next()
			}
		}

		// Call auth service
		req, err := http.NewRequestWithContext(c.Context(), "GET", cfg.AuthServiceURL+"/auth/me", nil)
		if err != nil {
			slog.Error("failed to create auth request", "error", err)
			return response.Error(c, apperror.Internal("auth service error"))
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		if err != nil {
			slog.Error("auth service unreachable", "error", err)
			return response.Error(c, apperror.Internal("auth service unavailable"))
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return response.Error(c, apperror.Unauthorized("Invalid or expired token"))
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if err != nil {
			return response.Error(c, apperror.Internal("auth response read error"))
		}

		var user struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		}
		if err := json.Unmarshal(body, &user); err != nil {
			return response.Error(c, apperror.Internal("auth response parse error"))
		}

		// Cache result (fail-open: if Redis write fails, the next request will re-validate via auth service.
		// This is safe because blacklist checks above are fail-closed.)
		// JTI deliberately omitted from the cache record — WS auth shares the
		// same cache key with a different writer; cache-hit consumers must
		// re-derive jti locally from the raw token.
		cu := cachedUser{ID: user.ID, Role: user.Role}
		cuJSON, _ := json.Marshal(cu)
		if err := cfg.Redis.Set(c.Context(), cacheKey, string(cuJSON), cfg.CacheTTL).Err(); err != nil {
			slog.Error("JWT cache write failed", "error", err)
		}

		// Check per-user blacklist (deactivated account) — fail-closed
		userBlacklisted, blUserErr := checkUserBlacklist(c.Context(), cfg.Redis, user.ID)
		if blUserErr != nil {
			slog.Error("JWT user blacklist Redis check failed, rejecting token", "error", blUserErr)
			return response.Error(c, apperror.Internal("Token validation temporarily unavailable"))
		}
		if userBlacklisted {
			if err := cfg.Redis.Del(c.Context(), cacheKey).Err(); err != nil {
				slog.Error("JWT cache del failed after user blacklist hit", "error", err)
			}
			return response.Error(c, apperror.Unauthorized("Account deactivated"))
		}

		// Per-user tokens-invalid-before threshold (set by auth.ResetAdmin).
		// Defends against the race where ResetAdmin fires between our /auth/me
		// call and the cache write — without this, the just-written cache entry
		// would happily serve the now-revoked token until CacheTTL expires.
		resetInvalid, riErr := checkTokenInvalidatedByReset(c.Context(), cfg.Redis, user.ID, token)
		if riErr != nil {
			slog.Error("JWT tokens-invalid-before Redis check failed, rejecting token", "error", riErr)
			return response.Error(c, apperror.Internal("Token validation temporarily unavailable"))
		}
		if resetInvalid {
			if err := cfg.Redis.Del(c.Context(), cacheKey).Err(); err != nil {
				slog.Error("JWT cache del failed after tokens-invalid-before hit", "error", err)
			}
			return response.Error(c, apperror.Unauthorized("Token has been revoked"))
		}

		c.Locals("userID", user.ID) // for rate limiter — cannot be spoofed by client
		c.Request().Header.Set("X-User-ID", user.ID)
		c.Request().Header.Set("X-User-Role", user.Role)
		if jti := extractJTI(token); jti != "" {
			c.Request().Header.Set("X-User-Session-ID", jti)
		}
		return c.Next()
	}
}

// checkUserBlacklist checks if the user has been globally blacklisted (e.g. deactivated).
// Returns true if blacklisted. Fail-closed: Redis error = treat as blacklisted.
func checkUserBlacklist(ctx context.Context, rdb *redis.Client, userID string) (bool, error) {
	key := "jwt_blacklist:user:" + userID
	exists, err := rdb.Exists(ctx, key).Result()
	if err != nil {
		return true, err
	}
	return exists > 0, nil
}

func extractBearerToken(c *fiber.Ctx) string {
	auth := c.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return auth[7:]
	}
	return ""
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
