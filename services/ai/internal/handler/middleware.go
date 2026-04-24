// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"crypto/subtle"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
)

// RequireInternalToken gates /api/v1/* routes behind a shared secret — the
// gateway signs forwarded requests with X-Internal-Token and we reject
// anything without a matching secret. X-User-ID is treated as trusted only
// after that check passes, as per the convention in CLAUDE.md:
//
//	"Backend-сервисы доверяют X-User-ID / X-User-Role ТОЛЬКО если
//	 X-Internal-Token валиден"
func RequireInternalToken(secret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Get("X-User-ID")
		if userID == "" {
			return response.Error(c, apperror.Unauthorized("Missing user context"))
		}
		token := c.Get("X-Internal-Token")
		if secret == "" || token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
			return response.Error(c, apperror.Unauthorized("Invalid internal token"))
		}
		return c.Next()
	}
}

// getUserID parses the trusted X-User-ID header into a UUID. Assumes
// RequireInternalToken has already validated the header's presence.
func getUserID(c *fiber.Ctx) (uuid.UUID, error) {
	idStr := strings.TrimSpace(c.Get("X-User-ID"))
	if idStr == "" {
		return uuid.Nil, apperror.Unauthorized("Missing user context")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return uuid.Nil, apperror.Unauthorized("Invalid user ID")
	}
	return id, nil
}
