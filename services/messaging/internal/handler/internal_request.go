package handler

import (
	"crypto/subtle"

	"github.com/gofiber/fiber/v2"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
)

func isInternalRequest(c *fiber.Ctx, secret string) bool {
	if secret == "" {
		return false
	}

	token := c.Get("X-Internal-Token")
	return token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(secret)) == 1
}

func requireInternalRequest(c *fiber.Ctx, secret string) error {
	if !isInternalRequest(c, secret) {
		return apperror.Forbidden("Internal access only")
	}

	return nil
}

// RequireInternalToken returns a Fiber middleware that validates X-Internal-Token
// on every request. X-User-ID is only trusted when the token is valid.
// This prevents identity spoofing if the service is reached outside the gateway.
func RequireInternalToken(secret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Get("X-User-ID")
		if userID == "" {
			return response.Error(c, apperror.Unauthorized("Missing user context"))
		}
		token := c.Get("X-Internal-Token")
		if secret == "" || token == "" ||
			subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
			return response.Error(c, apperror.Unauthorized("Invalid internal token"))
		}
		return c.Next()
	}
}
