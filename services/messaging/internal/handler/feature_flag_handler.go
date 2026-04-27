// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"github.com/gofiber/fiber/v2"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/featureflags"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

// FeatureFlagHandler exposes admin CRUD on feature flags + the
// auth/unauth client config endpoints.
type FeatureFlagHandler struct {
	svc *service.FeatureFlagService
}

func NewFeatureFlagHandler(svc *service.FeatureFlagService) *FeatureFlagHandler {
	return &FeatureFlagHandler{svc: svc}
}

func (h *FeatureFlagHandler) Register(app fiber.Router) {
	// Admin CRUD — gated by SysManageSettings inside the service.
	admin := app.Group("/admin")
	admin.Get("/feature-flags", h.ListAdmin)
	admin.Patch("/feature-flags/:key", h.Set)
	admin.Post("/admin-maintenance", h.SetMaintenance) // convenience for the banner UI

	// Authenticated client config — must be inside the X-Internal-Token group
	// so X-User-Role is trusted; the gateway proxies this through JWT.
	app.Get("/system/config", h.GetClientConfig)
}

// RegisterPublic mounts the unauthenticated config endpoint (no JWT) — wired
// directly off the messaging app root in main.go. Returns ONLY the
// allowlist of flags whose registry exposure is `unauth`. Maintenance state
// is always included so the login screen / pre-auth shell can show the
// banner.
func (h *FeatureFlagHandler) RegisterPublic(app fiber.Router) {
	app.Get("/public/system/config", h.GetPublicConfig)
}

func (h *FeatureFlagHandler) ListAdmin(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	flags, err := h.svc.ListAll(c.Context(), actorID, getUserRole(c), c.IP(), c.Get("User-Agent"))
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{"flags": flags})
}

type setFlagRequest struct {
	Enabled  bool                   `json:"enabled"`
	Metadata map[string]interface{} `json:"metadata"`
}

func (h *FeatureFlagHandler) Set(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	key := c.Params("key")
	if key == "" {
		return response.Error(c, apperror.BadRequest("Missing flag key"))
	}
	var req setFlagRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	if req.Metadata == nil {
		req.Metadata = map[string]interface{}{}
	}
	flag, err := h.svc.Set(c.Context(), actorID, getUserRole(c), key, req.Enabled, req.Metadata, c.IP(), c.Get("User-Agent"))
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{"flag": flag})
}

// SetMaintenance is a convenience wrapper over Set for the banner UI:
// `POST /admin/admin-maintenance` with `{enabled, message, block_writes}`.
type setMaintenanceRequest struct {
	Enabled     bool   `json:"enabled"`
	Message     string `json:"message"`
	BlockWrites bool   `json:"block_writes"`
}

func (h *FeatureFlagHandler) SetMaintenance(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	var req setMaintenanceRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	meta := map[string]interface{}{
		"message":      req.Message,
		"block_writes": req.BlockWrites,
	}
	flag, err := h.svc.Set(c.Context(), actorID, getUserRole(c), featureflags.KeyMaintenanceMode, req.Enabled, meta, c.IP(), c.Get("User-Agent"))
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{"flag": flag})
}

// GetClientConfig returns the auth-exposed flag set + maintenance state for
// the logged-in client.
//
// Uses PublicMaintenance: a non-admin authenticated user does not need to
// know which operator toggled the banner or when. Admins get the full state
// via /admin/feature-flags, which carries the metadata in `flag.metadata`.
func (h *FeatureFlagHandler) GetClientConfig(c *fiber.Ctx) error {
	flags := h.svc.VisibleFlags(c.Context(), featureflags.ExposureAuth)
	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"flags":       flags,
		"maintenance": h.svc.PublicMaintenance(c.Context()),
	})
}

// GetPublicConfig returns the unauth-exposed flag set + maintenance state.
// Mounted without JWT/internal-token, so the login screen can read it.
//
// Uses PublicMaintenance (not Maintenance) so the response cannot be used
// to enumerate which admin toggled the flag or when.
func (h *FeatureFlagHandler) GetPublicConfig(c *fiber.Ctx) error {
	flags := h.svc.VisibleFlags(c.Context(), featureflags.ExposureUnauth)
	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"flags":       flags,
		"maintenance": h.svc.PublicMaintenance(c.Context()),
	})
}
