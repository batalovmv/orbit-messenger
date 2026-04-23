package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
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
				c.Locals("userID", u.ID) // for rate limiter — cannot be spoofed by client
				c.Request().Header.Set("X-User-ID", u.ID)
				c.Request().Header.Set("X-User-Role", u.Role)
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

		c.Locals("userID", user.ID) // for rate limiter — cannot be spoofed by client
		c.Request().Header.Set("X-User-ID", user.ID)
		c.Request().Header.Set("X-User-Role", user.Role)
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
