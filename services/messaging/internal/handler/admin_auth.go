package handler

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
)

func getUserRole(c *fiber.Ctx) string {
	return strings.ToLower(strings.TrimSpace(c.Get("X-User-Role")))
}

// requireSysPermission returns a middleware that checks for a specific system permission.
func requireSysPermission(perm int64) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if _, err := getUserID(c); err != nil {
			return err
		}
		if !permissions.HasSysPermission(getUserRole(c), perm) {
			return apperror.Forbidden("Insufficient permissions")
		}
		return c.Next()
	}
}

// requireAdminRole checks that the caller has at least admin-level content management permissions.
// Kept for backward compatibility — wraps requireSysPermission(SysManageContent).
func requireAdminRole(c *fiber.Ctx) error {
	return requireSysPermission(permissions.SysManageContent)(c)
}
