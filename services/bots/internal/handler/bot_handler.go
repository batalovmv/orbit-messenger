// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/pkg/validator"
	"github.com/mst-corp/orbit/services/bots/internal/botapi"
	"github.com/mst-corp/orbit/services/bots/internal/model"
	"github.com/mst-corp/orbit/services/bots/internal/service"
	"github.com/mst-corp/orbit/services/bots/internal/store"
)

var botUsernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// BotFatherCallbackHandler handles inline keyboard callbacks for the BotFather system bot.
type BotFatherCallbackHandler interface {
	HandleCallback(ctx context.Context, callerID uuid.UUID, chatID uuid.UUID, queryID string, data string) map[string]any
	UserID() uuid.UUID
}

// BotFatherChatProvisioner lazily creates+pins the BotFather DM for a user.
// Wraps messaging-client calls so the handler stays HTTP-only.
type BotFatherChatProvisioner interface {
	EnsureChat(ctx context.Context, userID uuid.UUID) (chatID uuid.UUID, systemBotID uuid.UUID, err error)
}

// AuditLogger logs bot admin actions. Implementations must be non-fatal.
type AuditLogger interface {
	Log(ctx context.Context, entry model.AuditLogEntry) error
	ListByBot(ctx context.Context, botID uuid.UUID, limit int) ([]model.AuditLogEntry, error)
}

type BotHandler struct {
	svc           *service.BotService
	logger        *slog.Logger
	redis         *redis.Client
	webhookWorker *service.WebhookWorker
	updateQueue   *service.UpdateQueue
	installations store.InstallationStore
	encryptionKey []byte
	botFather     BotFatherCallbackHandler
	bfProvisioner BotFatherChatProvisioner
	auditStore    AuditLogger
}

func NewBotHandler(svc *service.BotService, logger *slog.Logger) *BotHandler {
	return &BotHandler{svc: svc, logger: logger}
}

// WithCallbackSupport adds callback delivery dependencies.
func (h *BotHandler) WithCallbackSupport(
	rdb *redis.Client,
	webhookWorker *service.WebhookWorker,
	updateQueue *service.UpdateQueue,
	installations store.InstallationStore,
	encryptionKey []byte,
) *BotHandler {
	h.redis = rdb
	h.webhookWorker = webhookWorker
	h.updateQueue = updateQueue
	h.installations = installations
	h.encryptionKey = encryptionKey
	return h
}

// SetBotFather sets the BotFather callback handler for inline keyboard interception.
func (h *BotHandler) SetBotFather(bf BotFatherCallbackHandler) {
	h.botFather = bf
}

// WithBotFatherChatProvisioner attaches the lazy-DM provisioner used by the
// /system/botfather/ensure-chat endpoint.
func (h *BotHandler) WithBotFatherChatProvisioner(p BotFatherChatProvisioner) *BotHandler {
	h.bfProvisioner = p
	return h
}

// WithAuditStore attaches an audit logger to the handler.
func (h *BotHandler) WithAuditStore(a AuditLogger) *BotHandler {
	h.auditStore = a
	return h
}

func (h *BotHandler) Register(router fiber.Router) {
	// Static paths before parameterized to avoid Fiber matching "by-user" or "callback" as :id
	router.Post("/bots/callback", h.sendCallback)
	router.Get("/bots/by-user/:userId", h.getBotByUserID)
	router.Get("/bots/check-username", h.checkBotUsername)

	router.Post("/bots", h.createBot)
	router.Get("/bots", h.listBots)
	router.Get("/bots/:id", h.getBot)
	router.Patch("/bots/:id", h.updateBot)
	router.Delete("/bots/:id", h.deleteBot)
	router.Post("/bots/:id/token/rotate", h.rotateToken)
	router.Put("/bots/:id/commands", h.setCommands)
	router.Get("/bots/:id/commands", h.getCommands)
	router.Post("/bots/:id/install", h.installBot)
	router.Delete("/bots/:id/install", h.uninstallBot)
	router.Get("/bots/:id/audit", h.listBotAudit)
	router.Get("/chats/:chatId/bots", h.listChatBots)
	router.Get("/chats/:chatId/bot-commands", h.listChatBotCommands)

	router.Post("/system/botfather/ensure-chat", h.ensureBotFatherChat)
}

