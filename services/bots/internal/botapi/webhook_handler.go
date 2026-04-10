package botapi

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/mst-corp/orbit/pkg/apperror"
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
	if err := validateWebhookURL(req.URL); err != nil {
		return botError(c, err)
	}

	webhookURL := strings.TrimSpace(req.URL)
	var secretHash *string
	if strings.TrimSpace(req.Secret) != "" {
		sum := sha256.Sum256([]byte(strings.TrimSpace(req.Secret)))
		value := hex.EncodeToString(sum[:])
		secretHash = &value
	}

	if _, err := h.svc.SetWebhook(c.Context(), bot.ID, &webhookURL, secretHash); err != nil {
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

func validateWebhookURL(raw string) error {
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
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() {
			return apperror.BadRequest("private webhook hosts are not allowed")
		}
	}

	return nil
}
