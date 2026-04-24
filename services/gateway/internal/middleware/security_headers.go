// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

// SecurityHeadersMiddleware adds standard security headers to every response.
// CSP is only applied to API routes — frontend pages use a <meta> CSP tag
// managed by webpack, avoiding duplicate/conflicting policies.
func SecurityHeadersMiddleware() fiber.Handler {
	const apiCSP = "default-src 'none'; " +
		"frame-ancestors 'none'; " +
		"base-uri 'none'"

	return func(c *fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Set("X-XSS-Protection", "0") // Disabled per modern best practice; CSP is preferred
		c.Set("Permissions-Policy", "camera=(self), microphone=(self), geolocation=()")

		// Only set CSP header for API responses — frontend CSP lives in <meta> tag.
		if strings.HasPrefix(c.Path(), "/api/") {
			c.Set("Content-Security-Policy", apiCSP)
		}

		return c.Next()
	}
}
