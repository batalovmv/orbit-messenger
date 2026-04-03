package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

type MessageHandler struct {
	svc            *service.MessageService
	pollSvc        *service.PollService
	reactionSvc    *service.ReactionService
	scheduledSvc   *service.ScheduledMessageService
	linkPreviewSvc *service.LinkPreviewService
	logger         *slog.Logger
}

type sendMessageRequest struct {
	Content          string          `json:"content"`
	Question         string          `json:"question"`
	Entities         json.RawMessage `json:"entities"`
	Solution         string          `json:"solution"`
	SolutionEntities json.RawMessage `json:"solution_entities"`
	ReplyToID        *string         `json:"reply_to_id"`
	Type             string          `json:"type"`
	MediaIDs         []string        `json:"media_ids"`
	GroupedID        *string         `json:"grouped_id"`
	IsSpoiler        bool            `json:"is_spoiler"`
	Options          []string        `json:"options"`
	IsAnonymous      *bool           `json:"is_anonymous"`
	IsMultiple       bool            `json:"is_multiple"`
	IsQuiz           bool            `json:"is_quiz"`
	CorrectOption    *int            `json:"correct_option"`
}

func NewMessageHandler(
	svc *service.MessageService,
	pollSvc *service.PollService,
	scheduledSvc *service.ScheduledMessageService,
	linkPreviewSvc *service.LinkPreviewService,
	logger *slog.Logger,
) *MessageHandler {
	return &MessageHandler{
		svc:            svc,
		pollSvc:        pollSvc,
		scheduledSvc:   scheduledSvc,
		linkPreviewSvc: linkPreviewSvc,
		logger:         logger,
	}
}

func (h *MessageHandler) SetReactionService(reactionSvc *service.ReactionService) *MessageHandler {
	h.reactionSvc = reactionSvc
	return h
}

func (h *MessageHandler) Register(app fiber.Router) {
	// Message endpoints under /chats/:id
	app.Get("/chats/:id/messages", h.ListMessages)
	app.Get("/chats/:id/history", h.FindByDate)
	app.Post("/chats/:id/messages", h.SendMessage)
	app.Get("/messages/:id", h.GetMessage)

	// Pin endpoints
	app.Get("/chats/:id/media", h.ListSharedMedia)
	app.Get("/chats/:id/pinned", h.ListPinned)
	app.Post("/chats/:id/pin/:messageId", h.PinMessage)
	app.Delete("/chats/:id/pin/:messageId", h.UnpinMessage)
	app.Delete("/chats/:id/pin", h.UnpinAll)

	// Read receipts
	app.Patch("/chats/:id/read", h.MarkRead)
	app.Post("/messages/:id/view", h.ViewOneTimeMessage)

	// Message actions (no chat prefix)
	app.Patch("/messages/:id", h.EditMessage)
	app.Delete("/messages/:id", h.DeleteMessage)
	app.Post("/messages/forward", h.ForwardMessages)

	// Link preview
	app.Get("/messages/link-preview", h.GetLinkPreview)
}

func (h *MessageHandler) ListMessages(c *fiber.Ctx) error {
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

	msgs, nextCursor, hasMore, err := h.svc.ListMessages(c.Context(), chatID, uid, cursor, limit)
	if err != nil {
		return response.Error(c, err)
	}

	if err := h.hydrateMessages(c.Context(), uid, msgs); err != nil {
		return response.Error(c, err)
	}

	return response.Paginated(c, msgs, nextCursor, hasMore)
}

func (h *MessageHandler) ListSharedMedia(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	mediaType := c.Query("type") // photo, video, file, voice, etc. Empty = all
	cursor := c.Query("cursor")
	limit := c.QueryInt("limit", 20)
	if limit > 50 {
		limit = 50
	}

	items, nextCursor, hasMore, err := h.svc.ListSharedMedia(c.Context(), chatID, uid, mediaType, cursor, limit)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Paginated(c, items, nextCursor, hasMore)
}