func RequireInternalToken(secret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Get("X-User-ID")
		if userID == "" {
			return response.Error(c, apperror.Unauthorized("Missing user context"))
		}
		token := c.Get("X-Internal-Token")
		if secret == "" || token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
			return response.Error(c, apperror.Unauthorized("Invalid internal token"))
		}
		return c.Next()
	}
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

func getUserRole(c *fiber.Ctx) string {
	return strings.ToLower(strings.TrimSpace(c.Get("X-User-Role")))
}

func checkManageBotsPermission(c *fiber.Ctx) error {
	if _, err := getUserID(c); err != nil {
		return err
	}
	if !permissions.HasSysPermission(getUserRole(c), permissions.SysManageBots) {
		return apperror.Forbidden("Insufficient permissions")
	}
	return nil
}

func (h *BotHandler) createBot(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	if err := checkManageBotsPermission(c); err != nil {
		return response.Error(c, err)
	}

	var req model.CreateBotRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	if err := validateBotUsername(req.Username); err != nil {
		return response.Error(c, err)
	}
	if err := validator.RequireString(req.DisplayName, "display_name", 1, 128); err != nil {
		return response.Error(c, err)
	}
	if req.Description != "" {
		if err := validator.RequireString(req.Description, "description", 1, 512); err != nil {
			return response.Error(c, err)
		}
	}
	if req.ShortDescription != "" {
		if err := validator.RequireString(req.ShortDescription, "short_description", 1, 256); err != nil {
			return response.Error(c, err)
		}
	}

	bot, token, err := h.svc.CreateBot(c.Context(), userID, req)
	if err != nil {
		return response.Error(c, err)
	}

	h.logAudit(c.Context(), userID, &bot.ID, "create", c.IP(), c.Get("User-Agent"), map[string]any{"username": bot.Username})

	return response.JSON(c, fiber.StatusCreated, fiber.Map{
		"bot":   bot,
		"token": token,
	})
}

func (h *BotHandler) listBots(c *fiber.Ctx) error {
	if err := checkManageBotsPermission(c); err != nil {
		return response.Error(c, err)
	}

	limit := c.QueryInt("limit", 50)
	offset := c.QueryInt("offset", 0)
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var ownerID *uuid.UUID
	if ownerIDParam := strings.TrimSpace(c.Query("owner_id")); ownerIDParam != "" {
		if err := validator.RequireUUID(ownerIDParam, "owner_id"); err != nil {
			return response.Error(c, err)
		}
		parsed, err := uuid.Parse(ownerIDParam)
		if err != nil {
			return response.Error(c, apperror.BadRequest("Invalid owner_id"))
		}
		ownerID = &parsed
	}

	bots, total, err := h.svc.ListBots(c.Context(), ownerID, limit, offset)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"data":  bots,
		"total": total,
	})
}

func (h *BotHandler) getBot(c *fiber.Ctx) error {
	if err := checkManageBotsPermission(c); err != nil {
		return response.Error(c, err)
	}

	botID, err := parseUUIDParam(c, "id", "bot ID")
	if err != nil {
		return response.Error(c, err)
	}

	bot, err := h.svc.GetBot(c.Context(), botID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, bot)
}

