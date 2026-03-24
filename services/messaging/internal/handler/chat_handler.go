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
	app.Post("/chats", h.CreateGroup)
	app.Get("/chats/:id", h.GetChat)
	app.Get("/chats/:id/members", h.GetMembers)
	app.Get("/chats/:id/member-ids", h.GetMemberIDs)
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

func (h *ChatHandler) CreateGroup(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	chat, err := h.svc.CreateGroup(c.Context(), uid, req.Name, req.Description)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, chat)
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
	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	ids, err := h.svc.GetMemberIDs(c.Context(), chatID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"member_ids": ids})
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
