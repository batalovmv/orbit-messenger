// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"net/url"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/auth/internal/service"
)

// OIDCHandler exposes the /auth/oidc/{provider}/{authorize,callback} routes.
// `provider` is the configured ProviderKey — when set to anything else the
// handlers return 404 to avoid leaking the configuration shape.
type OIDCHandler struct {
	svc      *service.AuthService
	provider *service.Provider
	cfg      *service.OIDCConfig
}

func NewOIDCHandler(svc *service.AuthService, provider *service.Provider, cfg *service.OIDCConfig) *OIDCHandler {
	return &OIDCHandler{svc: svc, provider: provider, cfg: cfg}
}

// Register wires the routes under /auth/oidc. Always called: the handler
// returns 404 internally when the provider isn't enabled, so the URL space
// is consistent regardless of configuration (no surprise 404 vs 405).
func (h *OIDCHandler) Register(app *fiber.App) {
	// /auth/oidc/config is intentionally registered before the :provider group
	// so the literal "config" segment never gets eaten as a provider key.
	app.Get("/auth/oidc/config", h.Config)
	g := app.Group("/auth/oidc/:provider")
	g.Get("/authorize", h.Authorize)
	g.Get("/callback", h.Callback)
}

// Config is a public endpoint the SPA polls on first paint to decide whether
// to render the SSO sign-in button. Always answers 200 — when SSO is off the
// FE simply hides the button. Returning a stable shape (rather than 404)
// avoids the FE having to special-case missing-endpoint vs disabled-provider.
func (h *OIDCHandler) Config(c *fiber.Ctx) error {
	if h.cfg == nil || !h.cfg.Enabled() || h.provider == nil {
		return c.JSON(fiber.Map{
			"enabled":     false,
			"providerKey": "",
			"displayName": "",
		})
	}
	return c.JSON(fiber.Map{
		"enabled":     true,
		"providerKey": h.cfg.ProviderKey,
		"displayName": h.cfg.DisplayLabel(),
	})
}

func (h *OIDCHandler) enabled(c *fiber.Ctx) bool {
	if h.provider == nil || h.cfg == nil || !h.cfg.Enabled() {
		return false
	}
	return c.Params("provider") == h.cfg.ProviderKey
}

func (h *OIDCHandler) Authorize(c *fiber.Ctx) error {
	if !h.enabled(c) {
		return response.Error(c, apperror.NotFound("OIDC provider not configured"))
	}
	returnTo := c.Query("return_to")
	authURL, err := h.svc.AuthorizeURL(c.Context(), h.provider, returnTo)
	if err != nil {
		return response.Error(c, err)
	}
	return c.Redirect(authURL, fiber.StatusFound)
}

func (h *OIDCHandler) Callback(c *fiber.Ctx) error {
	if !h.enabled(c) {
		return response.Error(c, apperror.NotFound("OIDC provider not configured"))
	}
	state := c.Query("state")
	code := c.Query("code")
	if errParam := c.Query("error"); errParam != "" {
		// Provider rejected — surface a useful message rather than just 400.
		desc := c.Query("error_description")
		return response.Error(c, apperror.BadRequest("OIDC provider error: "+errParam+" "+desc))
	}

	res, err := h.svc.HandleCallback(c.Context(), h.provider,
		state, code, c.IP(), c.Get("User-Agent"))
	if err != nil {
		return response.Error(c, err)
	}

	// Refresh cookie matches the existing /auth/login path semantics so
	// gateway middleware needs no special-casing for SSO sessions.
	c.Cookie(makeRefreshCookie(res.Tokens.RefreshToken, int(30*24*time.Hour/time.Second)))

	// Append the access token to the redirect URL so the SPA can read it
	// once on first paint and then store in memory; refresh cookie carries
	// the long-term session. The fragment-vs-query trade-off: query is
	// chosen so server-side analytics can spot SSO landings without JS
	// participation. The SPA must strip the params from history immediately.
	redirectURL := res.ReturnTo
	if u, perr := url.Parse(redirectURL); perr == nil {
		existing := u.Query()
		existing.Set("access_token", res.Tokens.AccessToken)
		existing.Set("expires_in", strconv.Itoa(res.Tokens.ExpiresIn))
		u.RawQuery = existing.Encode()
		redirectURL = u.String()
	}
	return c.Redirect(redirectURL, fiber.StatusFound)
}
