package middleware

import (
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

		// Check cache
		cached, err := cfg.Redis.Get(c.Context(), cacheKey).Result()
		if err == nil {
			var u cachedUser
			if json.Unmarshal([]byte(cached), &u) == nil {
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

		body, err := io.ReadAll(resp.Body)
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

		// Cache result
		cu := cachedUser{ID: user.ID, Role: user.Role}
		cuJSON, _ := json.Marshal(cu)
		cfg.Redis.Set(c.Context(), cacheKey, string(cuJSON), cfg.CacheTTL)

		c.Request().Header.Set("X-User-ID", user.ID)
		c.Request().Header.Set("X-User-Role", user.Role)
		return c.Next()
	}
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
