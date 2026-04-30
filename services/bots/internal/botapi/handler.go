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
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/validator"
	"github.com/mst-corp/orbit/services/bots/internal/client"
	"github.com/mst-corp/orbit/services/bots/internal/model"
	"github.com/redis/go-redis/v9"
)

const botAPIRateLimitPerSec = 30
const botAPIIPRateLimitPerSec = 60

var botAPICommandNameRegex = regexp.MustCompile(`^[a-z0-9_]+$`)

var botRateLimitScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
local ttl = redis.call('TTL', KEYS[1])
return {count, ttl}
`)

var ipRateLimitScript = redis.NewScript(`
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
	// UpdateBotProfile updates the public-facing fields a bot can change about
	// itself via the Bot API: display name, description, short description.
	// Each pointer is only applied when non-nil — passing nil for all is a no-op.
	UpdateBotProfile(ctx context.Context, botID uuid.UUID, name, description, shortDescription *string) (*model.Bot, error)
}

// CommandStore manages bot slash commands.
type CommandStore interface {
	SetCommands(ctx context.Context, botID uuid.UUID, commands []model.BotCommand) error
	GetCommands(ctx context.Context, botID uuid.UUID) ([]model.BotCommand, error)
	DeleteAllForBot(ctx context.Context, botID uuid.UUID) error
}

type UpdateQueue interface {
	Pop(ctx context.Context, botID uuid.UUID, limit int, timeout time.Duration) ([]Update, error)
	Ack(botID uuid.UUID, offset int64) error
}

type BotAPIHandler struct {
	svc              BotService
	msgClient        *client.MessagingClient
	mediaClient      *client.MediaClient
	redis            *redis.Client
	updateQueue      UpdateQueue
	commandStore     CommandStore
	encryptionKey    []byte
	fileIDCodec      *FileIDCodec
	logger           *slog.Logger
	webhookAllowList []string // nil = allow all (dev mode)
}

