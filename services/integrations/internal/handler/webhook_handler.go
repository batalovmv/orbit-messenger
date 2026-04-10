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

	rawBody := append([]byte(nil), c.Body()...)
	if len(rawBody) > 64*1024 {
		return response.Error(c, apperror.BadRequest("Payload too large (max 64KB)"))
	}
	if len(rawBody) == 0 || !json.Valid(rawBody) {
		return response.Error(c, apperror.BadRequest("Invalid JSON payload"))
	}

	signature := strings.TrimSpace(c.Get("X-Orbit-Signature"))
	timestamp := strings.TrimSpace(c.Get("X-Orbit-Timestamp"))
	// Timestamp validation happens regardless of signature presence when connector has a secret
	if signature != "" {
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
