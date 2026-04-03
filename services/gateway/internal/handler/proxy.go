package handler

import (
	"log/slog"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
)

// validInviteHash matches alphanumeric invite hashes (8-64 chars).
var validInviteHash = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,64}$`)

// sanitizeProxyPath strips the API prefix and cleans the path to prevent traversal.
func sanitizeProxyPath(rawPath string) string {
	p := strings.TrimPrefix(rawPath, "/api/v1")
	p = path.Clean(p)
	if p == "." || p == "" {
		return "/"
	}
	return p
}

type ProxyConfig struct {
	AuthServiceURL      string
	MessagingServiceURL string
	MediaServiceURL     string
	FrontendURL         string
	InternalSecret      string
}

type AuthProxyMiddlewares struct {
	Sensitive        fiber.Handler
	InviteValidation fiber.Handler
	Session          fiber.Handler
}

// doProxy performs a manual reverse proxy: sends request to upstream, copies
// status + body + safe headers back, then sets CORS headers from gateway config.
// This avoids proxy.DoRedirects which overwrites the entire fasthttp raw response
// buffer, making it impossible to reliably set CORS headers afterwards.
func doProxy(c *fiber.Ctx, url string, client *fasthttp.Client, frontendURL string, internalSecret ...string) error {
	// Build upstream request
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	// Copy method, headers, body from original request
	req.SetRequestURI(url)
	req.Header.SetMethod(c.Method())
	c.Request().Header.VisitAll(func(key, value []byte) {
		k := string(key)
		// Skip hop-by-hop and internal headers (prevent client impersonation)
		switch strings.ToLower(k) {
		case "connection", "keep-alive", "transfer-encoding", "te",
			"trailer", "upgrade", "proxy-authorization", "proxy-authenticate",
			"x-user-id", "x-user-role", "x-internal-token":
			return
		}
		req.Header.SetBytesKV(key, value)
	})
	// Re-add X-User-ID/X-User-Role set by JWT middleware (after stripping client-supplied values)
	if uid := c.Get("X-User-ID"); uid != "" {
		req.Header.Set("X-User-ID", uid)
	}
	if role := c.Get("X-User-Role"); role != "" {
		req.Header.Set("X-User-Role", role)
	}
	// Sign the request so downstream services can verify it came from the gateway
	if len(internalSecret) > 0 && internalSecret[0] != "" {
		req.Header.Set("X-Internal-Token", internalSecret[0])
	}
	// Forward request body
	if body := c.Body(); len(body) > 0 {
		req.SetBody(body)
	}

	// Execute request to upstream
	if err := client.Do(req, resp); err != nil {
		return err
	}

	// Copy status code
	c.Status(resp.StatusCode())

	// Copy safe response headers from upstream (content-related only)
	resp.Header.VisitAll(func(key, value []byte) {
		k := strings.ToLower(string(key))
		switch k {
		case "content-type", "content-disposition", "content-encoding",
			"cache-control", "etag", "last-modified", "x-request-id",
			"set-cookie", "location":
			c.Response().Header.AddBytesKV(key, value)
		}
		// Skip CORS headers from upstream — gateway owns CORS
	})

	// CORS headers are handled by CORSMiddleware — do not override here

	// Copy response body
	c.Response().SetBody(resp.Body())

	return nil
}

// PublicInviteProxy returns a handler that proxies invite info requests without JWT.
func PublicInviteProxy(messagingURL, frontendURL string) fiber.Handler {
	client := &fasthttp.Client{ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	return func(c *fiber.Ctx) error {
		hash := c.Params("hash")
		if !validInviteHash.MatchString(hash) {
			return response.Error(c, apperror.BadRequest("Invalid invite hash"))
		}
		url := messagingURL + "/chats/invite/" + hash
		if err := doProxy(c, url, client, frontendURL); err != nil {
			slog.Error("invite proxy error", "error", err, "url", url)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error": "service_unavailable", "message": "Messaging service unavailable", "status": 502,
			})
		}
		return nil
	}
}

// AuthProxyHandler returns a handler that proxies auth routes to the auth service.
func AuthProxyHandler(cfg ProxyConfig) fiber.Handler {
	client := &fasthttp.Client{
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return func(c *fiber.Ctx) error {
		url := cfg.AuthServiceURL + sanitizeProxyPath(c.Path())
		if q := c.Request().URI().QueryString(); len(q) > 0 {
			url += "?" + string(q)
		}
		if err := doProxy(c, url, client, cfg.FrontendURL, cfg.InternalSecret); err != nil {
			slog.Error("auth proxy error", "error", err, "url", url)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error": "service_unavailable", "message": "Auth service unavailable", "status": 502,
			})
		}
		return nil
	}
}

func routeHandlers(proxy fiber.Handler, middlewares ...fiber.Handler) []fiber.Handler {
	handlers := make([]fiber.Handler, 0, len(middlewares)+1)
	for _, middleware := range middlewares {
		if middleware != nil {
			handlers = append(handlers, middleware)
		}
	}
	handlers = append(handlers, proxy)
	return handlers
}

// RegisterAuthProxyRoutes configures auth proxy routes with per-endpoint middleware chains.
func RegisterAuthProxyRoutes(authGroup fiber.Router, cfg ProxyConfig, middlewares AuthProxyMiddlewares) {
	authProxy := AuthProxyHandler(cfg)

	authGroup.Post("/bootstrap", routeHandlers(authProxy, middlewares.Sensitive)...)
	authGroup.Post("/register", routeHandlers(authProxy, middlewares.Sensitive)...)
	authGroup.Post("/login", routeHandlers(authProxy, middlewares.Sensitive)...)
	authGroup.Post("/reset-admin", routeHandlers(authProxy, middlewares.Sensitive)...)
	authGroup.Post("/invite/validate", routeHandlers(authProxy, middlewares.InviteValidation)...)

	authGroup.Post("/refresh", routeHandlers(authProxy, middlewares.Session)...)
	authGroup.Get("/me", routeHandlers(authProxy, middlewares.Session)...)
	authGroup.Post("/logout", routeHandlers(authProxy, middlewares.Session)...)
	authGroup.Get("/sessions", routeHandlers(authProxy, middlewares.Session)...)
	authGroup.Delete("/sessions/:id", routeHandlers(authProxy, middlewares.Session)...)
	authGroup.Post("/2fa/setup", routeHandlers(authProxy, middlewares.Session)...)
	authGroup.Post("/2fa/verify", routeHandlers(authProxy, middlewares.Session)...)
	authGroup.Post("/2fa/disable", routeHandlers(authProxy, middlewares.Session)...)
	authGroup.Post("/invites", routeHandlers(authProxy, middlewares.Session)...)
	authGroup.Get("/invites", routeHandlers(authProxy, middlewares.Session)...)
	authGroup.Delete("/invites/:id", routeHandlers(authProxy, middlewares.Session)...)

	authGroup.All("/*", routeHandlers(authProxy, middlewares.Sensitive)...)
}

// PublicMediaProxy returns a handler that proxies media GET requests without JWT.
// Media service streams files directly from S3 (no redirects), so standard proxy works.
func PublicMediaProxy(mediaURL, frontendURL string) fiber.Handler {
	client := &fasthttp.Client{
		ReadTimeout:         120 * time.Second,
		WriteTimeout:        120 * time.Second,
		MaxResponseBodySize: 100 * 1024 * 1024, // 100MB
	}
	return func(c *fiber.Ctx) error {
		url := mediaURL + sanitizeProxyPath(c.Path())
		if err := doProxy(c, url, client, frontendURL); err != nil {
			slog.Error("media public proxy error", "error", err, "url", url)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error": "service_unavailable", "message": "Media service unavailable", "status": 502,
			})
		}
		return nil
	}
}

// SetupProxy configures reverse proxy routes.
func SetupProxy(app *fiber.App, apiGroup fiber.Router, cfg ProxyConfig) {
	msgClient := &fasthttp.Client{
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	// Media routes: proxy with JWT, higher timeouts for uploads
	mediaClient := &fasthttp.Client{
		ReadTimeout:         120 * time.Second,
		WriteTimeout:        120 * time.Second,
		MaxResponseBodySize: 100 * 1024 * 1024, // 100MB response (for large file info etc)
	}
	apiGroup.All("/media/*", func(c *fiber.Ctx) error {
		url := cfg.MediaServiceURL + sanitizeProxyPath(c.Path())
		if q := c.Request().URI().QueryString(); len(q) > 0 {
			url += "?" + string(q)
		}
		if err := doProxy(c, url, mediaClient, cfg.FrontendURL, cfg.InternalSecret); err != nil {
			slog.Error("media proxy error", "error", err, "url", url)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error": "service_unavailable", "message": "Media service unavailable", "status": 502,
			})
		}
		return nil
	})

	// Messaging routes: proxy with JWT already validated by middleware
	apiGroup.All("/*", func(c *fiber.Ctx) error {
		url := cfg.MessagingServiceURL + sanitizeProxyPath(c.Path())
		if q := c.Request().URI().QueryString(); len(q) > 0 {
			url += "?" + string(q)
		}
		if err := doProxy(c, url, msgClient, cfg.FrontendURL, cfg.InternalSecret); err != nil {
			slog.Error("messaging proxy error", "error", err, "url", url)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error": "service_unavailable", "message": "Messaging service unavailable", "status": 502,
			})
		}
		return nil
	})
}
