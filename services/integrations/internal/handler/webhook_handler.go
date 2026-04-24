// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/integrations/internal/model"
)

const webhookRateLimitPerMinute = 60

var webhookRateLimitScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
local ttl = redis.call('TTL', KEYS[1])
return {count, ttl}
`)

func (h *ConnectorHandler) RegisterPublic(router fiber.Router) {
	// Registered at root path (no /api/v1 prefix) so Fiber's group-level Use()
	// middleware on the /api/v1 authenticated group does not interfere.
	// The gateway proxies POST /api/v1/webhooks/in/:id → here as /webhooks/in/:id.
	router.Post("/webhooks/in/:connectorId", h.receiveWebhook)
	// GET variant for providers that send postbacks as query-string GETs
	// (e.g. Keitaro). HMAC and timestamp locations are controlled by
	// connector.config (see model.ConnectorConfig).
	router.Get("/webhooks/in/:connectorId", h.receiveWebhook)
}

func (h *ConnectorHandler) receiveWebhook(c *fiber.Ctx) error {
	connectorID, err := parseUUIDParam(c, "connectorId", "connector ID")
	if err != nil {
		return response.Error(c, err)
	}

	if err := h.enforceWebhookRateLimit(c, connectorID.String()); err != nil {
		return response.Error(c, err)
	}

	connector, err := h.svc.GetConnector(c.Context(), connectorID)
	if err != nil {
		return response.Error(c, err)
	}
	if !connector.IsActive {
		return response.Error(c, apperror.Forbidden("Connector is deactivated"))
	}

	cfg := connector.Parsed()

	// Method enforcement: if the connector is configured for one HTTP method
	// (e.g. Keitaro only ever sends GET), reject the other method to make
	// replay attacks against the "wrong" endpoint fail fast.
	if !methodAllowed(cfg.HttpMethod, c.Method()) {
		return response.Error(c, apperror.BadRequest(
			fmt.Sprintf("Connector expects %s but got %s", cfg.HttpMethod, c.Method()),
		))
	}

	// Extract signature + timestamp according to the connector's config.
	// Defaults (empty config) = header mode with X-Orbit-Signature and
	// X-Orbit-Timestamp, which is the original Orbit-native behaviour.
	var signature, timestamp string
	if cfg.SignatureLocation == "query" {
		signature = strings.TrimSpace(c.Query(cfg.SignatureParamName))
		timestamp = strings.TrimSpace(c.Query(cfg.TimestampParamName))
	} else {
		signature = strings.TrimSpace(c.Get(cfg.SignatureParamName))
		timestamp = strings.TrimSpace(c.Get(cfg.TimestampParamName))
	}

	// Extract the raw payload bytes that will be used both for signature
	// verification AND for template rendering downstream.
	var rawBody []byte
	if c.Method() == fiber.MethodGet {
		// Build a canonical JSON object from query params (excluding
		// signature/timestamp params). Keys are sorted alphabetically so
		// providers can reproduce the same byte sequence for HMAC.
		rawBody, err = canonicalizeQueryPayload(c, cfg.SignatureParamName, cfg.TimestampParamName)
		if err != nil {
			return response.Error(c, err)
		}
	} else {
		rawBody = append([]byte(nil), c.Body()...)
		if len(rawBody) > 64*1024 {
			return response.Error(c, apperror.BadRequest("Payload too large (max 64KB)"))
		}
		if len(rawBody) == 0 || !json.Valid(rawBody) {
			return response.Error(c, apperror.BadRequest("Invalid JSON payload"))
		}
	}

	// Always validate the timestamp window if a timestamp was supplied. When
	// no timestamp is supplied, VerifySignature below rejects the request
	// (timestamp is mandatory when a connector has a secret, which is always).
	if strings.TrimSpace(timestamp) != "" {
		if err := validateWebhookTimestamp(timestamp); err != nil {
			return response.Error(c, err)
		}
	}
	if err := h.svc.VerifySignature(c.Context(), connectorID, rawBody, signature, timestamp); err != nil {
		return response.Error(c, err)
	}

	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid JSON payload"))
	}

	eventType := firstStringField(payload, "event", "type", "event_type")
	if eventType == "" {
		return response.Error(c, apperror.BadRequest("event_type is required"))
	}
	correlationKey := firstStringField(payload, "correlation_key")
	externalEventID := firstStringField(payload, "external_event_id")
	if len([]rune(eventType)) > 128 {
		return response.Error(c, apperror.BadRequest("event_type is too long"))
	}
	if len([]rune(correlationKey)) > 256 {
		return response.Error(c, apperror.BadRequest("correlation_key is too long"))
	}
	if len([]rune(externalEventID)) > 256 {
		return response.Error(c, apperror.BadRequest("external_event_id is too long"))
	}

	if err := h.svc.ProcessInboundWebhook(
		c.Context(),
		connectorID,
		eventType,
		model.JSONB(rawBody),
		signature,
		timestamp,
		correlationKey,
		externalEventID,
	); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

// methodAllowed returns true when the configured HTTP method matches the
// actual request method. An empty configured method is treated as "POST only"
// for safety — the default ConnectorConfig.applyDefaults() fills POST, so an
// empty value here means the config blob was hand-crafted and incomplete.
func methodAllowed(configuredMethod, actualMethod string) bool {
	switch strings.ToUpper(configuredMethod) {
	case "", "POST":
		return actualMethod == fiber.MethodPost
	case "GET":
		return actualMethod == fiber.MethodGet
	}
	return false
}

// canonicalizeQueryPayload serialises the request's query parameters into a
// deterministic JSON object suitable for HMAC verification and downstream
// template rendering. Signature and timestamp parameters are excluded so the
// caller can derive the exact same payload on both sides.
//
// Multi-valued query params are joined by commas to keep the output shape
// flat (map[string]string). Empty query maps are rejected so the downstream
// firstStringField(event) requirement remains non-ambiguous.
func canonicalizeQueryPayload(c *fiber.Ctx, signatureParam, timestampParam string) ([]byte, error) {
	raw := c.Context().QueryArgs()
	if raw == nil {
		return nil, apperror.BadRequest("Missing query parameters")
	}

	values := make(map[string]string)
	raw.VisitAll(func(key, value []byte) {
		k := string(key)
		if k == signatureParam || k == timestampParam {
			return
		}
		v := string(value)
		if existing, ok := values[k]; ok && existing != "" {
			values[k] = existing + "," + v
		} else {
			values[k] = v
		}
	})

	if len(values) == 0 {
		return nil, apperror.BadRequest("Empty query payload")
	}

	// Marshal with sorted keys for deterministic byte output.
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, apperror.Internal("Failed to encode query payload")
	}
	if len(encoded) > 64*1024 {
		return nil, apperror.BadRequest("Payload too large (max 64KB)")
	}
	return encoded, nil
}

func (h *ConnectorHandler) enforceWebhookRateLimit(c *fiber.Ctx, connectorID string) error {
	if h.redis == nil {
		return apperror.Internal("Rate limiting unavailable")
	}

	ctx := c.Context()
	key := fmt.Sprintf("ratelimit:webhook:%s", connectorID)
	result, err := webhookRateLimitScript.Run(ctx, h.redis, []string{key}, 60).Int64Slice()
	if err != nil {
		h.logger.Error("integration webhook rate limiter Redis error", "connector_id", connectorID, "error", err)
		return apperror.Internal("Rate limiting unavailable")
	}

	count := int(result[0])
	ttlSec := int(result[1])

	c.Set("X-RateLimit-Limit", strconv.Itoa(webhookRateLimitPerMinute))
	remaining := webhookRateLimitPerMinute - count
	if remaining < 0 {
		remaining = 0
	}
	c.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

	if count > webhookRateLimitPerMinute {
		if ttlSec > 0 {
			c.Set("Retry-After", strconv.Itoa(ttlSec))
		}
		return apperror.TooManyRequests("Rate limit exceeded")
	}

	return nil
}

func validateWebhookTimestamp(raw string) error {
	value := strings.TrimSpace(raw)
	if value == "" {
		return apperror.Unauthorized("Missing webhook timestamp")
	}

	ts, err := parseWebhookTimestamp(value)
	if err != nil {
		return apperror.Unauthorized("Invalid webhook timestamp")
	}

	now := time.Now().UTC()
	if ts.Before(now.Add(-5*time.Minute)) || ts.After(now.Add(5*time.Minute)) {
		return apperror.Unauthorized("Webhook timestamp expired")
	}

	return nil
}

func parseWebhookTimestamp(value string) (time.Time, error) {
	if unixSeconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(unixSeconds, 0).UTC(), nil
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unsupported timestamp format")
}

func firstStringField(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text != "" {
			return text
		}
	}
	return ""
}
