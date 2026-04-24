// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/pkg/validator"
	"github.com/mst-corp/orbit/services/bots/internal/model"
)

var commandNameRegex = regexp.MustCompile(`^[a-z0-9_]+$`)

func (h *BotHandler) setCommands(c *fiber.Ctx) error {
	if err := checkManageBotsPermission(c); err != nil {
		return response.Error(c, err)
	}

	botID, err := parseUUIDParam(c, "id", "bot ID")
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		Commands []model.BotCommand `json:"commands"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	if len(req.Commands) > 100 {
		return response.Error(c, apperror.BadRequest("Too many commands (max 100)"))
	}

	for i := range req.Commands {
		req.Commands[i].Command = strings.TrimSpace(req.Commands[i].Command)
		req.Commands[i].Description = strings.TrimSpace(req.Commands[i].Description)

		if err := validator.RequireString(req.Commands[i].Command, "command", 1, 32); err != nil {
			return response.Error(c, err)
		}
		if !commandNameRegex.MatchString(req.Commands[i].Command) {
			return response.Error(c, apperror.BadRequest("command must match ^[a-z0-9_]+$"))
		}
		if err := validator.RequireString(req.Commands[i].Description, "description", 1, 256); err != nil {
			return response.Error(c, err)
		}
	}

	if err := h.svc.SetCommands(c.Context(), botID, req.Commands); err != nil {
		return response.Error(c, err)
	}

	commands, err := h.svc.GetCommands(c.Context(), botID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, commands)
}

func (h *BotHandler) getCommands(c *fiber.Ctx) error {
	if err := checkManageBotsPermission(c); err != nil {
		return response.Error(c, err)
	}

	botID, err := parseUUIDParam(c, "id", "bot ID")
	if err != nil {
		return response.Error(c, err)
	}

	commands, err := h.svc.GetCommands(c.Context(), botID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, commands)
}
