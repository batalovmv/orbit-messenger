// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
)

func (h *BotHandler) rotateToken(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	botID, err := parseUUIDParam(c, "id", "bot ID")
	if err != nil {
		return response.Error(c, err)
	}

	token, err := h.svc.RotateToken(c.Context(), userID, getUserRole(c), botID)
	if err != nil {
		return response.Error(c, err)
	}
	if token == "" {
		return response.Error(c, apperror.Internal("token generation failed"))
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"token": token})
}
