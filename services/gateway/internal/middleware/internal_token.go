package middleware

import (
	"crypto/subtle"

	"github.com/gofiber/fiber/v2"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
)

// RequireInternalToken gates a route behind the shared INTERNAL_SECRET.
// Intended for platform-scraper endpoints like /metrics that should never
// be reachable without the shared secret even though they do not accept
// user-level JWTs. When `secret` is empty the route is hard-blocked —
// an empty secret in production means misconfiguration, not "open
// access", and we fail closed by design (same posture as the rate
// limiter's Redis-error branch).
func RequireInternalToken(secret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if secret == "" {
			return response.Error(c, apperror.Unauthorized("Internal endpoint unavailable"))
		}
		got := c.Get("X-Internal-Token")
		if subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
			return response.Error(c, apperror.Unauthorized("Invalid internal token"))
		}
		return c.Next()
	}
}
