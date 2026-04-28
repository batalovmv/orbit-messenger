// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"github.com/gofiber/fiber/v2"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

// AdminPushHandler exposes Day 5 Push Inspector endpoint:
// POST /admin/push/test — admin-only, gated by SysManageSettings, audited.
type AdminPushHandler struct {
	svc *service.PushAdminService
}

func NewAdminPushHandler(svc *service.PushAdminService) *AdminPushHandler {
	return &AdminPushHandler{svc: svc}
}

func (h *AdminPushHandler) Register(app fiber.Router) {
	admin := app.Group("/admin")
	admin.Post("/push/test", h.SendTestPush)
}

type sendTestPushRequest struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

func (h *AdminPushHandler) SendTestPush(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req sendTestPushRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid JSON body"))
	}

	report, err := h.svc.SendTestPush(c.Context(), service.SendTestPushParams{
		ActorID:   actorID,
		ActorRole: getUserRole(c),
		UserID:    req.UserID,
		Email:     req.Email,
		Title:     req.Title,
		Body:      req.Body,
		IP:        c.IP(),
		UserAgent: c.Get("User-Agent"),
	})
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, report)
}