func (h *MessageHandler) FindByDate(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	dateStr := c.Query("date")
	if dateStr == "" {
		return response.Error(c, apperror.BadRequest("date query parameter is required"))
	}
	date, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid date format (use RFC3339)"))
	}

	limit := c.QueryInt("limit", 50)
	if limit > 100 {
		limit = 100
	}
	msgs, nextCursor, hasMore, err := h.svc.FindByDate(c.Context(), chatID, uid, date, limit)
	if err != nil {
		return response.Error(c, err)
	}

	if err := h.hydrateMessages(c.Context(), uid, msgs); err != nil {
		return response.Error(c, err)
	}
	return response.Paginated(c, msgs, nextCursor, hasMore)
}

func (h *MessageHandler) GetMessage(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	msgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid message ID"))
	}

	msg, err := h.svc.GetMessage(c.Context(), msgID, uid)
	if err != nil {
		return response.Error(c, err)
	}

	msgs := []model.Message{*msg}
	if err := h.hydrateMessages(c.Context(), uid, msgs); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, msgs[0])
}

func (h *MessageHandler) SendMessage(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	var req sendMessageRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	var replyTo *uuid.UUID
	if req.ReplyToID != nil && *req.ReplyToID != "" {
		id, err := uuid.Parse(*req.ReplyToID)
		if err != nil {
			return response.Error(c, apperror.BadRequest("Invalid reply_to_id"))
		}
		replyTo = &id
	}

	if len(req.MediaIDs) > 10 {
		return response.Error(c, apperror.BadRequest("Too many media attachments (max 10)"))
	}
	if len(req.SolutionEntities) > 65536 {
		return response.Error(c, apperror.BadRequest("solution_entities too large (max 64KB)"))
	}

	scheduledAtRaw := strings.TrimSpace(c.Query("scheduled_at"))
	if scheduledAtRaw != "" {
		return h.scheduleMessage(c, chatID, uid, req, replyTo, scheduledAtRaw)
	}

	if req.Type == "poll" {
		return h.sendPoll(c, chatID, uid, req, replyTo)
	}

	// Content is required unless media_ids are provided.
	if req.Content == "" && len(req.MediaIDs) == 0 {
		return response.Error(c, apperror.BadRequest("Content or media_ids is required"))
	}

	// Input length limits.
	if len(req.Content) > 4096 {
		return response.Error(c, apperror.BadRequest("Content too long (max 4096 characters)"))
	}
	if len(req.Entities) > 65536 {
		return response.Error(c, apperror.BadRequest("Entities too large (max 64KB)"))
	}

	validTypes := map[string]bool{
		"": true, "text": true, "photo": true, "video": true, "file": true,
		"voice": true, "video_note": true, "sticker": true, "gif": true, "system": true,
	}
	if !validTypes[req.Type] {
		return response.Error(c, apperror.BadRequest("Invalid message type"))
	}

	if len(req.MediaIDs) > 0 {
		var mediaUUIDs []uuid.UUID
		for _, idStr := range req.MediaIDs {
			id, err := uuid.Parse(idStr)
			if err != nil {
				return response.Error(c, apperror.BadRequest("Invalid media_id: "+idStr))
			}
			mediaUUIDs = append(mediaUUIDs, id)
		}

		msg, err := h.svc.SendMediaMessage(c.Context(), chatID, uid,
			req.Content, req.Entities, replyTo, req.Type,
			mediaUUIDs, req.IsSpoiler, req.GroupedID)
		if err != nil {
			return response.Error(c, err)
		}
		return response.JSON(c, fiber.StatusCreated, msg)
	}

	msg, err := h.svc.SendMessage(c.Context(), chatID, uid, req.Content, req.Entities, replyTo, req.Type)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, msg)
}

