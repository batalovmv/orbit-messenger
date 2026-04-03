package handler

import (
	"crypto/subtle"

	"github.com/gofiber/fiber/v2"
	"github.com/mst-corp/orbit/pkg/apperror"
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
