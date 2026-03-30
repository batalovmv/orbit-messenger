package handler

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

type ChatHandler struct {
	svc    *service.ChatService
	logger *slog.Logger
}

func NewChatHandler(svc *service.ChatService, logger *slog.Logger) *ChatHandler {
	return &ChatHandler{svc: svc, logger: logger}
}

func (h *ChatHandler) Register(app fiber.Router) {
	// Order matters: /chats/direct BEFORE /chats/:id
	app.Get("/chats", h.ListChats)
	app.Post("/chats/direct", h.CreateDirectChat)
	app.Post("/chats", h.CreateChat)
	app.Get("/chats/:id", h.GetChat)
	app.Put("/chats/:id", h.UpdateChat)
	app.Delete("/chats/:id", h.DeleteChat)
	app.Get("/chats/:id/members", h.GetMembers)
	app.Post("/chats/:id/members", h.AddMembers)
	app.Delete("/chats/:id/members/:userId", h.RemoveMember)
	app.Patch("/chats/:id/members/:userId", h.UpdateMemberRole)
	app.Get("/chats/:id/members/:userId", h.GetMember)
	app.Get("/chats/:id/member-ids", h.GetMemberIDs)
	app.Get("/chats/:id/admins", h.GetAdmins)
	app.Put("/chats/:id/permissions", h.UpdateDefaultPermissions)
	app.Put("/chats/:id/members/:userId/permissions", h.UpdateMemberPermissions)
	app.Post("/chats/:id/slow-mode", h.SetSlowMode)
	app.Put("/chats/:id/photo", h.UpdateChatPhoto)
	app.Delete("/chats/:id/photo", h.DeleteChatPhoto)
}

func (h *ChatHandler) ListChats(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	cursor := c.Query("cursor")
	limit := c.QueryInt("limit", 50)
	if limit > 100 {
		limit = 100
	}

	items, nextCursor, hasMore, err := h.svc.ListChats(c.Context(), uid, cursor, limit)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Paginated(c, items, nextCursor, hasMore)
}

func (h *ChatHandler) CreateDirectChat(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		UserID string `json:"user_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	otherID, err := uuid.Parse(req.UserID)
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user_id"))
	}

	chat, err := h.svc.CreateDirectChat(c.Context(), uid, otherID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, chat)
}

func (h *ChatHandler) CreateChat(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		Type        string   `json:"type"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		MemberIDs   []string `json:"member_ids"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	memberIDs := make([]uuid.UUID, 0, len(req.MemberIDs))
	for _, s := range req.MemberIDs {
		id, err := uuid.Parse(s)
		if err != nil {
			return response.Error(c, apperror.BadRequest("Invalid member_id: "+s))
		}
		memberIDs = append(memberIDs, id)
	}

	chat, err := h.svc.CreateChat(c.Context(), uid, req.Type, req.Name, req.Description, memberIDs)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, chat)
}

func (h *ChatHandler) UpdateChat(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		AvatarURL   *string `json:"avatar_url"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	chat, err := h.svc.UpdateChat(c.Context(), chatID, uid, req.Name, req.Description, req.AvatarURL)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, chat)
}

func (h *ChatHandler) DeleteChat(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	if err := h.svc.DeleteChat(c.Context(), chatID, uid); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

func (h *ChatHandler) AddMembers(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	var req struct {
		UserIDs []string `json:"user_ids"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	newMemberIDs := make([]uuid.UUID, 0, len(req.UserIDs))
	for _, s := range req.UserIDs {
		id, err := uuid.Parse(s)
		if err != nil {
			return response.Error(c, apperror.BadRequest("Invalid user_id: "+s))
		}
		newMemberIDs = append(newMemberIDs, id)
	}

	if err := h.svc.AddMembers(c.Context(), chatID, uid, newMemberIDs); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

func (h *ChatHandler) RemoveMember(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	targetID, err := uuid.Parse(c.Params("userId"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	if err := h.svc.RemoveMember(c.Context(), chatID, uid, targetID); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

func (h *ChatHandler) UpdateMemberRole(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	targetID, err := uuid.Parse(c.Params("userId"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	var req struct {
		Role        string  `json:"role"`
		Permissions int64   `json:"permissions"`
		CustomTitle *string `json:"custom_title"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if err := h.svc.UpdateMemberRole(c.Context(), chatID, uid, targetID, req.Role, req.Permissions, req.CustomTitle); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

func (h *ChatHandler) GetMember(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	targetID, err := uuid.Parse(c.Params("userId"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	member, err := h.svc.GetMember(c.Context(), chatID, uid, targetID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, member)
}

func (h *ChatHandler) GetAdmins(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	admins, err := h.svc.GetAdmins(c.Context(), chatID, uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, admins)
}

func (h *ChatHandler) UpdateDefaultPermissions(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	var req struct {
		Permissions int64 `json:"permissions"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if err := h.svc.UpdateDefaultPermissions(c.Context(), chatID, uid, req.Permissions); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

func (h *ChatHandler) UpdateMemberPermissions(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	targetID, err := uuid.Parse(c.Params("userId"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	var req struct {
		Permissions int64 `json:"permissions"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if err := h.svc.UpdateMemberPermissions(c.Context(), chatID, uid, targetID, req.Permissions); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

func (h *ChatHandler) SetSlowMode(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	var req struct {
		Seconds int `json:"seconds"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if err := h.svc.SetSlowMode(c.Context(), chatID, uid, req.Seconds); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

func (h *ChatHandler) GetChat(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	chat, err := h.svc.GetChat(c.Context(), chatID, uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, chat)
}

func (h *ChatHandler) GetMembers(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	if q := c.Query("q"); q != "" {
		limit := c.QueryInt("limit", 50)
		if limit > 100 {
			limit = 100
		}
		members, err := h.svc.SearchMembers(c.Context(), chatID, uid, q, limit)
		if err != nil {
			return response.Error(c, err)
		}
		return response.JSON(c, fiber.StatusOK, members)
	}

	cursor := c.Query("cursor")
	limit := c.QueryInt("limit", 50)
	if limit > 100 {
		limit = 100
	}

	members, nextCursor, hasMore, err := h.svc.GetMembers(c.Context(), chatID, uid, cursor, limit)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Paginated(c, members, nextCursor, hasMore)
}

// GetMemberIDs returns just the user IDs of a chat (internal, for gateway typing fanout).
func (h *ChatHandler) GetMemberIDs(c *fiber.Ctx) error {
	callerID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	// Verify caller is a member of this chat
	isMember, err := h.svc.IsMember(c.Context(), chatID, callerID)
	if err != nil {
		return response.Error(c, err)
	}
	if !isMember {
		return response.Error(c, apperror.Forbidden("Not a member of this chat"))
	}

	ids, err := h.svc.GetMemberIDs(c.Context(), chatID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"member_ids": ids})
}

func (h *ChatHandler) UpdateChatPhoto(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	var req struct {
		AvatarURL string `json:"avatar_url"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	if req.AvatarURL == "" {
		return response.Error(c, apperror.BadRequest("avatar_url is required"))
	}

	chat, err := h.svc.UpdateChat(c.Context(), chatID, uid, nil, nil, &req.AvatarURL)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, chat)
}

func (h *ChatHandler) DeleteChatPhoto(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	chat, err := h.svc.ClearChatPhoto(c.Context(), chatID, uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, chat)
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
