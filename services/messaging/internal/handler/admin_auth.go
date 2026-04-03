package handler

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/mst-corp/orbit/pkg/apperror"
)

func getUserRole(c *fiber.Ctx) string {
	return strings.ToLower(strings.TrimSpace(c.Get("X-User-Role")))
}

func requireAdminRole(c *fiber.Ctx) error {
	if _, err := getUserID(c); err != nil {
		return err
	}

	if getUserRole(c) != "admin" {
		return apperror.Forbidden("Admin role required")
	}

	return nil
}
