// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func newAppVersionApp(t *testing.T, cfg AppVersionConfig) *fiber.App {
	t.Helper()
	app := fiber.New()
	app.Use(AppVersionMiddleware(cfg))
	app.Get("/api/v1/test", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	app.Get("/api/v1/auth/refresh", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	app.Get("/api/v1/public/system/config", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	return app
}

func TestAppVersionMiddleware_LatestHeader_AlwaysEchoed(t *testing.T) {
	app := newAppVersionApp(t, AppVersionConfig{LatestVersion: "12.0.21"})
	req := httptest.NewRequest(fiber.MethodGet, "/api/v1/test", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	if got := resp.Header.Get("X-App-Latest-Version"); got != "12.0.21" {
		t.Fatalf("expected X-App-Latest-Version=12.0.21, got %q", got)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAppVersionMiddleware_LatestHeader_Empty_NoOp(t *testing.T) {
	app := newAppVersionApp(t, AppVersionConfig{})
	req := httptest.NewRequest(fiber.MethodGet, "/api/v1/test", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	if got := resp.Header.Get("X-App-Latest-Version"); got != "" {
		t.Fatalf("expected empty X-App-Latest-Version, got %q", got)
	}
}

func TestAppVersionMiddleware_MinVersion_RejectsTooOld(t *testing.T) {
	app := newAppVersionApp(t, AppVersionConfig{MinVersion: "12.0.20"})
	req := httptest.NewRequest(fiber.MethodGet, "/api/v1/test", nil)
	req.Header.Set("X-App-Version", "12.0.10")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	if resp.StatusCode != fiber.StatusUpgradeRequired {
		t.Fatalf("expected 426 for too-old client, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Required-Version"); got != "12.0.20" {
		t.Fatalf("expected X-Required-Version=12.0.20, got %q", got)
	}
	if got := resp.Header.Get("Retry-After"); got != "30" {
		t.Fatalf("expected Retry-After=30, got %q", got)
	}
}

func TestAppVersionMiddleware_MinVersion_AllowsEqualOrNewer(t *testing.T) {
	app := newAppVersionApp(t, AppVersionConfig{MinVersion: "12.0.20"})
	for _, v := range []string{"12.0.20", "12.0.21", "13.0.0"} {
		req := httptest.NewRequest(fiber.MethodGet, "/api/v1/test", nil)
		req.Header.Set("X-App-Version", v)
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("test request %s: %v", v, err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("client %s ≥ min: expected 200, got %d", v, resp.StatusCode)
		}
	}
}

// TestAppVersionMiddleware_MinVersion_PassesThroughMissingHeader is the
// fail-safe default: a too-old client that predates the X-App-Version
// header must NOT be locked out — otherwise rolling out the min-version
// guard would brick every cached client until they happen to receive the
// new bundle through other channels.
func TestAppVersionMiddleware_MinVersion_PassesThroughMissingHeader(t *testing.T) {
	app := newAppVersionApp(t, AppVersionConfig{MinVersion: "12.0.20"})
	req := httptest.NewRequest(fiber.MethodGet, "/api/v1/test", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 when X-App-Version missing, got %d", resp.StatusCode)
	}
}

func TestAppVersionMiddleware_MinVersion_SkipsAuthAndPublic(t *testing.T) {
	app := newAppVersionApp(t, AppVersionConfig{MinVersion: "12.0.20"})
	// Sub-min client must still be able to reach /auth/refresh (so it can
	// pull a fresh token after reload) and /public/system/config (so the
	// version-poll fallback keeps working).
	for _, path := range []string{"/api/v1/auth/refresh", "/api/v1/public/system/config"} {
		req := httptest.NewRequest(fiber.MethodGet, path, nil)
		req.Header.Set("X-App-Version", "12.0.10")
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("test request %s: %v", path, err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("path %s should bypass min-version, got %d", path, resp.StatusCode)
		}
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int // sign expected
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", +1},
		{"1.0.0", "1.0.1", -1},
		{"1.1.0", "1.0.99", +1},
		{"2.0.0", "1.99.99", +1},
		{"12.0.21", "12.0.20", +1},
		// Two-component form padded with implicit 0.
		{"12.0", "12.0.0", 0},
		{"12.1", "12.0.99", +1},
		// Prerelease/build suffix stripped — RC of current version sorts equal.
		{"12.0.21-rc1", "12.0.21", 0},
		{"12.0.21+ci.42", "12.0.21", 0},
		{"12.0.21-rc1", "12.0.20", +1}, // RC of newer still > older
	}
	for _, tc := range cases {
		got := compareSemver(tc.a, tc.b)
		if (got < 0) != (tc.want < 0) || (got > 0) != (tc.want > 0) || (got == 0) != (tc.want == 0) {
			t.Errorf("compareSemver(%q,%q)=%d want sign %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestAppVersionMiddleware_OPTIONS_BypassesMinVersion exercises the
// defence-in-depth OPTIONS skip. Even with a strict MinVersion gate, a
// preflight that somehow reaches this middleware (CORS reorder, future
// refactor) must NOT 426 — that would break the actual cross-origin
// request before it ever runs.
func TestAppVersionMiddleware_OPTIONS_BypassesMinVersion(t *testing.T) {
	app := newAppVersionApp(t, AppVersionConfig{MinVersion: "12.0.20"})
	app.Options("/api/v1/test", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })
	req := httptest.NewRequest(fiber.MethodOptions, "/api/v1/test", nil)
	req.Header.Set("X-App-Version", "1.0.0") // older than min
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	if resp.StatusCode == fiber.StatusUpgradeRequired {
		t.Fatalf("OPTIONS preflight must not be 426'd")
	}
}
