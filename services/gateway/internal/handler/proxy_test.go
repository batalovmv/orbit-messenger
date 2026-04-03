package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

type proxyTestResponse struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

func TestRegisterAuthProxyRoutes_UsesExpectedMiddlewareBuckets(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(proxyTestResponse{
			Method: r.Method,
			Path:   r.URL.Path,
		})
	}))
	defer upstream.Close()

	app := fiber.New()
	RegisterAuthProxyRoutes(app.Group("/api/v1/auth"), ProxyConfig{
		AuthServiceURL: upstream.URL,
		FrontendURL:    "http://localhost:3000",
	}, AuthProxyMiddlewares{
		Sensitive: func(c *fiber.Ctx) error {
			c.Set("X-Auth-Bucket", "sensitive")
			return c.Next()
		},
		InviteValidation: func(c *fiber.Ctx) error {
			c.Set("X-Auth-Bucket", "invite-validation")
			return c.Next()
		},
		Session: func(c *fiber.Ctx) error {
			c.Set("X-Auth-Bucket", "session")
			return c.Next()
		},
	})

	tests := []struct {
		name           string
		method         string
		path           string
		expectedBucket string
		expectedPath   string
	}{
		{
			name:           "login uses sensitive bucket",
			method:         http.MethodPost,
			path:           "/api/v1/auth/login",
			expectedBucket: "sensitive",
			expectedPath:   "/auth/login",
		},
		{
			name:           "invite validate uses dedicated bucket",
			method:         http.MethodPost,
			path:           "/api/v1/auth/invite/validate",
			expectedBucket: "invite-validation",
			expectedPath:   "/auth/invite/validate",
		},
		{
			name:           "me uses session bucket",
			method:         http.MethodGet,
			path:           "/api/v1/auth/me",
			expectedBucket: "session",
			expectedPath:   "/auth/me",
		},
		{
			name:           "refresh uses session bucket",
			method:         http.MethodPost,
			path:           "/api/v1/auth/refresh",
			expectedBucket: "session",
			expectedPath:   "/auth/refresh",
		},
		{
			name:           "fallback uses sensitive bucket",
			method:         http.MethodGet,
			path:           "/api/v1/auth/unknown",
			expectedBucket: "sensitive",
			expectedPath:   "/auth/unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)

			resp, err := app.Test(req, -1)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}

			if got := resp.Header.Get("X-Auth-Bucket"); got != tt.expectedBucket {
				t.Fatalf("expected X-Auth-Bucket %q, got %q", tt.expectedBucket, got)
			}

			var payload proxyTestResponse
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			if payload.Method != tt.method {
				t.Fatalf("expected method %q, got %q", tt.method, payload.Method)
			}

			if payload.Path != tt.expectedPath {
				t.Fatalf("expected upstream path %q, got %q", tt.expectedPath, payload.Path)
			}
		})
	}
}
