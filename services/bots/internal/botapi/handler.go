package botapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
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

type BotService interface {
	TokenValidator
	IsBotInstalled(ctx context.Context, botID, chatID uuid.UUID) (bool, error)
	SetWebhook(ctx context.Context, botID uuid.UUID, webhookURL, secretHash *string) (*model.Bot, error)
}

type UpdateQueue interface {
	Pop(ctx context.Context, botID uuid.UUID, limit int, timeout time.Duration) ([]Update, error)
	Ack(botID uuid.UUID, offset int64) error
}

type BotAPIHandler struct {
	svc           BotService
	msgClient     *client.MessagingClient
	redis         *redis.Client
	updateQueue   UpdateQueue
	encryptionKey []byte
	logger        *slog.Logger
}

func NewBotAPIHandler(svc BotService, msgClient *client.MessagingClient, encryptionKey []byte, logger *slog.Logger) *BotAPIHandler {
	return &BotAPIHandler{
		svc:           svc,
		msgClient:     msgClient,
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

func (h *BotAPIHandler) Register(router fiber.Router) {
	router.Get("/getMe", h.getMe)
	router.Post("/sendMessage", h.sendMessage)
	router.Post("/editMessageText", h.editMessageText)
	router.Post("/deleteMessage", h.deleteMessage)
	router.Post("/answerCallbackQuery", h.answerCallbackQuery)
	router.Post("/setWebhook", h.setWebhook)
	router.Post("/deleteWebhook", h.deleteWebhook)
	router.Post("/getWebhookInfo", h.getWebhookInfo)
	router.Post("/getUpdates", h.getUpdates)
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

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}

	installed, err := h.svc.IsBotInstalled(c.Context(), bot.ID, chatID)
	if err != nil {
		return botError(c, err)
	}
	if !installed {
		return botError(c, apperror.Forbidden("Bot is not installed in this chat"))
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

func (h *BotAPIHandler) editMessageText(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
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

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}

	installed, err := h.svc.IsBotInstalled(c.Context(), bot.ID, chatID)
	if err != nil {
		return botError(c, err)
	}
	if !installed {
		return botError(c, apperror.Forbidden("Bot is not installed in this chat"))
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

	installed, err := h.svc.IsBotInstalled(c.Context(), bot.ID, chatID)
	if err != nil {
		return botError(c, err)
	}
	if !installed {
		return botError(c, apperror.Forbidden("Bot is not installed in this chat"))
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
