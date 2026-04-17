package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/pkg/validator"
	"github.com/mst-corp/orbit/services/integrations/internal/service"
)

// updateRoute applies partial changes to a route (event_filter / template / is_active).
func (h *ConnectorHandler) updateRoute(c *fiber.Ctx) error {
	if err := checkManageIntegrationsPermission(c); err != nil {
		return response.Error(c, err)
	}

	routeID, err := parseUUIDParam(c, "id", "route ID")
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		EventFilter *string `json:"event_filter"`
		Template    *string `json:"template"`
		IsActive    *bool   `json:"is_active"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	if req.EventFilter != nil && strings.TrimSpace(*req.EventFilter) != "" {
		if err := validator.RequireString(*req.EventFilter, "event_filter", 1, 128); err != nil {
			return response.Error(c, err)
		}
	}
	if req.Template != nil && strings.TrimSpace(*req.Template) != "" {
		if err := validator.RequireString(*req.Template, "template", 1, 4000); err != nil {
			return response.Error(c, err)
		}
	}

	route, err := h.svc.UpdateRoute(c.Context(), routeID, service.UpdateRouteInput{
		EventFilter: req.EventFilter,
		Template:    req.Template,
		IsActive:    req.IsActive,
	})
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, route)
}

// getConnectorStats returns a status-bucketed delivery summary for the requested window.
// Window parsed from ?window= using Go's time.ParseDuration (e.g. "24h", "1h", "7d"
// is NOT supported — use "168h" for a week, mirroring the standard parser).
func (h *ConnectorHandler) getConnectorStats(c *fiber.Ctx) error {
	if err := checkIntegrationLogsPermission(c); err != nil {
		return response.Error(c, err)
	}

	connectorID, err := parseUUIDParam(c, "id", "connector ID")
	if err != nil {
		return response.Error(c, err)
	}

	window := 24 * time.Hour
	if raw := strings.TrimSpace(c.Query("window")); raw != "" {
		parsed, parseErr := time.ParseDuration(raw)
		if parseErr != nil {
			return response.Error(c, apperror.BadRequest("window must be a duration like 1h or 24h"))
		}
		if parsed < time.Minute || parsed > 30*24*time.Hour {
			return response.Error(c, apperror.BadRequest("window must be between 1m and 30d"))
		}
		window = parsed
	}

	stats, err := h.svc.GetConnectorStats(c.Context(), connectorID, window)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, stats)
}

// testConnector injects a synthetic event payload through the connector's
// normal template/routing pipeline so admins can validate wiring end-to-end
// without touching the upstream provider.
func (h *ConnectorHandler) testConnector(c *fiber.Ctx) error {
	if err := checkManageIntegrationsPermission(c); err != nil {
		return response.Error(c, err)
	}

	connectorID, err := parseUUIDParam(c, "id", "connector ID")
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		EventType string          `json:"event_type"`
		Payload   map[string]any  `json:"payload"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	if strings.TrimSpace(req.EventType) == "" {
		req.EventType = "test.event"
	}
	if req.Payload == nil {
		req.Payload = map[string]any{
			"message":   "Test event from Orbit Integrations.",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"source":    "admin_test_button",
		}
	}

	result, err := h.svc.TestConnector(c.Context(), connectorID, req.EventType, req.Payload)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusAccepted, result)
}

// numberParam is a small helper used by pagination: parse an int query param
// with a default fallback (kept separate from Fiber's QueryInt so validation
// errors are consistent with the rest of the integration handlers).
func numberParam(c *fiber.Ctx, name string, def int) int {
	if raw := strings.TrimSpace(c.Query(name)); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			return n
		}
	}
	return def
}