func NewBotAPIHandler(svc BotService, msgClient *client.MessagingClient, mediaClient *client.MediaClient, encryptionKey []byte, logger *slog.Logger) *BotAPIHandler {
	return &BotAPIHandler{
		svc:           svc,
		msgClient:     msgClient,
		mediaClient:   mediaClient,
		encryptionKey: encryptionKey,
		fileIDCodec:   NewFileIDCodec(encryptionKey),
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

func (h *BotAPIHandler) WithCommandStore(cs CommandStore) *BotAPIHandler {
	h.commandStore = cs
	return h
}

// WithWebhookAllowList sets the allowed webhook hostnames.
// Each entry may be a plain hostname ("mst.local") or a wildcard subdomain ("*.saturn.ac").
// If list is empty or nil, all hosts are allowed (dev mode).
func (h *BotAPIHandler) WithWebhookAllowList(list []string) *BotAPIHandler {
	h.webhookAllowList = list
	return h
}

// IPRateLimitMiddleware returns a Fiber middleware that enforces a per-IP rate limit
// of botAPIIPRateLimitPerSec requests per second across all Bot API routes.
// It is a no-op when Redis is not configured.
func (h *BotAPIHandler) IPRateLimitMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if h.redis == nil {
			return c.Next()
		}
		ip := c.IP()
		if ip == "" {
			return c.Next()
		}
		key := fmt.Sprintf("ratelimit:botapi:ip:%s", ip)
		result, err := ipRateLimitScript.Run(c.Context(), h.redis, []string{key}, 1).Int64Slice()
		if err != nil {
			h.logger.Error("bot API IP rate limiter Redis error", "ip", ip, "error", err)
			return c.Next() // fail-open on Redis error
		}
		count := int(result[0])
		ttlSec := int(result[1])
		if count > botAPIIPRateLimitPerSec {
			retryAfter := ttlSec
			if retryAfter <= 0 {
				retryAfter = 1
			}
			c.Set("Retry-After", strconv.Itoa(retryAfter))
			return apperror.TooManyRequests("IP rate limit exceeded (60 req/sec)")
		}
		return c.Next()
	}
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
	router.Post("/sendMediaGroup", h.sendMediaGroup)
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
	router.Get("/getChat", h.getChat)
	router.Get("/getChatMember", h.getChatMember)
	router.Get("/getChatAdministrators", h.getChatAdministrators)
	router.Get("/getChatMemberCount", h.getChatMemberCount)
	router.Post("/sendChatAction", h.sendChatAction)
	router.Post("/pinChatMessage", h.pinChatMessage)
	router.Post("/unpinChatMessage", h.unpinChatMessage)
	router.Post("/setMyCommands", h.setMyCommands)
	router.Get("/getMyCommands", h.getMyCommands)
	router.Post("/deleteMyCommands", h.deleteMyCommands)
	router.Post("/banChatMember", h.banChatMember)
	router.Post("/restrictChatMember", h.restrictChatMember)
	router.Get("/getFile", h.getFile)
	router.Get("/file/:file_id", h.downloadFile)

	router.Post("/setMyName", h.setMyName)
	router.Get("/getMyName", h.getMyName)
	router.Post("/setMyDescription", h.setMyDescription)
	router.Get("/getMyDescription", h.getMyDescription)
	router.Post("/setMyShortDescription", h.setMyShortDescription)
	router.Get("/getMyShortDescription", h.getMyShortDescription)
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

	finalText, entities, err := resolveTextAndEntities(req.Text, req.ParseMode, req.Entities)
	if err != nil {
		return botError(c, err)
	}
	entitiesJSON, err := encodeEntities(entities)
	if err != nil {
		return botError(c, err)
	}

	message, err := h.msgClient.SendMessage(c.Context(), bot.UserID, chatID, finalText, "text", client.SendMessageOptions{
		ReplyMarkup: req.ReplyMarkup,
		ReplyToID:   replyToID,
		Entities:    entitiesJSON,
	})
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

	// JSON variant with `<fieldName>: "<file_id>"` reuses an existing media
	// item without re-uploading bytes. We branch early so multipart parsing
	// doesn't run on a JSON body.
	contentType := strings.ToLower(strings.TrimSpace(c.Get("Content-Type")))
	if strings.HasPrefix(contentType, "application/json") {
		return h.sendMediaByFileID(c, fieldName, msgType, bot)
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

	parseMode := c.FormValue("parse_mode", "")
	var captionEntities []MessageEntity
	if raw := c.FormValue("caption_entities", ""); raw != "" {
		if err := json.Unmarshal([]byte(raw), &captionEntities); err != nil {
			return botError(c, apperror.BadRequest("Invalid caption_entities"))
		}
	}
	finalCaption, entities, err := resolveTextAndEntities(caption, parseMode, captionEntities)
	if err != nil {
		return botError(c, err)
	}
	entitiesJSON, err := encodeEntities(entities)
	if err != nil {
		return botError(c, err)
	}

	var replyToID *uuid.UUID
	if rtID := c.FormValue("reply_to_message_id", ""); rtID != "" {
		parsed, parseErr := uuid.Parse(rtID)
		if parseErr == nil {
			replyToID = &parsed
		}
	}

	message, err := h.msgClient.SendMessage(c.Context(), bot.UserID, chatID, finalCaption, msgType, client.SendMessageOptions{
		ReplyMarkup: replyMarkup,
		ReplyToID:   replyToID,
		Entities:    entitiesJSON,
		MediaIDs:    []string{mediaID},
	})
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

const (
	mediaGroupMin = 2
	mediaGroupMax = 10
)

var allowedMediaGroupTypes = map[string]struct{}{
	"photo": {}, "video": {}, "document": {}, "audio": {},
}

// sendMediaGroup sends an album of 2-10 already-uploaded media items
// referenced by file_id. Returns an array of one message (a single
// messaging row holds the full media_ids set — clients render it as an
// album). Per-item captions beyond the first are ignored in v1.
func (h *BotAPIHandler) sendMediaGroup(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	var req SendMediaGroupRequest
	if err := c.BodyParser(&req); err != nil {
		return botError(c, apperror.BadRequest("Invalid request body"))
	}
	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return botError(c, err)
	}
	if len(req.Media) < mediaGroupMin || len(req.Media) > mediaGroupMax {
		return botError(c, apperror.BadRequest(
			fmt.Sprintf("media must contain between %d and %d items", mediaGroupMin, mediaGroupMax),
		))
	}

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}
	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopePostMessages); err != nil {
		return botError(c, err)
	}

	if h.fileIDCodec == nil {
		return botError(c, apperror.Internal("File codec not configured"))
	}

	mediaIDs := make([]string, 0, len(req.Media))
	for i, item := range req.Media {
		if _, ok := allowedMediaGroupTypes[item.Type]; !ok {
			return botError(c, apperror.BadRequest(
				fmt.Sprintf("media[%d].type must be one of photo|video|document|audio", i),
			))
		}
		if strings.TrimSpace(item.Media) == "" {
			return botError(c, apperror.BadRequest(
				fmt.Sprintf("media[%d].media (file_id) is required", i),
			))
		}

		mediaID, sourceChatID, decErr := h.fileIDCodec.Decode(item.Media, bot.ID)
		if decErr != nil {
			return botError(c, apperror.BadRequest(
				fmt.Sprintf("media[%d]: invalid file_id", i),
			))
		}
		installed, instErr := h.svc.IsBotInstalled(c.Context(), bot.ID, sourceChatID)
		if instErr != nil {
			return botError(c, instErr)
		}
		if !installed {
			return botError(c, apperror.Forbidden(
				fmt.Sprintf("media[%d]: bot is not installed in the source chat", i),
			))
		}
		mediaIDs = append(mediaIDs, mediaID.String())
	}

	first := req.Media[0]
	finalCaption, entities, err := resolveTextAndEntities(first.Caption, first.ParseMode, first.CaptionEntities)
	if err != nil {
		return botError(c, err)
	}
	entitiesJSON, err := encodeEntities(entities)
	if err != nil {
		return botError(c, err)
	}

	var replyToID *uuid.UUID
	if req.ReplyToMessageID != nil && strings.TrimSpace(*req.ReplyToMessageID) != "" {
		parsed, parseErr := uuid.Parse(*req.ReplyToMessageID)
		if parseErr != nil {
			return botError(c, apperror.BadRequest("Invalid reply_to_message_id"))
		}
		replyToID = &parsed
	}

	message, err := h.msgClient.SendMessage(
		c.Context(), bot.UserID, chatID, finalCaption, first.Type,
		client.SendMessageOptions{
			ReplyToID: replyToID,
			Entities:  entitiesJSON,
			MediaIDs:  mediaIDs,
		},
	)
	if err != nil {
		return botError(c, err)
	}

	// Bot API contract: sendMediaGroup returns an array of messages. We hold
	// them as one DB row with N media_ids, so we surface a single-element
	// array — keeps clients that iterate result[] working without changes.
	return botSuccess(c, []any{message})
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

	finalText, entities, err := resolveTextAndEntities(req.Text, req.ParseMode, req.Entities)
	if err != nil {
		return botError(c, err)
	}
	entitiesJSON, err := encodeEntities(entities)
	if err != nil {
		return botError(c, err)
	}

	message, err := h.msgClient.EditMessage(c.Context(), bot.UserID, messageID, finalText, req.ReplyMarkup, entitiesJSON)
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
	captionOverridden := req.Caption != nil && strings.TrimSpace(*req.Caption) != ""
	if captionOverridden {
		content = *req.Caption
	}

	// When the bot supplies a caption override, parse_mode applies to that
	// override. Otherwise we copy through entities verbatim from the source.
	var entitiesJSON json.RawMessage
	if captionOverridden {
		finalText, entities, parseErr := resolveTextAndEntities(content, req.ParseMode, req.CaptionEntities)
		if parseErr != nil {
			return botError(c, parseErr)
		}
		content = finalText
		entitiesJSON, err = encodeEntities(entities)
		if err != nil {
			return botError(c, err)
		}
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

	message, err := h.msgClient.SendMessage(c.Context(), bot.UserID, chatID, content, "text", client.SendMessageOptions{
		ReplyMarkup: req.ReplyMarkup,
		ReplyToID:   replyToID,
		Entities:    entitiesJSON,
	})
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

	finalCaption, entities, err := resolveTextAndEntities(req.Caption, req.ParseMode, req.CaptionEntities)
	if err != nil {
		return botError(c, err)
	}
	entitiesJSON, err := encodeEntities(entities)
	if err != nil {
		return botError(c, err)
	}

	message, err := h.msgClient.EditCaption(c.Context(), bot.UserID, messageID, finalCaption, req.ReplyMarkup, entitiesJSON)
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

func (h *BotAPIHandler) getChat(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	chatIDStr := c.Query("chat_id")
	if err := validator.RequireUUID(chatIDStr, "chat_id"); err != nil {
		return botError(c, err)
	}
	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}

	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopeReadChat); err != nil {
		return botError(c, err)
	}

	chat, err := h.msgClient.GetChat(c.Context(), bot.UserID, chatID)
	if err != nil {
		return botError(c, err)
	}

	return botSuccess(c, chat)
}

