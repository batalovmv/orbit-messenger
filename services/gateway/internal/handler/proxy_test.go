// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gofiber/fiber/v2"
)

type proxyTestResponse struct {
	Method          string `json:"method"`
	Path            string `json:"path"`
	TrustedClientIP string `json:"trusted_client_ip,omitempty"`
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
			name:           "notification priority get uses session bucket",
			method:         http.MethodGet,
			path:           "/api/v1/auth/users/me/notification-priority",
			expectedBucket: "session",
			expectedPath:   "/auth/users/me/notification-priority",
		},
		{
			name:           "admin session list uses session bucket",
			method:         http.MethodGet,
			path:           "/api/v1/auth/admin/users/11111111-1111-1111-1111-111111111111/sessions",
			expectedBucket: "session",
			expectedPath:   "/auth/admin/users/11111111-1111-1111-1111-111111111111/sessions",
		},
		{
			name:           "admin session revoke uses session bucket",
			method:         http.MethodDelete,
			path:           "/api/v1/auth/admin/users/11111111-1111-1111-1111-111111111111/sessions/22222222-2222-2222-2222-222222222222",
			expectedBucket: "session",
			expectedPath:   "/auth/admin/users/11111111-1111-1111-1111-111111111111/sessions/22222222-2222-2222-2222-222222222222",
		},
		{
			name:           "admin all sessions revoke uses session bucket",
			method:         http.MethodDelete,
			path:           "/api/v1/auth/admin/users/11111111-1111-1111-1111-111111111111/sessions",
			expectedBucket: "session",
			expectedPath:   "/auth/admin/users/11111111-1111-1111-1111-111111111111/sessions",
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

func TestSetupProxy_ForwardsTrustedClientIPToMedia(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(proxyTestResponse{
			Method:          r.Method,
			Path:            r.URL.Path,
			TrustedClientIP: r.Header.Get("X-Trusted-Client-IP"),
		})
	}))
	defer upstream.Close()

	app := fiber.New()
	apiGroup := app.Group("/api/v1")
	SetupProxy(app, apiGroup, ProxyConfig{
		MediaServiceURL: upstream.URL,
		FrontendURL:     "http://localhost:3000",
		InternalSecret:  "secret",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/media/test", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var payload proxyTestResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.TrustedClientIP == "" {
		t.Fatal("expected trusted client ip header to be forwarded")
	}
}

func TestSetupProxy_BlocksInternalPathsAfterSanitization(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(proxyTestResponse{
			Method: r.Method,
			Path:   r.URL.Path,
		})
	}))
	defer upstream.Close()

	app := fiber.New()
	apiGroup := app.Group("/api/v1")
	SetupProxy(app, apiGroup, ProxyConfig{
		MessagingServiceURL: upstream.URL,
		MediaServiceURL:     upstream.URL,
		CallsServiceURL:     upstream.URL,
		FrontendURL:         "http://localhost:3000",
		InternalSecret:      "secret",
	})

	tests := []struct {
		name         string
		path         string
		wantStatus   int
		wantUpstream int32
		wantPath     string
	}{
		{
			name:         "dot segment breakout blocked",
			path:         "/api/v1/chats/../../internal/foo",
			wantStatus:   http.StatusNotFound,
			wantUpstream: 0,
		},
		{
			name:         "direct internal path blocked",
			path:         "/api/v1/internal/foo",
			wantStatus:   http.StatusNotFound,
			wantUpstream: 0,
		},
		{
			name:         "legit path proxied",
			path:         "/api/v1/chats/legit",
			wantStatus:   http.StatusOK,
			wantUpstream: 1,
			wantPath:     "/chats/legit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits.Store(0)
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)

			resp, err := app.Test(req, -1)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}

			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}
			if got := hits.Load(); got != tt.wantUpstream {
				t.Fatalf("expected upstream hits %d, got %d", tt.wantUpstream, got)
			}

			if tt.wantPath == "" {
				return
			}

			var payload proxyTestResponse
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if payload.Path != tt.wantPath {
				t.Fatalf("expected upstream path %q, got %q", tt.wantPath, payload.Path)
			}
		})
	}
}
