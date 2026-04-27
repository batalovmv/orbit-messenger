package handler

import (
	"bufio"
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
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
	admin.Get("/chats/:id/export", h.ExportChat)
	admin.Get("/users", h.ListAllUsers)
	admin.Post("/users/:id/deactivate", h.DeactivateUser)
	admin.Post("/users/:id/reactivate", h.ReactivateUser)
	admin.Patch("/users/:id/role", h.ChangeUserRole)
	admin.Get("/users/:id/export", h.ExportUser)
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
		c.IP(), c.Get("User-Agent"),
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
	// Free-text search across action / target / actor name / details. Cap at
	// 200 chars — long inputs are almost certainly mistakes (paste of a JWT
	// token, etc.) and would slow the ILIKE scan with no hit.
	if q := c.Query("q"); q != "" {
		if len(q) > 200 {
			q = q[:200]
		}
		filter.Q = q
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

func (h *AdminHandler) ExportChat(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	actorRole := getUserRole(c)
	ip := c.IP()
	ua := c.Get("User-Agent")
	if !permissions.HasSysPermission(actorRole, permissions.SysExportData) {
		return response.Error(c, apperror.Forbidden("Insufficient permissions"))
	}
	chatID := c.Params("id")
	c.Set("Content-Type", "application/x-ndjson")
	c.Set("Content-Disposition", "attachment; filename=\"chat-"+chatID+".ndjson\"")
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		if exportErr := h.svc.ExportChatMessages(c.Context(), actorID, actorRole, chatID,
			ip, ua,
			func(row []byte) error {
				if _, writeErr := w.Write(append(row, '\n')); writeErr != nil {
					return writeErr
				}
				return w.Flush()
			}); exportErr != nil {
			errRow, _ := json.Marshal(map[string]string{"error": exportErr.Error()})
			_, _ = w.Write(append(errRow, '\n'))
			_ = w.Flush()
		}
	})
	return nil
}

func (h *AdminHandler) ExportUser(c *fiber.Ctx) error {
	actorID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	actorRole := getUserRole(c)
	ip := c.IP()
	ua := c.Get("User-Agent")
	if !permissions.HasSysPermission(actorRole, permissions.SysExportData) {
		return response.Error(c, apperror.Forbidden("Insufficient permissions"))
	}
	userID := c.Params("id")
	c.Set("Content-Type", "application/x-ndjson")
	c.Set("Content-Disposition", "attachment; filename=\"user-"+userID+".ndjson\"")
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		if exportErr := h.svc.ExportUserData(c.Context(), actorID, actorRole, userID,
			ip, ua,
			func(row []byte) error {
				if _, writeErr := w.Write(append(row, '\n')); writeErr != nil {
					return writeErr
				}
				return w.Flush()
			}); exportErr != nil {
			errRow, _ := json.Marshal(map[string]string{"error": exportErr.Error()})
			_, _ = w.Write(append(errRow, '\n'))
			_ = w.Flush()
		}
	})
	return nil
}
