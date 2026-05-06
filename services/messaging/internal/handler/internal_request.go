// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"crypto/subtle"

	"github.com/gofiber/fiber/v2"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
)

func isInternalRequest(c *fiber.Ctx, secret string) bool {
	if secret == "" {
		return false
	}

	token := c.Get("X-Internal-Token")
	return token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(secret)) == 1
}

func requireInternalRequest(c *fiber.Ctx, secret string) error {
	if !isInternalRequest(c, secret) {
		return apperror.Forbidden("Internal access only")
	}

	return nil
}

// RequireInternalOnly returns a Fiber middleware for server-to-server routes
// where no user context is present. Only validates X-Internal-Token.
func RequireInternalOnly(secret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		token := c.Get("X-Internal-Token")
		if secret == "" || token == "" ||
			subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
			return response.Error(c, apperror.Forbidden("Internal access only"))
		}
		return c.Next()
	}
}

// RequireInternalToken returns a Fiber middleware that validates X-Internal-Token
// on every request. X-User-ID is only trusted when the token is valid.
// This prevents identity spoofing if the service is reached outside the gateway.
func RequireInternalToken(secret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Get("X-User-ID")
		if userID == "" {
			return response.Error(c, apperror.Unauthorized("Missing user context"))
		}
		token := c.Get("X-Internal-Token")
		if secret == "" || token == "" ||
			subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
			return response.Error(c, apperror.Unauthorized("Invalid internal token"))
		}
		return c.Next()
	}
}
