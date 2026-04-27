// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

// configServer builds an httptest server that always returns the given
// maintenance state on /public/system/config and increments calls on each hit.
func configServer(t *testing.T, state maintenanceState, calls *atomic.Int32) string {
	t.Helper()
	body, _ := json.Marshal(publicConfig{Maintenance: state})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			calls.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestMaintenance_BlocksWrites_WhenActiveAndBlockingEnabled(t *testing.T) {
	url := configServer(t, maintenanceState{Active: true, Message: "down", BlockWrites: true}, nil)

	app := fiber.New()
	app.Use(MaintenanceMiddleware(MaintenanceConfig{MessagingURL: url, PollInterval: 1 * time.Hour}))
	app.Post("/x", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest(fiber.MethodPost, "/x", nil)
	req.Header.Set("X-User-Role", "member")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Retry-After"); got != "30" {
		t.Errorf("expected Retry-After=30, got %q", got)
	}
}

func TestMaintenance_AllowsReads_WhenActive(t *testing.T) {
	url := configServer(t, maintenanceState{Active: true, BlockWrites: true}, nil)

	app := fiber.New()
	app.Use(MaintenanceMiddleware(MaintenanceConfig{MessagingURL: url, PollInterval: 1 * time.Hour}))
	app.Get("/x", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest(fiber.MethodGet, "/x", nil)
	req.Header.Set("X-User-Role", "member")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestMaintenance_AllowsSuperadmin_Through(t *testing.T) {
	url := configServer(t, maintenanceState{Active: true, BlockWrites: true}, nil)

	app := fiber.New()
	app.Use(MaintenanceMiddleware(MaintenanceConfig{MessagingURL: url, PollInterval: 1 * time.Hour}))
	app.Post("/admin/x", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest(fiber.MethodPost, "/admin/x", nil)
	req.Header.Set("X-User-Role", "SUPERADMIN ")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("superadmin must bypass; got %d", resp.StatusCode)
	}
}

func TestMaintenance_BannerOnly_DoesNotBlock(t *testing.T) {
	url := configServer(t, maintenanceState{Active: true, BlockWrites: false, Message: "info"}, nil)

	app := fiber.New()
	app.Use(MaintenanceMiddleware(MaintenanceConfig{MessagingURL: url, PollInterval: 1 * time.Hour}))
	app.Post("/x", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest(fiber.MethodPost, "/x", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("banner-only mode must not block writes; got %d", resp.StatusCode)
	}
}

func TestMaintenance_FailsOpen_OnUpstreamUnreachable(t *testing.T) {
	app := fiber.New()
	// Bogus URL — no server bound to that port.
	app.Use(MaintenanceMiddleware(MaintenanceConfig{MessagingURL: "http://127.0.0.1:1", PollInterval: 1 * time.Hour}))
	app.Post("/x", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest(fiber.MethodPost, "/x", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("must fail open if messaging is unreachable; got %d", resp.StatusCode)
	}
}

func TestIsMutating(t *testing.T) {
	cases := map[string]bool{
		fiber.MethodGet:     false,
		fiber.MethodHead:    false,
		fiber.MethodOptions: false,
		fiber.MethodPost:    true,
		fiber.MethodPut:     true,
		fiber.MethodPatch:   true,
		fiber.MethodDelete:  true,
	}
	for m, want := range cases {
		if got := isMutating(strings.ToUpper(m)); got != want {
			t.Errorf("isMutating(%q) = %v, want %v", m, got, want)
		}
	}
}
