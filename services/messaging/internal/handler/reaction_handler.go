package handler

import (
	"log/slog"
	"regexp"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

// ReactionHandler handles HTTP requests for message reactions.
type ReactionHandler struct {
	svc    *service.ReactionService
	logger *slog.Logger
}

// NewReactionHandler creates a new ReactionHandler.
func NewReactionHandler(svc *service.ReactionService, logger *slog.Logger) *ReactionHandler {
	return &ReactionHandler{svc: svc, logger: logger}
}

// Register registers reaction routes on the Fiber app.
func (h *ReactionHandler) Register(app fiber.Router) {
	app.Post("/messages/:id/reactions", h.AddReaction)
	app.Delete("/messages/:id/reactions", h.RemoveReaction)
	app.Get("/messages/:id/reactions", h.ListReactions)
	app.Get("/messages/:id/reactions/users", h.ListReactionUsers)
	app.Get("/chats/:id/available-reactions", h.GetAvailableReactions)
	app.Put("/chats/:id/available-reactions", h.SetAvailableReactions)
}

func (h *ReactionHandler) AddReaction(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	msgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid message ID"))
	}
	var body struct {
		Emoji string `json:"emoji"`
	}
	if err := c.BodyParser(&body); err != nil || body.Emoji == "" {
		return response.Error(c, apperror.BadRequest("Emoji is required"))
	}
	if len(body.Emoji) > 32 || looksLikeUUID(body.Emoji) {
		return response.Error(c, apperror.BadRequest("Invalid emoji"))
	}
	if err := h.svc.AddReaction(c.Context(), msgID, userID, body.Emoji); err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 201, fiber.Map{"ok": true})
}

func (h *ReactionHandler) RemoveReaction(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	msgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid message ID"))
	}
	var body struct {
		Emoji string `json:"emoji"`
	}
	if err := c.BodyParser(&body); err != nil || body.Emoji == "" {
		return response.Error(c, apperror.BadRequest("Emoji is required"))
	}
	if err := h.svc.RemoveReaction(c.Context(), msgID, userID, body.Emoji); err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, fiber.Map{"ok": true})
}

func (h *ReactionHandler) ListReactions(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	msgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid message ID"))
	}
	reactions, err := h.svc.ListReactions(c.Context(), msgID, userID)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, reactions)
}

func (h *ReactionHandler) ListReactionUsers(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	msgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid message ID"))
	}

	emoji := c.Query("emoji")
	cursor := c.Query("cursor")
	limit := c.QueryInt("limit", 50)

	reactions, nextCursor, hasMore, err := h.svc.ListReactionUsers(c.Context(), msgID, userID, emoji, cursor, limit)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Paginated(c, reactions, nextCursor, hasMore)
}

func (h *ReactionHandler) GetAvailableReactions(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	reactions, err := h.svc.GetAvailableReactions(c.Context(), chatID, userID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, 200, reactions)
}

func (h *ReactionHandler) SetAvailableReactions(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}
	var body struct {
		Mode   string   `json:"mode"`
		Emojis []string `json:"emojis"`
	}
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	if body.Mode == "" {
		return response.Error(c, apperror.BadRequest("Mode is required"))
	}
	if err := h.svc.SetAvailableReactions(c.Context(), chatID, userID, body.Mode, body.Emojis); err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, fiber.Map{"ok": true})
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func looksLikeUUID(s string) bool {
	return uuidPattern.MatchString(s)
}
