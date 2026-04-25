// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botapi

import (
	"net"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/crypto"
)

func (h *BotAPIHandler) setWebhook(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}

	var req SetWebhookRequest
	if err := c.BodyParser(&req); err != nil {
		return botError(c, apperror.BadRequest("Invalid request body"))
	}
	if err := validateWebhookURL(req.URL, h.webhookAllowList); err != nil {
		return botError(c, err)
	}

	webhookURL := strings.TrimSpace(req.URL)
	var secretEnc *string
	if strings.TrimSpace(req.Secret) != "" {
		encrypted, encErr := crypto.Encrypt(strings.TrimSpace(req.Secret), h.encryptionKey)
		if encErr != nil {
			return botError(c, apperror.Internal("failed to encrypt webhook secret"))
		}
		secretEnc = &encrypted
	}

	if _, err := h.svc.SetWebhook(c.Context(), bot.ID, &webhookURL, secretEnc); err != nil {
		return botError(c, err)
	}

	return botSuccess(c, true)
}

func (h *BotAPIHandler) deleteWebhook(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}

	if _, err := h.svc.SetWebhook(c.Context(), bot.ID, nil, nil); err != nil {
		return botError(c, err)
	}

	return botSuccess(c, true)
}

func (h *BotAPIHandler) getWebhookInfo(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}

	urlValue := ""
	if bot.WebhookURL != nil {
		urlValue = *bot.WebhookURL
	}

	return botSuccess(c, map[string]any{
		"url":                    urlValue,
		"has_custom_certificate": false,
		"pending_update_count":   0,
	})
}

func validateWebhookURL(raw string, allowList []string) error {
	value := strings.TrimSpace(raw)
	if value == "" {
		return apperror.BadRequest("url is required")
	}
	if len(value) > 2048 {
		return apperror.BadRequest("url is too long")
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return apperror.BadRequest("url is invalid")
	}

	host := parsed.Hostname()
	if host == "" {
		return apperror.BadRequest("url is invalid")
	}
	if !strings.EqualFold(host, "localhost") && parsed.Scheme != "https" {
		return apperror.BadRequest("url must use https")
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return apperror.BadRequest("failed to resolve webhook host")
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() {
			return apperror.BadRequest("private or reserved webhook hosts are not allowed")
		}
	}

	// Allow-list check (skip if list is empty — dev mode)
	if len(allowList) > 0 {
		if !isHostAllowed(host, allowList) {
			return apperror.BadRequest("webhook host not in allow-list")
		}
	}

	return nil
}

// isHostAllowed checks if host matches any entry in the allow-list.
// Entries may be plain hostnames or wildcard patterns like "*.example.com".
func isHostAllowed(host string, allowList []string) bool {
	host = strings.ToLower(host)
	for _, entry := range allowList {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if strings.HasPrefix(entry, "*.") {
			// Wildcard: *.example.com matches foo.example.com but NOT example.com
			suffix := entry[1:] // ".example.com"
			if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
				return true
			}
		} else {
			if host == entry {
				return true
			}
		}
	}
	return false
}
