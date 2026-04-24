// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/pkg/validator"
	"github.com/mst-corp/orbit/services/integrations/internal/model"
	"github.com/mst-corp/orbit/services/integrations/internal/service"
)

var connectorNameRegex = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

type ConnectorHandler struct {
	svc    *service.IntegrationService
	logger *slog.Logger
	redis  *redis.Client
}

func NewConnectorHandler(svc *service.IntegrationService, logger *slog.Logger) *ConnectorHandler {
	return &ConnectorHandler{svc: svc, logger: logger}
}

func (h *ConnectorHandler) WithRedis(redisClient *redis.Client) *ConnectorHandler {
	h.redis = redisClient
	return h
}

func (h *ConnectorHandler) Register(router fiber.Router) {
	router.Post("/integrations/connectors", h.createConnector)
	router.Get("/integrations/connectors", h.listConnectors)
	router.Get("/integrations/connectors/:id", h.getConnector)
	router.Patch("/integrations/connectors/:id", h.updateConnector)
	router.Delete("/integrations/connectors/:id", h.deleteConnector)
	router.Post("/integrations/connectors/:id/rotate-secret", h.rotateSecret)
	router.Post("/integrations/connectors/:id/routes", h.createRoute)
	router.Delete("/integrations/routes/:id", h.deleteRoute)
	router.Patch("/integrations/routes/:id", h.updateRoute)
	router.Get("/integrations/connectors/:id/routes", h.listRoutes)
	router.Get("/integrations/connectors/:id/deliveries", h.listDeliveries)
	router.Get("/integrations/connectors/:id/stats", h.getConnectorStats)
	router.Post("/integrations/connectors/:id/test", h.testConnector)
	router.Post("/integrations/templates/preview", h.previewTemplate)
	router.Get("/integrations/deliveries/:id", h.getDelivery)
	router.Post("/integrations/deliveries/:id/retry", h.retryDelivery)
}

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

func getUserID(c *fiber.Ctx) (uuid.UUID, error) {
	idStr := c.Get("X-User-ID")
	if idStr == "" {
		return uuid.Nil, apperror.Unauthorized("Missing user context")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return uuid.Nil, apperror.Unauthorized("Invalid user ID")
	}
	return id, nil
}

func getUserRole(c *fiber.Ctx) string {
	return strings.ToLower(strings.TrimSpace(c.Get("X-User-Role")))
}

func checkManageIntegrationsPermission(c *fiber.Ctx) error {
	if _, err := getUserID(c); err != nil {
		return err
	}
	if !permissions.HasSysPermission(getUserRole(c), permissions.SysManageIntegrations) {
		return apperror.Forbidden("Insufficient permissions")
	}
	return nil
}

func checkIntegrationLogsPermission(c *fiber.Ctx) error {
	if _, err := getUserID(c); err != nil {
		return err
	}
	role := getUserRole(c)
	if permissions.HasSysPermission(role, permissions.SysManageIntegrations) || permissions.HasSysPermission(role, permissions.SysViewBotLogs) {
		return nil
	}
	return apperror.Forbidden("Insufficient permissions")
}

func (h *ConnectorHandler) createConnector(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	if err := checkManageIntegrationsPermission(c); err != nil {
		return response.Error(c, err)
	}

	var req model.CreateConnectorRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	if err := validateConnectorName(req.Name); err != nil {
		return response.Error(c, err)
	}
	if err := validator.RequireString(req.DisplayName, "display_name", 1, 128); err != nil {
		return response.Error(c, err)
	}
	if err := validateConnectorType(req.Type); err != nil {
		return response.Error(c, err)
	}
	if req.BotID != nil && strings.TrimSpace(*req.BotID) != "" {
		if err := validator.RequireUUID(strings.TrimSpace(*req.BotID), "bot_id"); err != nil {
			return response.Error(c, err)
		}
	}

	connector, secret, err := h.svc.CreateConnector(c.Context(), userID, req)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, fiber.Map{
		"connector": connector,
		"secret":    secret,
	})
}

func (h *ConnectorHandler) listConnectors(c *fiber.Ctx) error {
	if err := checkManageIntegrationsPermission(c); err != nil {
		return response.Error(c, err)
	}

	limit := c.QueryInt("limit", 50)
	offset := c.QueryInt("offset", 0)
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	connectors, total, err := h.svc.ListConnectors(c.Context(), limit, offset)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"data":  connectors,
		"total": total,
	})
}

func (h *ConnectorHandler) getConnector(c *fiber.Ctx) error {
	if err := checkManageIntegrationsPermission(c); err != nil {
		return response.Error(c, err)
	}

	connectorID, err := parseUUIDParam(c, "id", "connector ID")
	if err != nil {
		return response.Error(c, err)
	}

	connector, err := h.svc.GetConnector(c.Context(), connectorID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, connector)
}

