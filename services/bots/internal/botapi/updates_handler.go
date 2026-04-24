// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botapi

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/mst-corp/orbit/pkg/apperror"
)

func (h *BotAPIHandler) getUpdates(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if bot.WebhookURL != nil && strings.TrimSpace(*bot.WebhookURL) != "" {
		return botError(c, apperror.BadRequest("Webhook is already set"))
	}
	if h.updateQueue == nil {
		return botError(c, apperror.Internal("Update queue is not configured"))
	}

	var req struct {
		Offset  int64 `json:"offset"`
		Limit   int   `json:"limit"`
		Timeout int   `json:"timeout"`
	}
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return botError(c, apperror.BadRequest("Invalid request body"))
		}
	}

	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 100
	}
	if req.Timeout < 0 {
		req.Timeout = 0
	}
	if req.Timeout > 50 {
		req.Timeout = 50
	}

	if req.Offset > 0 {
		if err := h.updateQueue.Ack(bot.ID, req.Offset); err != nil {
			return botError(c, err)
		}
	}

	updates, err := h.updateQueue.Pop(c.Context(), bot.ID, req.Limit, time.Duration(req.Timeout)*time.Second)
	if err != nil {
		return botError(c, err)
	}

	return botSuccess(c, updates)
}
