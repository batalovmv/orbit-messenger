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

// getUserSessionID returns the caller's JWT jti as injected by the gateway's
// JWT middleware (X-User-Session-ID header). Returns "" when the header is
// absent — older clients/JWTs without a jti claim. Callers that depend on
// the value (e.g. own-current-session guard) must treat empty as "do not
// match" rather than as "matches everything".
func getUserSessionID(c *fiber.Ctx) string {
	return strings.TrimSpace(c.Get("X-User-Session-ID"))
}

// checkSysPermission validates that the current request has the given system permission.
// Returns nil on success, *apperror.AppError on failure. It is a pure validator — it does
// NOT call c.Next(), so callers must use it inline inside a handler, not as middleware.
func checkSysPermission(c *fiber.Ctx, perm int64) error {
	if _, err := getUserID(c); err != nil {
		return err
	}
	if !permissions.HasSysPermission(getUserRole(c), perm) {
		return apperror.Forbidden("Insufficient permissions")
	}
	return nil
}

// requireSysPermission returns a Fiber middleware that enforces a system permission.
// Use it when mounting on routes/groups: `app.Post("/x", requireSysPermission(perm), handler)`.
func requireSysPermission(perm int64) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if err := checkSysPermission(c, perm); err != nil {
			return err
		}
		return c.Next()
	}
}

// requireAdminRole checks that the caller has at least admin-level content management
// permissions. It is a pure validator intended for inline use inside handlers.
// Returns nil on success, *apperror.AppError on failure.
func requireAdminRole(c *fiber.Ctx) error {
	return checkSysPermission(c, permissions.SysManageContent)
}
