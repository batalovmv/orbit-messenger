package middleware

import "github.com/gofiber/fiber/v2"

// SecurityHeadersMiddleware adds standard security headers to every response.
func SecurityHeadersMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Set("X-XSS-Protection", "0") // Disabled per modern best practice; CSP is preferred
		c.Set("Permissions-Policy", "camera=(self), microphone=(self), geolocation=()")
		// CSP: restrict script/style sources; allow WebSocket connections; block object/embed.
		// connect-src 'self' wss: — allows API calls and WebSocket upgrades.
		// img-src 'self' blob: data: — allows inline thumbnails and blob URLs for media.
		c.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'wasm-unsafe-eval'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' blob: data: https:; "+
				"media-src 'self' blob: https:; "+
				"connect-src 'self' https: wss:; "+
				"font-src 'self'; "+
				"object-src 'none'; "+
				"base-uri 'self'; "+
				"frame-ancestors 'none'")
		return c.Next()
	}
}
