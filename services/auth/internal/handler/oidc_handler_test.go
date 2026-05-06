// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/mst-corp/orbit/services/auth/internal/service"
)

// TestOIDC_Config_ReportsDisabledShape ensures the public /auth/oidc/config
// endpoint always responds 200 with a stable shape — the FE relies on
// `enabled:false` to hide the SSO button rather than handling 404s.
func TestOIDC_Config_ReportsDisabledShape(t *testing.T) {
	app := fiber.New()
	h := NewOIDCHandler(nil, nil, &service.OIDCConfig{})
	h.Register(app)

	req := httptest.NewRequest("GET", "/auth/oidc/config", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("json: %v body=%s", err, body)
	}
	if got["enabled"] != false {
		t.Errorf("enabled=%v want false", got["enabled"])
	}
	if got["providerKey"] != "" {
		t.Errorf("providerKey=%q want empty", got["providerKey"])
	}
	if got["displayName"] != "" {
		t.Errorf("displayName=%q want empty", got["displayName"])
	}
}

// TestOIDC_Config_ReportsEnabledShape covers the happy path: when the env
// block is fully populated and provider discovery has succeeded, the FE
// gets the data it needs to render the labelled sign-in button.
func TestOIDC_Config_ReportsEnabledShape(t *testing.T) {
	cfg := &service.OIDCConfig{
		ProviderKey:  "google",
		DisplayName:  "Acme SSO",
		Issuer:       "https://accounts.google.com",
		ClientID:     "cid",
		ClientSecret: "secret",
		RedirectURL:  "https://app/callback",
	}
	// We don't need a real *Provider here — the handler only checks for
	// non-nil, since the discovered Provider is the source of truth that
	// /authorize and /callback can actually run.
	app := fiber.New()
	h := &OIDCHandler{cfg: cfg, provider: &service.Provider{}}
	h.Register(app)

	req := httptest.NewRequest("GET", "/auth/oidc/config", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("json: %v body=%s", err, body)
	}
	if got["enabled"] != true {
		t.Errorf("enabled=%v want true", got["enabled"])
	}
	if got["providerKey"] != "google" {
		t.Errorf("providerKey=%q want google", got["providerKey"])
	}
	if got["displayName"] != "Acme SSO" {
		t.Errorf("displayName=%q want Acme SSO", got["displayName"])
	}
}

// TestOIDC_Config_RegisteredBeforeProviderGroup pins down the route order:
// /auth/oidc/config must NOT be eaten by the /:provider param group.
func TestOIDC_Config_RegisteredBeforeProviderGroup(t *testing.T) {
	app := fiber.New()
	h := NewOIDCHandler(nil, nil, &service.OIDCConfig{})
	h.Register(app)

	// Hitting /auth/oidc/config/authorize would only match if "config" was
	// being parsed as :provider — that path doesn't exist on the handler,
	// so we expect 404, not a redirect (which is what /authorize returns).
	req := httptest.NewRequest("GET", "/auth/oidc/config/authorize", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != 404 {
		t.Errorf("expected 404 for /auth/oidc/config/authorize, got %d", resp.StatusCode)
	}

	// And the literal /config should answer 200.
	req2 := httptest.NewRequest("GET", "/auth/oidc/config", nil)
	resp2, _ := app.Test(req2)
	if resp2.StatusCode != 200 {
		t.Errorf("expected 200 for /auth/oidc/config, got %d", resp2.StatusCode)
	}
}
