package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

// CORSMiddleware handles CORS for all requests.
// frontendURL may contain multiple comma-separated origins (e.g. "http://localhost:3000,http://localhost:3300").
// The middleware matches the request Origin against allowed origins and reflects it back.
func CORSMiddleware(frontendURL string) fiber.Handler {
	allowed := make(map[string]bool)
	for _, u := range strings.Split(frontendURL, ",") {
		u = strings.TrimSpace(u)
		if u != "" {
			allowed[u] = true
		}
	}

	return func(c *fiber.Ctx) error {
		origin := c.Get("Origin")

		// Only set CORS headers for allowed origins
		if !allowed[origin] {
			// No CORS headers for disallowed origins — browser will block the response
			if c.Method() == fiber.MethodOptions {
				return c.SendStatus(fiber.StatusNoContent)
			}
			return c.Next()
		}

		c.Set("Access-Control-Allow-Origin", origin)
		c.Set("Access-Control-Allow-Credentials", "true")
		c.Set("Vary", "Origin")

		// Preflight: add extra headers and return 204
		if c.Method() == fiber.MethodOptions {
			c.Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
			c.Set("Access-Control-Allow-Headers", "Origin,Content-Type,Authorization,Accept,X-Requested-With")
			c.Set("Access-Control-Max-Age", "86400")
			return c.SendStatus(fiber.StatusNoContent)
		}

		return c.Next()
	}
}