func (h *BotAPIHandler) getChatMember(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	chatIDStr := c.Query("chat_id")
	if err := validator.RequireUUID(chatIDStr, "chat_id"); err != nil {
		return botError(c, err)
	}
	userIDStr := c.Query("user_id")
	if err := validator.RequireUUID(userIDStr, "user_id"); err != nil {
		return botError(c, err)
	}

	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid user_id"))
	}

	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopeReadChat); err != nil {
		return botError(c, err)
	}

	member, err := h.msgClient.GetChatMember(c.Context(), bot.UserID, chatID, userID)
	if err != nil {
		return botError(c, err)
	}

	return botSuccess(c, member)
}

func (h *BotAPIHandler) getChatAdministrators(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	chatIDStr := c.Query("chat_id")
	if err := validator.RequireUUID(chatIDStr, "chat_id"); err != nil {
		return botError(c, err)
	}
	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}

	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopeReadChat); err != nil {
		return botError(c, err)
	}

	admins, err := h.msgClient.GetChatAdministrators(c.Context(), bot.UserID, chatID)
	if err != nil {
		return botError(c, err)
	}

	return botSuccess(c, admins)
}

func (h *BotAPIHandler) getChatMemberCount(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	chatIDStr := c.Query("chat_id")
	if err := validator.RequireUUID(chatIDStr, "chat_id"); err != nil {
		return botError(c, err)
	}
	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}

	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopeReadChat); err != nil {
		return botError(c, err)
	}

	count, err := h.msgClient.GetChatMemberCount(c.Context(), bot.UserID, chatID)
	if err != nil {
		return botError(c, err)
	}

	return botSuccess(c, count)
}