func (h *MessageHandler) sendPoll(
	c *fiber.Ctx,
	chatID uuid.UUID,
	userID uuid.UUID,
	req sendMessageRequest,
	replyTo *uuid.UUID,
) error {
	if h.pollSvc == nil {
		return response.Error(c, apperror.Internal("Poll service is not configured"))
	}
	if len(req.MediaIDs) > 0 {
		return response.Error(c, apperror.BadRequest("Poll messages do not support media attachments"))
	}
	if replyTo != nil {
		return response.Error(c, apperror.BadRequest("Poll messages do not support reply_to_id yet"))
	}

	question := strings.TrimSpace(req.Question)
	if question == "" {
		question = strings.TrimSpace(req.Content)
	}
	if len(question) > 4096 {
		return response.Error(c, apperror.BadRequest("Content too long (max 4096 characters)"))
	}

	isAnonymous := true
	if req.IsAnonymous != nil {
		isAnonymous = *req.IsAnonymous
	}
	solution := strings.TrimSpace(req.Solution)
	var solutionPtr *string
	if solution != "" {
		solutionPtr = &solution
	}

	poll, msg, err := h.pollSvc.CreatePoll(
		c.Context(),
		chatID,
		userID,
		question,
		req.Options,
		isAnonymous,
		req.IsMultiple,
		req.IsQuiz,
		req.CorrectOption,
		solutionPtr,
		req.SolutionEntities,
	)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, fiber.Map{
		"message": msg,
		"poll":    poll,
	})
}

func (h *MessageHandler) scheduleMessage(
	c *fiber.Ctx,
	chatID uuid.UUID,
	userID uuid.UUID,
	req sendMessageRequest,
	replyTo *uuid.UUID,
	scheduledAtRaw string,
) error {
	if h.scheduledSvc == nil {
		return response.Error(c, apperror.Internal("Scheduled message service is not configured"))
	}
	if len(req.Entities) > 65536 {
		return response.Error(c, apperror.BadRequest("Entities too large (max 64KB)"))
	}

	validTypes := map[string]bool{
		"": true, "text": true, "photo": true, "video": true, "file": true,
		"voice": true, "video_note": true, "sticker": true, "gif": true, "poll": true, "system": true,
	}
	if !validTypes[req.Type] {
		return response.Error(c, apperror.BadRequest("Invalid scheduled message type"))
	}

	if req.Type == "poll" && replyTo != nil {
		return response.Error(c, apperror.BadRequest("Poll messages do not support reply_to_id yet"))
	}

	scheduledAt, err := time.Parse(time.RFC3339, scheduledAtRaw)
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid scheduled_at format (use RFC3339)"))
	}

	msgType := req.Type
	if msgType == "" {
		msgType = "text"
	}

	var mediaUUIDs []uuid.UUID
	for _, idStr := range req.MediaIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			return response.Error(c, apperror.BadRequest("Invalid media_id: "+idStr))
		}
		mediaUUIDs = append(mediaUUIDs, id)
	}

	var pollPayload *model.ScheduledPollPayload
	if req.Type == "poll" {
		question := strings.TrimSpace(req.Question)
		if question == "" {
			question = strings.TrimSpace(req.Content)
		}
		isAnonymous := true
		if req.IsAnonymous != nil {
			isAnonymous = *req.IsAnonymous
		}
		solution := strings.TrimSpace(req.Solution)
		var solutionPtr *string
		if solution != "" {
			solutionPtr = &solution
		}
		pollPayload = &model.ScheduledPollPayload{
			Question:         question,
			Options:          req.Options,
			IsAnonymous:      isAnonymous,
			IsMultiple:       req.IsMultiple,
			IsQuiz:           req.IsQuiz,
			CorrectOption:    req.CorrectOption,
			Solution:         solutionPtr,
			SolutionEntities: req.SolutionEntities,
		}
	}

	msg, err := h.scheduledSvc.Schedule(
		c.Context(),
		chatID,
		userID,
		service.ScheduleMessageInput{
			Content:     req.Content,
			Entities:    req.Entities,
			ReplyToID:   replyTo,
			Type:        msgType,
			MediaIDs:    mediaUUIDs,
			IsSpoiler:   req.IsSpoiler,
			Poll:        pollPayload,
			ScheduledAt: scheduledAt,
		},
	)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, msg)
}

