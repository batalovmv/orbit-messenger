// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"strings"
	"time"

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
// `POST /admin/admin-maintenance` with `{enabled, message, block_writes,
// start_at?, end_at?}`. start_at / end_at are RFC3339 (or the browser-
// native "YYYY-MM-DDTHH:MM" produced by <input type=datetime-local>);
// the service-layer sanitiser parses both and stores RFC3339 in metadata.
type setMaintenanceRequest struct {
	Enabled     bool   `json:"enabled"`
	Message     string `json:"message"`
	BlockWrites bool   `json:"block_writes"`
	StartAt     string `json:"start_at,omitempty"`
	EndAt       string `json:"end_at,omitempty"`
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

	// Pre-validate the window at the handler boundary. The service-layer
	// sanitiser is defence-in-depth (silently drops invalid/inverted
	// pairs), but a 400 here gives the operator a clear signal so they
	// fix their input rather than seeing maintenance silently saved
	// without a window.
	startAt, err := parseSetMaintenanceTime(req.StartAt)
	if err != nil {
		return response.Error(c, apperror.BadRequest("invalid start_at"))
	}
	endAt, err := parseSetMaintenanceTime(req.EndAt)
	if err != nil {
		return response.Error(c, apperror.BadRequest("invalid end_at"))
	}
	if startAt != nil && endAt != nil && !endAt.After(*startAt) {
		// Reject equal or inverted windows: a zero-length window would be
		// open for a single tick, which is almost certainly a typo, not
		// the operator's intent.
		return response.Error(c, apperror.BadRequest("end_at must be strictly after start_at"))
	}

	meta := map[string]interface{}{
		"message":      req.Message,
		"block_writes": req.BlockWrites,
	}
	if req.StartAt != "" {
		meta["start_at"] = req.StartAt
	}
	if req.EndAt != "" {
		meta["end_at"] = req.EndAt
	}
	flag, err := h.svc.Set(c.Context(), actorID, getUserRole(c), featureflags.KeyMaintenanceMode, req.Enabled, meta, c.IP(), c.Get("User-Agent"))
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{"flag": flag})
}

// parseSetMaintenanceTime accepts the same shapes the service-layer
// sanitiser does, but reports parse failures so the handler can return a
// proper 400. Empty string is "no bound" → (nil, nil).
func parseSetMaintenanceTime(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil //nolint:nilnil // intentional sentinel for "no bound"
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t, nil
	}
	if t, err := time.Parse("2006-01-02T15:04", s); err == nil {
		return &t, nil
	}
	return nil, errMaintenanceTime
}

var errMaintenanceTime = apperror.BadRequest("invalid maintenance time")

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
