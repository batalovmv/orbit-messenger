// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/pkg/validator"
)

func (h *BotHandler) installBot(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	if err := checkManageBotsPermission(c); err != nil {
		return response.Error(c, err)
	}

	botID, err := parseUUIDParam(c, "id", "bot ID")
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		ChatID string `json:"chat_id"`
		Scopes int64  `json:"scopes"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return response.Error(c, err)
	}
	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat_id"))
	}

	if err := h.svc.InstallBot(c.Context(), botID, chatID, userID, req.Scopes); err != nil {
		return response.Error(c, err)
	}

	h.logAudit(c.Context(), userID, &botID, "install", c.IP(), c.Get("User-Agent"), map[string]any{"bot_id": botID, "chat_id": chatID})

	return response.JSON(c, fiber.StatusCreated, fiber.Map{
		"bot_id":       botID,
		"chat_id":      chatID,
		"installed_by": userID,
		"scopes":       req.Scopes,
		"is_active":    true,
	})
}

func (h *BotHandler) uninstallBot(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	if err := checkManageBotsPermission(c); err != nil {
		return response.Error(c, err)
	}

	botID, err := parseUUIDParam(c, "id", "bot ID")
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		ChatID string `json:"chat_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return response.Error(c, err)
	}
	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat_id"))
	}

	if err := h.svc.UninstallBot(c.Context(), botID, chatID); err != nil {
		return response.Error(c, err)
	}

	h.logAudit(c.Context(), userID, &botID, "uninstall", c.IP(), c.Get("User-Agent"), map[string]any{"bot_id": botID})

	return response.JSON(c, fiber.StatusOK, fiber.Map{"message": "Bot uninstalled"})
}

func (h *BotHandler) listChatBots(c *fiber.Ctx) error {
	if err := checkManageBotsPermission(c); err != nil {
		return response.Error(c, err)
	}

	chatID, err := parseUUIDParam(c, "chatId", "chat ID")
	if err != nil {
		return response.Error(c, err)
	}

	installations, err := h.svc.ListChatBots(c.Context(), chatID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, installations)
}

// listChatBotCommands returns the slash commands of every bot installed in a
// chat. It is read-only and intentionally lives outside the admin-gated
// /chats/:chatId/bots endpoint so any chat member's composer can populate
// the slash autocomplete tooltip without requiring manage_bots permission.
func (h *BotHandler) listChatBotCommands(c *fiber.Ctx) error {
	chatID, err := parseUUIDParam(c, "chatId", "chat ID")
	if err != nil {
		return response.Error(c, err)
	}

	commands, err := h.svc.ListChatBotCommands(c.Context(), chatID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, commands)
}
