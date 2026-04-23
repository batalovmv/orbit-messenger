package handler

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

type AdminHandler struct {
	svc *service.AdminService
}

func NewAdminHandler(svc *service.AdminService) *AdminHandler {
	return &AdminHandler{svc: svc}
}

func (h *AdminHandler) Register(app fiber.Router) {
	admin := app.Group("/admin")
	admin.Get("/chats", h.ListAllChats)
	admin.Get("/users", h.ListAllUsers)
	admin.Post("/users/:id/deactivate", h.DeactivateUser)
	admin.Post("/users/:id/reactivate", h.ReactivateUser)
	admin.Patch("/users/:id/role", h.ChangeUserRole)
	admin.Get("/audit-log", h.GetAuditLog)
}

func (h *AdminHandler) ListAllChats(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	cursor := c.Query("cursor")
	limit := c.QueryInt("limit", 50)

	chats, nextCursor, hasMore, err := h.svc.ListAllChats(
		c.Context(), actorID, getUserRole(c), cursor, limit,
		c.IP(), c.Get("User-Agent"),
	)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Paginated(c, chats, nextCursor, hasMore)
}

func (h *AdminHandler) ListAllUsers(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	cursor := c.Query("cursor")
	limit := c.QueryInt("limit", 50)

	users, nextCursor, hasMore, err := h.svc.ListAllUsers(
		c.Context(), actorID, getUserRole(c), cursor, limit,
	)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Paginated(c, users, nextCursor, hasMore)
}

func (h *AdminHandler) DeactivateUser(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	targetID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	var req struct {
		Reason string `json:"reason"`
	}
	c.BodyParser(&req) //nolint: optional body

	if err := h.svc.DeactivateUser(
		c.Context(), actorID, targetID, getUserRole(c), req.Reason,
		c.IP(), c.Get("User-Agent"),
	); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, 200, fiber.Map{"status": "deactivated"})
}

func (h *AdminHandler) ReactivateUser(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	targetID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	if err := h.svc.ReactivateUser(
		c.Context(), actorID, targetID, getUserRole(c),
		c.IP(), c.Get("User-Agent"),
	); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, 200, fiber.Map{"status": "reactivated"})
}

func (h *AdminHandler) ChangeUserRole(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	targetID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if err := h.svc.ChangeUserRole(
		c.Context(), actorID, targetID, getUserRole(c), req.Role,
		c.IP(), c.Get("User-Agent"),
	); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, 200, fiber.Map{"status": "role_changed", "new_role": req.Role})
}

func (h *AdminHandler) GetAuditLog(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	filter := store.AuditFilter{
		Cursor: c.Query("cursor"),
		Limit:  c.QueryInt("limit", 50),
	}

	if actorParam := c.Query("actor_id"); actorParam != "" {
		if id, err := uuid.Parse(actorParam); err == nil {
			filter.ActorID = &id
		}
	}
	if action := c.Query("action"); action != "" {
		filter.Action = &action
	}
	if targetType := c.Query("target_type"); targetType != "" {
		filter.TargetType = &targetType
	}
	if targetID := c.Query("target_id"); targetID != "" {
		filter.TargetID = &targetID
	}
	if since := c.Query("since"); since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			filter.Since = &t
		}
	}
	if until := c.Query("until"); until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			filter.Until = &t
		}
	}

	entries, nextCursor, hasMore, err := h.svc.GetAuditLog(
		c.Context(), actorID, getUserRole(c), filter,
		c.IP(), c.Get("User-Agent"),
	)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Paginated(c, entries, nextCursor, hasMore)
}
