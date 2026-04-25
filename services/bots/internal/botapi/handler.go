// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/validator"
	"github.com/mst-corp/orbit/services/bots/internal/client"
	"github.com/mst-corp/orbit/services/bots/internal/model"
	"github.com/redis/go-redis/v9"
)

const botAPIRateLimitPerSec = 30

var botRateLimitScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
local ttl = redis.call('TTL', KEYS[1])
return {count, ttl}
`)

type BotService interface {
	TokenValidator
	IsBotInstalled(ctx context.Context, botID, chatID uuid.UUID) (bool, error)
	CheckBotScope(ctx context.Context, botID, chatID uuid.UUID, requiredScope int64) error
	SetWebhook(ctx context.Context, botID uuid.UUID, webhookURL, secretHash *string) (*model.Bot, error)
}

type UpdateQueue interface {
	Pop(ctx context.Context, botID uuid.UUID, limit int, timeout time.Duration) ([]Update, error)
	Ack(botID uuid.UUID, offset int64) error
}

type BotAPIHandler struct {
	svc           BotService
	msgClient     *client.MessagingClient
	mediaClient   *client.MediaClient
	redis         *redis.Client
	updateQueue   UpdateQueue
	encryptionKey []byte
	logger        *slog.Logger
}

func NewBotAPIHandler(svc BotService, msgClient *client.MessagingClient, mediaClient *client.MediaClient, encryptionKey []byte, logger *slog.Logger) *BotAPIHandler {
	return &BotAPIHandler{
		svc:           svc,
		msgClient:     msgClient,
		mediaClient:   mediaClient,
		encryptionKey: encryptionKey,
		logger:        logger,
	}
}

func (h *BotAPIHandler) WithRedis(redisClient *redis.Client) *BotAPIHandler {
	h.redis = redisClient
	return h
}

func (h *BotAPIHandler) WithUpdateQueue(updateQueue UpdateQueue) *BotAPIHandler {
	h.updateQueue = updateQueue
	return h
}

func (h *BotAPIHandler) checkRateLimit(c *fiber.Ctx, botID string) error {
	if h.redis == nil {
		return nil // rate limiting disabled if Redis not configured
	}
	key := fmt.Sprintf("ratelimit:botapi:%s", botID)
	result, err := botRateLimitScript.Run(c.Context(), h.redis, []string{key}, 1).Int64Slice()
	if err != nil {
		h.logger.Error("bot API rate limiter Redis error", "bot_id", botID, "error", err)
		return nil // fail-open on Redis error (don't block legitimate traffic)
	}
	count := int(result[0])
	ttlSec := int(result[1])
	if count > botAPIRateLimitPerSec {
		retryAfter := ttlSec
		if retryAfter <= 0 {
			retryAfter = 1
		}
		c.Set("Retry-After", strconv.Itoa(retryAfter))
		return apperror.TooManyRequests("Bot rate limit exceeded (30 req/sec)")
	}
	return nil
}

func (h *BotAPIHandler) Register(router fiber.Router) {
	router.Get("/getMe", h.getMe)
	router.Post("/sendMessage", h.sendMessage)
	router.Post("/sendPhoto", h.sendPhoto)
	router.Post("/sendDocument", h.sendDocument)
	router.Post("/sendVideo", h.sendVideo)
	router.Post("/sendAudio", h.sendAudio)
	router.Post("/sendVoice", h.sendVoice)
	router.Post("/editMessageText", h.editMessageText)
	router.Post("/deleteMessage", h.deleteMessage)
	router.Post("/answerCallbackQuery", h.answerCallbackQuery)
	router.Post("/setWebhook", h.setWebhook)
	router.Post("/deleteWebhook", h.deleteWebhook)
	router.Post("/getWebhookInfo", h.getWebhookInfo)
	router.Post("/getUpdates", h.getUpdates)
	router.Post("/copyMessage", h.copyMessage)
	router.Post("/forwardMessage", h.forwardMessage)
	router.Post("/editMessageReplyMarkup", h.editMessageReplyMarkup)
	router.Post("/editMessageCaption", h.editMessageCaption)
}

func (h *BotAPIHandler) getMe(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}

	return botSuccess(c, bot)
}

func (h *BotAPIHandler) sendMessage(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	var req SendMessageRequest
	if err := c.BodyParser(&req); err != nil {
		return botError(c, apperror.BadRequest("Invalid request body"))
	}
	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return botError(c, err)
	}
	if err := validator.RequireString(req.Text, "text", 1, 4096); err != nil {
		return botError(c, err)
	}
	if err := ValidateReplyMarkup(req.ReplyMarkup); err != nil {
		return botError(c, err)
	}

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}

	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopePostMessages); err != nil {
		return botError(c, err)
	}

	var replyToID *uuid.UUID
	if req.ReplyToMessageID != nil && strings.TrimSpace(*req.ReplyToMessageID) != "" {
		if err := validator.RequireUUID(*req.ReplyToMessageID, "reply_to_message_id"); err != nil {
			return botError(c, err)
		}
		parsed, err := uuid.Parse(*req.ReplyToMessageID)
		if err != nil {
			return botError(c, apperror.BadRequest("Invalid reply_to_message_id"))
		}
		replyToID = &parsed
	}

	message, err := h.msgClient.SendMessage(c.Context(), bot.UserID, chatID, req.Text, "text", req.ReplyMarkup, replyToID)
	if err != nil {
		return botError(c, err)
	}

	return botSuccess(c, message)
}

func (h *BotAPIHandler) sendMedia(c *fiber.Ctx, fieldName, msgType string) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	chatIDStr := c.FormValue("chat_id")
	if err := validator.RequireUUID(chatIDStr, "chat_id"); err != nil {
		return botError(c, err)
	}
	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}

	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopePostMessages); err != nil {
		return botError(c, err)
	}

	file, err := c.FormFile(fieldName)
	if err != nil {
		return botError(c, apperror.BadRequest("Missing "+fieldName+" file"))
	}

	f, err := file.Open()
	if err != nil {
		return botError(c, apperror.Internal("Failed to open uploaded file"))
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return botError(c, apperror.Internal("Failed to read uploaded file"))
	}

	if h.mediaClient == nil {
		return botError(c, apperror.Internal("Media service not configured"))
	}

	mediaID, err := h.mediaClient.UploadFile(c.Context(), bot.UserID, file.Filename, msgType, data)
	if err != nil {
		return botError(c, apperror.Internal("Failed to upload media: "+err.Error()))
	}

	caption := c.FormValue("caption", "")
	replyMarkupRaw := c.FormValue("reply_markup", "")
	var replyMarkup json.RawMessage
	if replyMarkupRaw != "" {
		replyMarkup = json.RawMessage(replyMarkupRaw)
	}
	if err := ValidateReplyMarkup(replyMarkup); err != nil {
		return botError(c, err)
	}

	var replyToID *uuid.UUID
	if rtID := c.FormValue("reply_to_message_id", ""); rtID != "" {
		parsed, parseErr := uuid.Parse(rtID)
		if parseErr == nil {
			replyToID = &parsed
		}
	}

	message, err := h.msgClient.SendMessage(c.Context(), bot.UserID, chatID, caption, msgType, replyMarkup, replyToID, mediaID)
	if err != nil {
		return botError(c, err)
	}

	return botSuccess(c, message)
}

func (h *BotAPIHandler) sendPhoto(c *fiber.Ctx) error {
	return h.sendMedia(c, "photo", "photo")
}

func (h *BotAPIHandler) sendDocument(c *fiber.Ctx) error {
	return h.sendMedia(c, "document", "document")
}

func (h *BotAPIHandler) sendVideo(c *fiber.Ctx) error {
	return h.sendMedia(c, "video", "video")
}

func (h *BotAPIHandler) sendAudio(c *fiber.Ctx) error {
	return h.sendMedia(c, "audio", "audio")
}

func (h *BotAPIHandler) sendVoice(c *fiber.Ctx) error {
	return h.sendMedia(c, "voice", "voice")
}

func (h *BotAPIHandler) editMessageText(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	var req EditMessageRequest
	if err := c.BodyParser(&req); err != nil {
		return botError(c, apperror.BadRequest("Invalid request body"))
	}
	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return botError(c, err)
	}
	if err := validator.RequireUUID(req.MessageID, "message_id"); err != nil {
		return botError(c, err)
	}
	if err := validator.RequireString(req.Text, "text", 1, 4096); err != nil {
		return botError(c, err)
	}
	if err := ValidateReplyMarkup(req.ReplyMarkup); err != nil {
		return botError(c, err)
	}

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}

	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopePostMessages); err != nil {
		return botError(c, err)
	}

	messageID, err := uuid.Parse(req.MessageID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid message_id"))
	}

	message, err := h.msgClient.EditMessage(c.Context(), bot.UserID, messageID, req.Text, req.ReplyMarkup)
	if err != nil {
		return botError(c, err)
	}

	return botSuccess(c, message)
}

func (h *BotAPIHandler) deleteMessage(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	var req DeleteMessageRequest
	if err := c.BodyParser(&req); err != nil {
		return botError(c, apperror.BadRequest("Invalid request body"))
	}
	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return botError(c, err)
	}
	if err := validator.RequireUUID(req.MessageID, "message_id"); err != nil {
		return botError(c, err)
	}

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}

	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopePostMessages); err != nil {
		return botError(c, err)
	}

	messageID, err := uuid.Parse(req.MessageID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid message_id"))
	}

	if err := h.msgClient.DeleteMessage(c.Context(), bot.UserID, messageID); err != nil {
		return botError(c, err)
	}

	return botSuccess(c, true)
}

func (h *BotAPIHandler) copyMessage(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	var req CopyMessageRequest
	if err := c.BodyParser(&req); err != nil {
		return botError(c, apperror.BadRequest("Invalid request body"))
	}
	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return botError(c, err)
	}
	if err := validator.RequireUUID(req.FromChatID, "from_chat_id"); err != nil {
		return botError(c, err)
	}
	if err := validator.RequireUUID(req.MessageID, "message_id"); err != nil {
		return botError(c, err)
	}
	if err := ValidateReplyMarkup(req.ReplyMarkup); err != nil {
		return botError(c, err)
	}

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}

	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopePostMessages); err != nil {
		return botError(c, err)
	}

	messageID, err := uuid.Parse(req.MessageID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid message_id"))
	}

	source, err := h.msgClient.GetMessage(c.Context(), bot.UserID, messageID)
	if err != nil {
		return botError(c, err)
	}

	content := source.Content
	if req.Caption != nil && strings.TrimSpace(*req.Caption) != "" {
		content = *req.Caption
	}

	var replyToID *uuid.UUID
	if req.ReplyToMessageID != nil && strings.TrimSpace(*req.ReplyToMessageID) != "" {
		if err := validator.RequireUUID(*req.ReplyToMessageID, "reply_to_message_id"); err != nil {
			return botError(c, err)
		}
		parsed, err := uuid.Parse(*req.ReplyToMessageID)
		if err != nil {
			return botError(c, apperror.BadRequest("Invalid reply_to_message_id"))
		}
		replyToID = &parsed
	}

	message, err := h.msgClient.SendMessage(c.Context(), bot.UserID, chatID, content, "text", req.ReplyMarkup, replyToID)
	if err != nil {
		return botError(c, err)
	}

	return botSuccess(c, message)
}

func (h *BotAPIHandler) forwardMessage(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	var req ForwardMessageRequest
	if err := c.BodyParser(&req); err != nil {
		return botError(c, apperror.BadRequest("Invalid request body"))
	}
	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return botError(c, err)
	}
	if err := validator.RequireUUID(req.FromChatID, "from_chat_id"); err != nil {
		return botError(c, err)
	}
	if err := validator.RequireUUID(req.MessageID, "message_id"); err != nil {
		return botError(c, err)
	}

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}

	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopePostMessages); err != nil {
		return botError(c, err)
	}

	messageID, err := uuid.Parse(req.MessageID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid message_id"))
	}

	message, err := h.msgClient.ForwardMessage(c.Context(), bot.UserID, messageID, chatID)
	if err != nil {
		return botError(c, err)
	}

	return botSuccess(c, message)
}

func (h *BotAPIHandler) editMessageReplyMarkup(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	var req EditReplyMarkupRequest
	if err := c.BodyParser(&req); err != nil {
		return botError(c, apperror.BadRequest("Invalid request body"))
	}
	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return botError(c, err)
	}
	if err := validator.RequireUUID(req.MessageID, "message_id"); err != nil {
		return botError(c, err)
	}
	if err := ValidateReplyMarkup(req.ReplyMarkup); err != nil {
		return botError(c, err)
	}

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}

	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopePostMessages); err != nil {
		return botError(c, err)
	}

	messageID, err := uuid.Parse(req.MessageID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid message_id"))
	}

	message, err := h.msgClient.EditReplyMarkup(c.Context(), bot.UserID, messageID, req.ReplyMarkup)
	if err != nil {
		return botError(c, err)
	}

	return botSuccess(c, message)
}

func (h *BotAPIHandler) editMessageCaption(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	var req EditCaptionRequest
	if err := c.BodyParser(&req); err != nil {
		return botError(c, apperror.BadRequest("Invalid request body"))
	}
	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return botError(c, err)
	}
	if err := validator.RequireUUID(req.MessageID, "message_id"); err != nil {
		return botError(c, err)
	}
	if err := validator.RequireString(req.Caption, "caption", 1, 1024); err != nil {
		return botError(c, err)
	}
	if err := ValidateReplyMarkup(req.ReplyMarkup); err != nil {
		return botError(c, err)
	}

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}

	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopePostMessages); err != nil {
		return botError(c, err)
	}

	messageID, err := uuid.Parse(req.MessageID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid message_id"))
	}

	message, err := h.msgClient.EditCaption(c.Context(), bot.UserID, messageID, req.Caption, req.ReplyMarkup)
	if err != nil {
		return botError(c, err)
	}

	return botSuccess(c, message)
}

func currentBot(c *fiber.Ctx) (*model.Bot, error) {
	botValue := c.Locals("bot")
	if botValue == nil {
		return nil, apperror.Unauthorized("Missing bot context")
	}
	bot, ok := botValue.(*model.Bot)
	if !ok || bot == nil {
		return nil, apperror.Unauthorized("Invalid bot context")
	}
	return bot, nil
}

func botSuccess(c *fiber.Ctx, result any) error {
	return c.Status(http.StatusOK).JSON(BotAPIResponse{OK: true, Result: result})
}

func botError(c *fiber.Ctx, err error) error {
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		return c.Status(appErr.Status).JSON(BotAPIResponse{
			OK:          false,
			Description: appErr.Message,
			ErrorCode:   appErr.Status,
		})
	}

	var clientErr *client.ClientError
	if errors.As(err, &clientErr) {
		return c.Status(clientErr.StatusCode).JSON(BotAPIResponse{
			OK:          false,
			Description: clientErr.Message,
			ErrorCode:   clientErr.StatusCode,
		})
	}

	return c.Status(http.StatusInternalServerError).JSON(BotAPIResponse{
		OK:          false,
		Description: "Internal server error",
		ErrorCode:   http.StatusInternalServerError,
	})
}