func (h *BotHandler) updateBot(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	if err := checkManageBotsPermission(c); err != nil {
		return response.Error(c, err)
	}

	botID, err := parseUUIDParam(c, "id", "bot ID")
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		Username                *string           `json:"username"`
		DisplayName             *string           `json:"display_name"`
		Description             *string           `json:"description"`
		ShortDescription        *string           `json:"short_description"`
		AboutText               *string           `json:"about_text"`
		IsInline                *bool             `json:"is_inline"`
		InlinePlaceholder       *string           `json:"inline_placeholder"`
		IsPrivacyEnabled        *bool             `json:"is_privacy_enabled"`
		CanJoinGroups           *bool             `json:"can_join_groups"`
		CanReadAllGroupMessages *bool             `json:"can_read_all_group_messages"`
		MenuButton              *model.MenuButton `json:"menu_button"`
		ClearMenuButton         bool              `json:"clear_menu_button"`
		WebhookURL              *string           `json:"webhook_url"`
		IsActive                *bool             `json:"is_active"`
		ShareUserEmails         *bool             `json:"share_user_emails"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if req.Username != nil {
		if err := validateBotUsername(*req.Username); err != nil {
			return response.Error(c, err)
		}
	}
	if req.DisplayName != nil {
		if err := validator.RequireString(*req.DisplayName, "display_name", 1, 128); err != nil {
			return response.Error(c, err)
		}
	}
	if req.Description != nil && strings.TrimSpace(*req.Description) != "" {
		if err := validator.RequireString(*req.Description, "description", 1, 512); err != nil {
			return response.Error(c, err)
		}
	}
	if req.ShortDescription != nil && strings.TrimSpace(*req.ShortDescription) != "" {
		if err := validator.RequireString(*req.ShortDescription, "short_description", 1, 256); err != nil {
			return response.Error(c, err)
		}
	}
	if req.AboutText != nil && strings.TrimSpace(*req.AboutText) != "" {
		if err := validator.RequireString(*req.AboutText, "about_text", 1, 120); err != nil {
			return response.Error(c, err)
		}
	}
	if req.InlinePlaceholder != nil && strings.TrimSpace(*req.InlinePlaceholder) != "" {
		if err := validator.RequireString(*req.InlinePlaceholder, "inline_placeholder", 1, 64); err != nil {
			return response.Error(c, err)
		}
	}
	if req.MenuButton != nil {
		switch req.MenuButton.Type {
		case "default", "commands":
			// no extra fields required
		case "web_app":
			if strings.TrimSpace(req.MenuButton.WebAppURL) == "" {
				return response.Error(c, apperror.BadRequest("web_app menu button requires web_app_url"))
			}
		default:
			return response.Error(c, apperror.BadRequest("menu_button.type must be one of: default, commands, web_app"))
		}
	}
	if req.WebhookURL != nil {
		u := strings.TrimSpace(*req.WebhookURL)
		if u == "" {
			return response.Error(c, apperror.BadRequest("webhook_url must not be blank"))
		}
		if len(u) > 2048 {
			return response.Error(c, apperror.BadRequest("webhook_url is too long"))
		}
		parsedURL, parseErr := url.ParseRequestURI(u)
		if parseErr != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
			return response.Error(c, apperror.BadRequest("webhook_url must be a valid HTTPS URL"))
		}
		req.WebhookURL = &u
	}

	bot, err := h.svc.UpdateBot(c.Context(), userID, getUserRole(c), botID, service.UpdateBotInput{
		Username:                req.Username,
		DisplayName:             req.DisplayName,
		Description:             req.Description,
		ShortDescription:        req.ShortDescription,
		AboutText:               req.AboutText,
		IsInline:                req.IsInline,
		InlinePlaceholder:       req.InlinePlaceholder,
		IsPrivacyEnabled:        req.IsPrivacyEnabled,
		CanJoinGroups:           req.CanJoinGroups,
		CanReadAllGroupMessages: req.CanReadAllGroupMessages,
		MenuButton:              req.MenuButton,
		ClearMenuButton:         req.ClearMenuButton,
		WebhookURL:              req.WebhookURL,
		IsActive:                req.IsActive,
		ShareUserEmails:         req.ShareUserEmails,
	})
	if err != nil {
		return response.Error(c, err)
	}

	auditDetails := map[string]any{"bot_id": botID}
	if req.ShareUserEmails != nil {
		auditDetails["share_user_emails"] = *req.ShareUserEmails
	}
	h.logAudit(c.Context(), userID, &botID, "update", c.IP(), c.Get("User-Agent"), auditDetails)

	return response.JSON(c, fiber.StatusOK, bot)
}

func (h *BotHandler) deleteBot(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	if err := checkManageBotsPermission(c); err != nil {
		return response.Error(c, err)
	}

	botID, err := parseUUIDParam(c, "id", "bot ID")
	if err != nil {
		return response.Error(c, err)
	}

	if err := h.svc.DeleteBot(c.Context(), userID, getUserRole(c), botID); err != nil {
		return response.Error(c, err)
	}

	h.logAudit(c.Context(), userID, &botID, "delete", c.IP(), c.Get("User-Agent"), map[string]any{"bot_id": botID})

	return response.JSON(c, fiber.StatusOK, fiber.Map{"message": "Bot deleted"})
}

func parseUUIDParam(c *fiber.Ctx, name, label string) (uuid.UUID, error) {
	value := strings.TrimSpace(c.Params(name))
	if err := validator.RequireUUID(value, name); err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, apperror.BadRequest("Invalid " + label)
	}
	return id, nil
}

func validateBotUsername(username string) error {
	if err := validator.RequireString(username, "username", 3, 64); err != nil {
		return err
	}
	if !botUsernameRegex.MatchString(strings.TrimSpace(username)) {
		return apperror.BadRequest("username must contain only letters, numbers, or underscores")
	}
	return nil
}

// logAudit records a bot admin action. Always non-fatal — errors are logged, not returned.
func (h *BotHandler) logAudit(ctx context.Context, actorID uuid.UUID, botID *uuid.UUID, action, sourceIP, ua string, details map[string]any) {
	if h.auditStore == nil {
		return
	}
	raw, err := json.Marshal(details)
	if err != nil {
		raw = json.RawMessage("{}")
	}
	entry := model.AuditLogEntry{
		ActorID:   actorID,
		BotID:     botID,
		Action:    action,
		Details:   raw,
		SourceIP:  &sourceIP,
		UserAgent: &ua,
	}
	if err := h.auditStore.Log(ctx, entry); err != nil {
		h.logger.Error("audit log failed", "error", err, "action", action)
	}
}

func (h *BotHandler) listBotAudit(c *fiber.Ctx) error {
	if err := checkManageBotsPermission(c); err != nil {
		return response.Error(c, err)
	}
	botID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid bot ID"))
	}
	limit := c.QueryInt("limit", 50)
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if h.auditStore == nil {
		return response.JSON(c, fiber.StatusOK, fiber.Map{"entries": []any{}})
	}
	entries, err := h.auditStore.ListByBot(c.Context(), botID, limit)
	if err != nil {
		return response.Error(c, apperror.Internal("Failed to fetch audit log"))
	}
	if entries == nil {
		entries = []model.AuditLogEntry{}
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{"entries": entries})
}

// checkBotUsername reports whether a username is well-formed and not yet taken.
// Powers the live availability check in the bot creation wizard.
// Always returns 200 with {available, valid, reason?} so the UI can render
// a hint without juggling HTTP status codes.
func (h *BotHandler) checkBotUsername(c *fiber.Ctx) error {
	if _, err := getUserID(c); err != nil {
		return response.Error(c, err)
	}
	if err := checkManageBotsPermission(c); err != nil {
		return response.Error(c, err)
	}

	username := strings.TrimSpace(c.Query("username"))
	if err := validateBotUsername(username); err != nil {
		reason := "invalid_format"
		if username == "" {
			reason = "empty"
		}
		return response.JSON(c, fiber.StatusOK, fiber.Map{
			"available": false,
			"valid":     false,
			"reason":    reason,
		})
	}

	existing, err := h.svc.LookupBotByUsername(c.Context(), username)
	if err != nil {
		return response.Error(c, err)
	}
	if existing != nil {
		return response.JSON(c, fiber.StatusOK, fiber.Map{
			"available": false,
			"valid":     true,
			"reason":    "taken",
		})
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"available": true,
		"valid":     true,
	})
}

// ensureBotFatherChat lazily creates and pins the BotFather DM for the
// authenticated user. Idempotent — the underlying messaging endpoint already
// returns the existing chat when one is found.
func (h *BotHandler) ensureBotFatherChat(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	if h.bfProvisioner == nil {
		return response.Error(c, apperror.Internal("BotFather chat provisioner not configured"))
	}

	chatID, systemBotID, err := h.bfProvisioner.EnsureChat(c.Context(), userID)
	if err != nil {
		return response.Error(c, apperror.Internal("ensure botfather chat: "+err.Error()))
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"chat_id":       chatID.String(),
		"system_bot_id": systemBotID.String(),
	})
}

func (h *BotHandler) getBotByUserID(c *fiber.Ctx) error {
	userID, err := parseUUIDParam(c, "userId", "user ID")
	if err != nil {
		return response.Error(c, err)
	}

	bot, err := h.svc.GetBotByUserID(c.Context(), userID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, bot)
}

func (h *BotHandler) sendCallback(c *fiber.Ctx) error {
	if h.redis == nil || (h.webhookWorker == nil && h.updateQueue == nil) {
		return response.Error(c, apperror.Internal("Callback delivery not configured"))
	}

	callerID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}

	var req struct {
		MessageID string `json:"message_id"`
		ChatID    string `json:"chat_id"`
		ViaBotID  string `json:"via_bot_id"`
		Data      string `json:"data"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if req.MessageID == "" || req.ChatID == "" || req.ViaBotID == "" {
		return response.Error(c, apperror.BadRequest("message_id, chat_id, and via_bot_id are required"))
	}
	if len(req.Data) > 256 {
		return response.Error(c, apperror.BadRequest("callback data too long (max 256 bytes)"))
	}

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat_id"))
	}
	viaBotID, err := uuid.Parse(req.ViaBotID)
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid via_bot_id"))
	}

	// Get bot by user ID
	bot, err := h.svc.GetBotByUserID(c.Context(), viaBotID)
	if err != nil {
		return response.Error(c, err)
	}

	// BotFather interception: handle callbacks synchronously without webhook/polling
	if h.botFather != nil && bot.IsSystem && viaBotID == h.botFather.UserID() {
		queryID := uuid.New().String()
		result := h.botFather.HandleCallback(c.Context(), callerID, chatID, queryID, req.Data)
		return response.JSON(c, fiber.StatusOK, result)
	}

	// Security: verify this bot is installed in the specified chat
	installed, err := h.svc.IsBotInstalled(c.Context(), bot.ID, chatID)
	if err != nil {
		return response.Error(c, err)
	}
	if !installed {
		return response.Error(c, apperror.Forbidden("Bot is not installed in this chat"))
	}

	// Generate callback query ID
	queryID := uuid.New().String()

	// Build callback query update
	update := botapi.Update{
		UpdateID: time.Now().UnixMilli(),
		CallbackQuery: &botapi.CallbackQuery{
			ID:     queryID,
			FromID: callerID.String(),
			Message: &botapi.APIMessage{
				MessageID: req.MessageID,
				ChatID:    req.ChatID,
			},
			Data: req.Data,
		},
	}

	// Deliver to bot via webhook or polling queue
	if bot.WebhookURL != nil && *bot.WebhookURL != "" {
		secretEnc := ""
		if bot.WebhookSecretHash != nil {
			secretEnc = *bot.WebhookSecretHash
		}
		if err := h.webhookWorker.Enqueue(bot.ID, *bot.WebhookURL, secretEnc, update); err != nil {
			h.logger.Error("failed to enqueue callback webhook", "bot_id", bot.ID, "error", err)
		}
	} else {
		if err := h.updateQueue.Push(bot.ID, update); err != nil {
			h.logger.Error("failed to push callback to update queue", "bot_id", bot.ID, "error", err)
		}
	}

	// Wait for bot's answerCallbackQuery response (max 30s)
	result, err := h.waitForCallbackAnswer(c.Context(), queryID)
	if err != nil {
		// Timeout or error — still return success, bot just didn't answer
		return response.JSON(c, fiber.StatusOK, fiber.Map{})
	}

	return response.JSON(c, fiber.StatusOK, result)
}

func (h *BotHandler) waitForCallbackAnswer(ctx context.Context, queryID string) (map[string]any, error) {
	key := "callback_ack:" + queryID
	// The public gateway currently times out proxied bot-management requests
	// after 15s, so the service must fail open before that for polling bots
	// that do not answer callback queries.
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
			return nil, fmt.Errorf("callback answer timeout")
		case <-ticker.C:
			val, err := h.redis.Get(ctx, key).Result()
			if err != nil {
				continue // Key not found yet
			}
			// Cleanup
			h.redis.Del(ctx, key)

			var result map[string]any
			if err := json.Unmarshal([]byte(val), &result); err != nil {
				return nil, fmt.Errorf("unmarshal callback answer: %w", err)
			}
			return result, nil
		}
	}
}
