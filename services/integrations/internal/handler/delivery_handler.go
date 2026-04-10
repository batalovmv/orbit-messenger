package handler

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
)

func (h *ConnectorHandler) listDeliveries(c *fiber.Ctx) error {
	if err := checkIntegrationLogsPermission(c); err != nil {
		return response.Error(c, err)
	}

	connectorID, err := parseUUIDParam(c, "id", "connector ID")
	if err != nil {
		return response.Error(c, err)
	}

	limit := c.QueryInt("limit", 50)
	offset := c.QueryInt("offset", 0)

	statusValue := strings.TrimSpace(c.Query("status"))
	var status *string
	if statusValue != "" {
		if !isValidDeliveryStatus(statusValue) {
			return response.Error(c, apperror.BadRequest("status must be one of: pending, delivered, failed, dead_letter"))
		}
		status = &statusValue
	}

	deliveries, total, err := h.svc.ListDeliveries(c.Context(), connectorID, status, limit, offset)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"data":  deliveries,
		"total": total,
	})
}

func (h *ConnectorHandler) getDelivery(c *fiber.Ctx) error {
	if err := checkIntegrationLogsPermission(c); err != nil {
		return response.Error(c, err)
	}

	deliveryID, err := parseUUIDParam(c, "id", "delivery ID")
	if err != nil {
		return response.Error(c, err)
	}

	delivery, err := h.svc.GetDelivery(c.Context(), deliveryID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, delivery)
}

func (h *ConnectorHandler) retryDelivery(c *fiber.Ctx) error {
	if err := checkManageIntegrationsPermission(c); err != nil {
		return response.Error(c, err)
	}

	deliveryID, err := parseUUIDParam(c, "id", "delivery ID")
	if err != nil {
		return response.Error(c, err)
	}

	if err := h.svc.RetryDelivery(c.Context(), deliveryID); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"message":       "Delivery queued for retry",
		"queued_at_utc": time.Now().UTC().Format(time.RFC3339),
	})
}

func isValidDeliveryStatus(value string) bool {
	switch strings.TrimSpace(value) {
	case "pending", "delivered", "failed", "dead_letter":
		return true
	default:
		return false
	}
}