func (h *BotAPIHandler) sendChatAction(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	var req SendChatActionRequest
	if err := c.BodyParser(&req); err != nil {
		return botError(c, apperror.BadRequest("Invalid request body"))
	}
	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return botError(c, err)
	}
	if err := validator.RequireString(req.Action, "action", 1, 64); err != nil {
		return botError(c, err)
	}

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}

	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopePostMessages); err != nil {
		return botError(c, err)
	}

	// fire-and-forget: ignore error from typing endpoint
	_ = h.msgClient.SendChatAction(c.Context(), bot.UserID, chatID, req.Action)

	return botSuccess(c, true)
}

func (h *BotAPIHandler) pinChatMessage(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	var req PinChatMessageRequest
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
	messageID, err := uuid.Parse(req.MessageID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid message_id"))
	}

	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopePostMessages); err != nil {
		return botError(c, err)
	}

	if err := h.msgClient.PinMessage(c.Context(), bot.UserID, chatID, messageID); err != nil {
		return botError(c, err)
	}

	return botSuccess(c, true)
}

func (h *BotAPIHandler) unpinChatMessage(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	var req UnpinChatMessageRequest
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
	messageID, err := uuid.Parse(req.MessageID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid message_id"))
	}

	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopePostMessages); err != nil {
		return botError(c, err)
	}

	if err := h.msgClient.UnpinMessage(c.Context(), bot.UserID, chatID, messageID); err != nil {
		return botError(c, err)
	}

	return botSuccess(c, true)
}

func (h *BotAPIHandler) setMyCommands(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	if h.commandStore == nil {
		return botError(c, apperror.Internal("command store not configured"))
	}

	var req SetMyCommandsRequest
	if err := c.BodyParser(&req); err != nil {
		return botError(c, apperror.BadRequest("Invalid request body"))
	}
	if len(req.Commands) > 100 {
		return botError(c, apperror.BadRequest("Too many commands (max 100)"))
	}

	commands := make([]model.BotCommand, len(req.Commands))
	for i, item := range req.Commands {
		command := strings.TrimSpace(item.Command)
		description := strings.TrimSpace(item.Description)

		if err := validator.RequireString(command, "command", 1, 32); err != nil {
			return botError(c, err)
		}
		if !botAPICommandNameRegex.MatchString(command) {
			return botError(c, apperror.BadRequest("command must match ^[a-z0-9_]+$"))
		}
		if err := validator.RequireString(description, "description", 1, 256); err != nil {
			return botError(c, err)
		}

		commands[i] = model.BotCommand{
			BotID:       bot.ID,
			Command:     command,
			Description: description,
		}
	}

	if err := h.commandStore.SetCommands(c.Context(), bot.ID, commands); err != nil {
		return botError(c, err)
	}

	return botSuccess(c, true)
}

func (h *BotAPIHandler) getMyCommands(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	if h.commandStore == nil {
		return botSuccess(c, []model.BotCommand{})
	}

	commands, err := h.commandStore.GetCommands(c.Context(), bot.ID)
	if err != nil {
		return botError(c, err)
	}

	return botSuccess(c, commands)
}

