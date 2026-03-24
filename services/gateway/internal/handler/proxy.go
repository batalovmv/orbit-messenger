package handler

import (
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/proxy"
	"github.com/valyala/fasthttp"
)

type ProxyConfig struct {
	AuthServiceURL      string
	MessagingServiceURL string
}

// SetupProxy configures reverse proxy routes.
func SetupProxy(app *fiber.App, authGroup fiber.Router, apiGroup fiber.Router, cfg ProxyConfig) {
	authClient := &fasthttp.Client{
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	msgClient := &fasthttp.Client{
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	// Auth routes: proxy without JWT validation
	authGroup.All("/*", func(c *fiber.Ctx) error {
		url := cfg.AuthServiceURL + c.Path()
		if q := c.Request().URI().QueryString(); len(q) > 0 {
			url += "?" + string(q)
		}
		if err := proxy.DoRedirects(c, url, 0, authClient); err != nil {
			slog.Error("auth proxy error", "error", err, "url", url)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error": "service_unavailable", "message": "Auth service unavailable", "status": 502,
			})
		}
		return nil
	})

	// Messaging routes: proxy with JWT already validated by middleware
	apiGroup.All("/*", func(c *fiber.Ctx) error {
		// Strip /api/v1 prefix for downstream
		path := strings.TrimPrefix(c.Path(), "/api/v1")
		if path == "" {
			path = "/"
		}
		url := cfg.MessagingServiceURL + path
		if q := c.Request().URI().QueryString(); len(q) > 0 {
			url += "?" + string(q)
		}
		if err := proxy.DoRedirects(c, url, 0, msgClient); err != nil {
			slog.Error("messaging proxy error", "error", err, "url", url)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error": "service_unavailable", "message": "Messaging service unavailable", "status": 502,
			})
		}
		return nil
	})
}
