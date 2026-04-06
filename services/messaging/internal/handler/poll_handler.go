package handler

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

// PollHandler handles HTTP requests for polls.
type PollHandler struct {
	svc    *service.PollService
	logger *slog.Logger
}

// NewPollHandler creates a new PollHandler.
func NewPollHandler(svc *service.PollService, logger *slog.Logger) *PollHandler {
	return &PollHandler{svc: svc, logger: logger}
}

// Register registers poll routes.
func (h *PollHandler) Register(app fiber.Router) {
	app.Post("/messages/:id/poll/vote", h.Vote)
	app.Delete("/messages/:id/poll/vote", h.Unvote)
	app.Post("/messages/:id/poll/close", h.ClosePoll)
	app.Get("/messages/:id/poll/voters", h.GetVoters)
}

func (h *PollHandler) Vote(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	msgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid message ID"))
	}
	var body struct {
		OptionIDs []string `json:"option_ids"`
	}
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	if len(body.OptionIDs) == 0 {
		return response.Error(c, apperror.BadRequest("At least one option_id is required"))
	}
	optionIDs := make([]uuid.UUID, len(body.OptionIDs))
	for i, id := range body.OptionIDs {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return response.Error(c, apperror.BadRequest("Invalid option ID"))
		}
		optionIDs[i] = parsed
	}
	poll, err := h.svc.Vote(c.Context(), msgID, userID, optionIDs)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, poll)
}

func (h *PollHandler) Unvote(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	msgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid message ID"))
	}
	poll, err := h.svc.Unvote(c.Context(), msgID, userID)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, poll)
}

func (h *PollHandler) ClosePoll(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	msgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid message ID"))
	}
	poll, err := h.svc.ClosePoll(c.Context(), msgID, userID)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, poll)
}

func (h *PollHandler) GetVoters(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	msgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid message ID"))
	}
	optionID, err := uuid.Parse(c.Query("option_id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid option ID"))
	}

	limit := c.QueryInt("limit", 50)
	cursor := c.Query("cursor")
	voters, nextCursor, hasMore, err := h.svc.GetPollVoters(c.Context(), msgID, userID, optionID, limit, cursor)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Paginated(c, voters, nextCursor, hasMore)
}
