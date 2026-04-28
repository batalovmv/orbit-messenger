// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"github.com/gofiber/fiber/v2"

	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

// AdminSessionHandler exposes Day 5.2 admin Sessions tab endpoints:
//
//	GET    /admin/users/:id/sessions  — list active sessions for a user
//	DELETE /admin/sessions/:id        — revoke one session
//
// Permission, audit, guards and the cross-call to auth all live in the
// service layer. The handler only parses request context.
type AdminSessionHandler struct {
	svc *service.SessionAdminService
}

func NewAdminSessionHandler(svc *service.SessionAdminService) *AdminSessionHandler {
	return &AdminSessionHandler{svc: svc}
}

func (h *AdminSessionHandler) Register(app fiber.Router) {
	admin := app.Group("/admin")
	admin.Get("/users/:id/sessions", h.ListUserSessions)
	admin.Delete("/sessions/:id", h.RevokeSession)
}

func (h *AdminSessionHandler) ListUserSessions(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	sessions, err := h.svc.ListUserSessions(c.Context(), service.ListUserSessionsParams{
		ActorID:        actorID,
		ActorRole:      getUserRole(c),
		ActorSessionID: getUserSessionID(c),
		TargetUserID:   c.Params("id"),
	})
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{"sessions": sessions})
}

func (h *AdminSessionHandler) RevokeSession(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	if err := h.svc.RevokeSession(c.Context(), service.RevokeSessionParams{
		ActorID:        actorID,
		ActorRole:      getUserRole(c),
		ActorSessionID: getUserSessionID(c),
		SessionID:      c.Params("id"),
		IP:             c.IP(),
		UserAgent:      c.Get("User-Agent"),
	}); err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{"status": "revoked"})
}