func (h *MessageHandler) EditMessage(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	msgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid message ID"))
	}

	var req struct {
		Content  string          `json:"content"`
		Entities json.RawMessage `json:"entities"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	if req.Content == "" {
		return response.Error(c, apperror.BadRequest("Content is required"))
	}
	if len(req.Content) > 4096 {
		return response.Error(c, apperror.BadRequest("Content too long (max 4096 characters)"))
	}
	if len(req.Entities) > 65536 {
		return response.Error(c, apperror.BadRequest("Entities too large (max 64KB)"))
	}

	msg, err := h.svc.EditMessage(c.Context(), msgID, uid, req.Content, req.Entities)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, msg)
}

func (h *MessageHandler) DeleteMessage(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	msgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid message ID"))
	}

	if err := h.svc.DeleteMessage(c.Context(), msgID, uid); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"message": "Message deleted"})
}

func (h *MessageHandler) ForwardMessages(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		MessageIDs []string `json:"message_ids"`
		ToChatID   string   `json:"to_chat_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if len(req.MessageIDs) == 0 {
		return response.Error(c, apperror.BadRequest("message_ids is required"))
	}
	if len(req.MessageIDs) > 100 {
		return response.Error(c, apperror.BadRequest("Cannot forward more than 100 messages at once"))
	}

	toChatID, err := uuid.Parse(req.ToChatID)
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid to_chat_id"))
	}

	var msgIDs []uuid.UUID
	for _, idStr := range req.MessageIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			return response.Error(c, apperror.BadRequest("Invalid message ID: "+idStr))
		}
		msgIDs = append(msgIDs, id)
	}

	msgs, err := h.svc.ForwardMessages(c.Context(), msgIDs, toChatID, uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, fiber.Map{"messages": msgs})
}

func (h *MessageHandler) PinMessage(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	msgID, err := uuid.Parse(c.Params("messageId"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid message ID"))
	}

	if err := h.svc.PinMessage(c.Context(), chatID, msgID, uid); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"message": "Message pinned"})
}

func (h *MessageHandler) UnpinMessage(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	msgID, err := uuid.Parse(c.Params("messageId"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid message ID"))
	}

	if err := h.svc.UnpinMessage(c.Context(), chatID, msgID, uid); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"message": "Message unpinned"})
}

func (h *MessageHandler) UnpinAll(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	if err := h.svc.UnpinAll(c.Context(), chatID, uid); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"message": "All messages unpinned"})
}

func (h *MessageHandler) ListPinned(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	msgs, err := h.svc.ListPinned(c.Context(), chatID, uid)
	if err != nil {
		return response.Error(c, err)
	}

	if err := h.hydrateMessages(c.Context(), uid, msgs); err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{"messages": msgs})
}

func (h *MessageHandler) MarkRead(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	var req struct {
		LastReadMessageID string `json:"last_read_message_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	msgID, err := uuid.Parse(req.LastReadMessageID)
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid last_read_message_id"))
	}

	if err := h.svc.MarkRead(c.Context(), chatID, uid, msgID); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"message": "Read pointer updated"})
}

func (h *MessageHandler) ViewOneTimeMessage(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	msgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid message ID"))
	}

	msg, err := h.svc.ViewOneTimeMessage(c.Context(), msgID, uid)
	if err != nil {
		return response.Error(c, err)
	}

	msgs := []model.Message{*msg}
	if err := h.hydrateMessages(c.Context(), uid, msgs); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, msgs[0])
}

func (h *MessageHandler) GetLinkPreview(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	if !h.linkPreviewSvc.CheckRateLimit(c.Context(), uid.String()) {
		return response.Error(c, apperror.TooManyRequests("Link preview rate limit exceeded"))
	}

	rawURL := c.Query("url")
	if rawURL == "" {
		return response.Error(c, apperror.BadRequest("url query parameter is required"))
	}

	preview, err := h.linkPreviewSvc.FetchPreview(c.Context(), rawURL)
	if err != nil {
		h.logger.Warn("link preview failed", "url", rawURL, "error", err)
		return response.JSON(c, fiber.StatusOK, fiber.Map{"preview": nil})
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"preview": preview})
}

func (h *MessageHandler) hydrateMessages(ctx context.Context, userID uuid.UUID, msgs []model.Message) error {
	h.svc.EnrichMessagesMedia(ctx, msgs)

	if h.reactionSvc != nil {
		if err := h.reactionSvc.HydrateMessageReactions(ctx, msgs); err != nil {
			return err
		}
	}
	if h.pollSvc != nil {
		if err := h.pollSvc.HydrateMessagePolls(ctx, userID, msgs); err != nil {
			return err
		}
	}

	return nil
}
