// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

type InviteHandler struct {
	svc    *service.InviteService
	logger *slog.Logger
}

func NewInviteHandler(svc *service.InviteService, logger *slog.Logger) *InviteHandler {
	return &InviteHandler{svc: svc, logger: logger}
}

// Register mounts routes that require JWT (handled at gateway level via X-User-ID header).
func (h *InviteHandler) Register(app fiber.Router) {
	app.Post("/chats/:id/invite-link", h.CreateInviteLink)
	app.Get("/chats/:id/invite-links", h.ListInviteLinks)
	app.Put("/invite-links/:id", h.EditInviteLink)
	app.Delete("/invite-links/:id", h.RevokeInviteLink)
	app.Get("/chats/:id/join-requests", h.ListJoinRequests)
	app.Post("/chats/:id/join-requests/:userId/approve", h.ApproveJoinRequest)
	app.Post("/chats/:id/join-requests/:userId/reject", h.RejectJoinRequest)
}

// RegisterPublic mounts routes that bypass JWT at the gateway.
// JoinByInvite still reads X-User-ID because the user must be logged in.
func (h *InviteHandler) RegisterPublic(app fiber.Router) {
	app.Post("/chats/join/:hash", h.JoinByInvite)
	app.Get("/chats/invite/:hash", h.GetInviteInfo)
}

func (h *InviteHandler) CreateInviteLink(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	var req struct {
		Title            *string    `json:"title"`
		ExpireAt         *time.Time `json:"expire_at"`
		UsageLimit       int        `json:"usage_limit"`
		RequiresApproval bool       `json:"requires_approval"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	link, err := h.svc.CreateInviteLink(c.Context(), chatID, uid, req.Title, req.ExpireAt, req.UsageLimit, req.RequiresApproval)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, link)
}

func (h *InviteHandler) ListInviteLinks(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	links, err := h.svc.ListInviteLinks(c.Context(), chatID, uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, links)
}

func (h *InviteHandler) EditInviteLink(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	linkID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid invite link ID"))
	}

	var req struct {
		Title            *string    `json:"title"`
		ExpireAt         *time.Time `json:"expire_at"`
		UsageLimit       *int       `json:"usage_limit"`
		RequiresApproval *bool      `json:"requires_approval"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if err := h.svc.EditInviteLink(c.Context(), linkID, uid, req.Title, req.ExpireAt, req.UsageLimit, req.RequiresApproval); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

func (h *InviteHandler) RevokeInviteLink(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	linkID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid invite link ID"))
	}

	if err := h.svc.RevokeInviteLink(c.Context(), linkID, uid); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

func (h *InviteHandler) GetInviteInfo(c *fiber.Ctx) error {
	hash := c.Params("hash")
	if hash == "" {
		return response.Error(c, apperror.BadRequest("Missing invite hash"))
	}

	info, err := h.svc.GetInviteInfo(c.Context(), hash)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, info)
}

func (h *InviteHandler) JoinByInvite(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	hash := c.Params("hash")
	if hash == "" {
		return response.Error(c, apperror.BadRequest("Missing invite hash"))
	}

	result, err := h.svc.JoinByInvite(c.Context(), hash, uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, result)
}

func (h *InviteHandler) ListJoinRequests(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	requests, err := h.svc.ListJoinRequests(c.Context(), chatID, uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, requests)
}

func (h *InviteHandler) ApproveJoinRequest(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	targetID, err := uuid.Parse(c.Params("userId"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	if err := h.svc.ApproveJoinRequest(c.Context(), chatID, uid, targetID); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

func (h *InviteHandler) RejectJoinRequest(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	targetID, err := uuid.Parse(c.Params("userId"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	if err := h.svc.RejectJoinRequest(c.Context(), chatID, uid, targetID); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}