func (h *ConnectorHandler) updateConnector(c *fiber.Ctx) error {
	if err := checkManageIntegrationsPermission(c); err != nil {
		return response.Error(c, err)
	}

	connectorID, err := parseUUIDParam(c, "id", "connector ID")
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		Name        *string          `json:"name"`
		DisplayName *string          `json:"display_name"`
		Type        *string          `json:"type"`
		BotID       *string          `json:"bot_id"`
		Config      *json.RawMessage `json:"config"`
		IsActive    *bool            `json:"is_active"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if req.Name != nil {
		if err := validateConnectorName(*req.Name); err != nil {
			return response.Error(c, err)
		}
	}
	if req.DisplayName != nil {
		if err := validator.RequireString(*req.DisplayName, "display_name", 1, 128); err != nil {
			return response.Error(c, err)
		}
	}
	if req.Type != nil {
		if err := validateConnectorType(*req.Type); err != nil {
			return response.Error(c, err)
		}
	}

	var botID *uuid.UUID
	if req.BotID != nil && strings.TrimSpace(*req.BotID) != "" {
		if err := validator.RequireUUID(strings.TrimSpace(*req.BotID), "bot_id"); err != nil {
			return response.Error(c, err)
		}
		parsed, err := uuid.Parse(strings.TrimSpace(*req.BotID))
		if err != nil {
			return response.Error(c, apperror.BadRequest("Invalid bot_id"))
		}
		botID = &parsed
	}

	var configValue *model.JSONB
	if req.Config != nil {
		if !json.Valid(*req.Config) {
			return response.Error(c, apperror.BadRequest("config must be valid JSON"))
		}
		if len(*req.Config) > 16*1024 {
			return response.Error(c, apperror.BadRequest("config must not exceed 16KB"))
		}
		cfg := model.JSONB(append([]byte(nil), (*req.Config)...))
		configValue = &cfg
	}

	connector, err := h.svc.UpdateConnector(c.Context(), connectorID, service.UpdateConnectorInput{
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Type:        req.Type,
		BotID:       botID,
		Config:      configValue,
		IsActive:    req.IsActive,
	})
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, connector)
}

func (h *ConnectorHandler) deleteConnector(c *fiber.Ctx) error {
	if err := checkManageIntegrationsPermission(c); err != nil {
		return response.Error(c, err)
	}

	connectorID, err := parseUUIDParam(c, "id", "connector ID")
	if err != nil {
		return response.Error(c, err)
	}

	if err := h.svc.DeleteConnector(c.Context(), connectorID); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"message": "Connector deleted"})
}

func (h *ConnectorHandler) rotateSecret(c *fiber.Ctx) error {
	if err := checkManageIntegrationsPermission(c); err != nil {
		return response.Error(c, err)
	}

	connectorID, err := parseUUIDParam(c, "id", "connector ID")
	if err != nil {
		return response.Error(c, err)
	}

	secret, err := h.svc.RotateSecret(c.Context(), connectorID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"secret": secret})
}

func (h *ConnectorHandler) createRoute(c *fiber.Ctx) error {
	if err := checkManageIntegrationsPermission(c); err != nil {
		return response.Error(c, err)
	}

	connectorID, err := parseUUIDParam(c, "id", "connector ID")
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		ChatID      string  `json:"chat_id"`
		EventFilter *string `json:"event_filter"`
		Template    *string `json:"template"`
		IsActive    *bool   `json:"is_active"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	if err := validator.RequireUUID(strings.TrimSpace(req.ChatID), "chat_id"); err != nil {
		return response.Error(c, err)
	}
	chatID, err := uuid.Parse(strings.TrimSpace(req.ChatID))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat_id"))
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

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	route, err := h.svc.CreateRoute(c.Context(), &model.Route{
		ConnectorID: connectorID,
		ChatID:      chatID,
		EventFilter: normalizeNullableString(req.EventFilter),
		Template:    normalizeNullableString(req.Template),
		IsActive:    isActive,
	})
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, route)
}

func (h *ConnectorHandler) deleteRoute(c *fiber.Ctx) error {
	if err := checkManageIntegrationsPermission(c); err != nil {
		return response.Error(c, err)
	}

	routeID, err := parseUUIDParam(c, "id", "route ID")
	if err != nil {
		return response.Error(c, err)
	}

	if err := h.svc.DeleteRoute(c.Context(), routeID); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"message": "Route deleted"})
}

func (h *ConnectorHandler) listRoutes(c *fiber.Ctx) error {
	if err := checkManageIntegrationsPermission(c); err != nil {
		return response.Error(c, err)
	}

	connectorID, err := parseUUIDParam(c, "id", "connector ID")
	if err != nil {
		return response.Error(c, err)
	}

	routes, err := h.svc.ListRoutes(c.Context(), connectorID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"data": routes})
}

func parseUUIDParam(c *fiber.Ctx, name, label string) (uuid.UUID, error) {
	value := strings.TrimSpace(c.Params(name))
	if err := validator.RequireUUID(value, name); err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, apperror.BadRequest("Invalid " + label)
	}
	return id, nil
}

func validateConnectorName(name string) error {
	if err := validator.RequireString(name, "name", 3, 64); err != nil {
		return err
	}
	if !connectorNameRegex.MatchString(strings.TrimSpace(name)) {
		return apperror.BadRequest("name must contain only letters, numbers, or hyphens")
	}
	return nil
}

func validateConnectorType(value string) error {
	switch strings.TrimSpace(value) {
	case "inbound_webhook", "outbound_webhook", "polling":
		return nil
	default:
		return apperror.BadRequest("type must be one of: inbound_webhook, outbound_webhook, polling")
	}
}

func normalizeNullableString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