func (h *BotAPIHandler) deleteMyCommands(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	if h.commandStore == nil {
		return botSuccess(c, true)
	}

	if err := h.commandStore.DeleteAllForBot(c.Context(), bot.ID); err != nil {
		return botError(c, err)
	}

	return botSuccess(c, true)
}

func (h *BotAPIHandler) banChatMember(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	var req BanChatMemberRequest
	if err := c.BodyParser(&req); err != nil {
		return botError(c, apperror.BadRequest("Invalid request body"))
	}
	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return botError(c, err)
	}
	if err := validator.RequireUUID(req.UserID, "user_id"); err != nil {
		return botError(c, err)
	}

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}
	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid user_id"))
	}

	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopeManageMembers); err != nil {
		return botError(c, err)
	}

	if err := h.msgClient.BanMember(c.Context(), bot.UserID, chatID, userID); err != nil {
		return botError(c, err)
	}

	return botSuccess(c, true)
}

func (h *BotAPIHandler) restrictChatMember(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	var req RestrictChatMemberRequest
	if err := c.BodyParser(&req); err != nil {
		return botError(c, apperror.BadRequest("Invalid request body"))
	}
	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return botError(c, err)
	}
	if err := validator.RequireUUID(req.UserID, "user_id"); err != nil {
		return botError(c, err)
	}

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}
	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid user_id"))
	}

	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopeManageMembers); err != nil {
		return botError(c, err)
	}

	if err := h.msgClient.RestrictMember(c.Context(), bot.UserID, chatID, userID, req.PermissionsMask); err != nil {
		return botError(c, err)
	}

	return botSuccess(c, true)
}

const (
	maxBotNameRunes             = 64
	maxBotDescriptionRunes      = 512
	maxBotShortDescriptionRunes = 120
)

// setMyName updates the bot's display name. Telegram allows 1-64 chars.
func (h *BotAPIHandler) setMyName(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	var req SetMyNameRequest
	if err := c.BodyParser(&req); err != nil {
		return botError(c, apperror.BadRequest("Invalid request body"))
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return botError(c, apperror.BadRequest("name is required"))
	}
	if utf8.RuneCountInString(name) > maxBotNameRunes {
		return botError(c, apperror.BadRequest("name is too long (max 64)"))
	}

	if _, err := h.svc.UpdateBotProfile(c.Context(), bot.ID, &name, nil, nil); err != nil {
		return botError(c, err)
	}
	return botSuccess(c, true)
}

func (h *BotAPIHandler) getMyName(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	return botSuccess(c, fiber.Map{"name": bot.DisplayName})
}

// setMyDescription updates the long bot description. Empty string clears it.
func (h *BotAPIHandler) setMyDescription(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	var req SetMyDescriptionRequest
	if err := c.BodyParser(&req); err != nil {
		return botError(c, apperror.BadRequest("Invalid request body"))
	}
	if utf8.RuneCountInString(req.Description) > maxBotDescriptionRunes {
		return botError(c, apperror.BadRequest("description is too long (max 512)"))
	}

	desc := req.Description
	if _, err := h.svc.UpdateBotProfile(c.Context(), bot.ID, nil, &desc, nil); err != nil {
		return botError(c, err)
	}
	return botSuccess(c, true)
}

func (h *BotAPIHandler) getMyDescription(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	desc := ""
	if bot.Description != nil {
		desc = *bot.Description
	}
	return botSuccess(c, fiber.Map{"description": desc})
}

// setMyShortDescription updates the short bot description (used in chat header).
// Empty string clears it.
func (h *BotAPIHandler) setMyShortDescription(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	var req SetMyShortDescriptionRequest
	if err := c.BodyParser(&req); err != nil {
		return botError(c, apperror.BadRequest("Invalid request body"))
	}
	if utf8.RuneCountInString(req.ShortDescription) > maxBotShortDescriptionRunes {
		return botError(c, apperror.BadRequest("short_description is too long (max 120)"))
	}

	short := req.ShortDescription
	if _, err := h.svc.UpdateBotProfile(c.Context(), bot.ID, nil, nil, &short); err != nil {
		return botError(c, err)
	}
	return botSuccess(c, true)
}

func (h *BotAPIHandler) getMyShortDescription(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	short := ""
	if bot.ShortDescription != nil {
		short = *bot.ShortDescription
	}
	return botSuccess(c, fiber.Map{"short_description": short})
}
