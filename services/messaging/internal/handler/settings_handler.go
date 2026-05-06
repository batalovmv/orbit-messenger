// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

type SettingsHandler struct {
	settingsSvc    *service.SettingsService
	pushStore      store.PushSubscriptionStore
	logger         *slog.Logger
	internalSecret string
}

func NewSettingsHandler(
	settingsSvc *service.SettingsService,
	pushStore store.PushSubscriptionStore,
	logger *slog.Logger,
	internalSecret string,
) *SettingsHandler {
	h := &SettingsHandler{
		settingsSvc:    settingsSvc,
		pushStore:      pushStore,
		logger:         logger,
		internalSecret: internalSecret,
	}
	return h
}

// RegisterInternal registers server-to-server routes that don't carry a user
// context (no X-User-ID). Must be mounted behind RequireInternalOnly.
func (h *SettingsHandler) RegisterInternal(app fiber.Router) {
	app.Post("/internal/notification-settings/muted-users", h.ListMutedUsers)
}

func (h *SettingsHandler) Register(app fiber.Router) {
	app.Get("/internal/push-subscriptions/:userId", h.GetInternalPushSubscriptions)
	app.Delete("/internal/push-subscriptions/:userId", h.DeleteInternalPushSubscription)
	app.Get("/users/me/settings/privacy", h.GetPrivacySettings)
	app.Put("/users/me/settings/privacy", h.UpdatePrivacySettings)
	app.Get("/users/me/settings/appearance", h.GetUserSettings)
	app.Put("/users/me/settings/appearance", h.UpdateUserSettings)
	app.Get("/users/me/blocked", h.ListBlockedUsers)
	app.Post("/users/me/blocked/:userId", h.BlockUser)
	app.Delete("/users/me/blocked/:userId", h.UnblockUser)
	app.Get("/users/me/settings/notifications", h.GetGlobalNotifySettings)
	app.Put("/users/me/settings/notifications", h.UpdateGlobalNotifySettings)
	app.Get("/users/me/notification-exceptions", h.ListNotificationExceptions)
	app.Put("/chats/:id/notifications", h.UpdateChatNotifications)
	app.Get("/chats/:id/notifications", h.GetChatNotifications)
	app.Delete("/chats/:id/notifications", h.DeleteChatNotifications)
	app.Put("/chats/:id/notification-priority", h.UpdateChatNotificationPriority)
	app.Post("/push/subscribe", h.SubscribePush)
	app.Delete("/push/subscribe", h.UnsubscribePush)
}

func (h *SettingsHandler) GetPrivacySettings(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	ps, err := h.settingsSvc.GetPrivacySettings(c.Context(), uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, ps)
}

func (h *SettingsHandler) UpdatePrivacySettings(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		LastSeen  string `json:"last_seen"`
		Avatar    string `json:"avatar"`
		Phone     string `json:"phone"`
		Calls     string `json:"calls"`
		Groups    string `json:"groups"`
		Forwarded string `json:"forwarded"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	ps, err := h.settingsSvc.UpdatePrivacySettings(
		c.Context(), uid,
		req.LastSeen, req.Avatar, req.Phone, req.Calls, req.Groups, req.Forwarded,
	)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, ps)
}

func (h *SettingsHandler) GetUserSettings(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	us, err := h.settingsSvc.GetUserSettings(c.Context(), uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, us)
}

func (h *SettingsHandler) UpdateUserSettings(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		Theme       string  `json:"theme"`
		Language    string  `json:"language"`
		FontSize    int     `json:"font_size"`
		SendByEnter bool    `json:"send_by_enter"`
		DNDFrom     *string `json:"dnd_from"`
		DNDUntil             *string `json:"dnd_until"`
		DefaultTranslateLang *string `json:"default_translate_lang"`
		CanTranslate         *bool   `json:"can_translate"`
		CanTranslateChats    *bool   `json:"can_translate_chats"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	// Validate default_translate_lang if provided
	if req.DefaultTranslateLang != nil {
		validLangs := map[string]bool{"ru": true, "en": true, "de": true, "es": true, "fr": true, "zh": true, "ja": true}
		if *req.DefaultTranslateLang != "" && !validLangs[*req.DefaultTranslateLang] {
			return response.Error(c, apperror.BadRequest("invalid default_translate_lang"))
		}
		// Empty string means clear the setting
		if *req.DefaultTranslateLang == "" {
			req.DefaultTranslateLang = nil
		}
	}

	// Preserve existing translate prefs when the client omits them.
	existing, err := h.settingsSvc.GetUserSettings(c.Context(), uid)
	if err != nil {
		return response.Error(c, err)
	}
	canTranslate := existing.CanTranslate
	if req.CanTranslate != nil {
		canTranslate = *req.CanTranslate
	}
	canTranslateChats := existing.CanTranslateChats
	if req.CanTranslateChats != nil {
		canTranslateChats = *req.CanTranslateChats
	}

	_, err = h.settingsSvc.UpdateUserSettings(
		c.Context(), uid,
		req.Theme, req.Language, req.FontSize, req.SendByEnter,
		req.DNDFrom, req.DNDUntil, req.DefaultTranslateLang,
		canTranslate, canTranslateChats,
	)
	if err != nil {
		return response.Error(c, err)
	}

	us, err := h.settingsSvc.GetUserSettings(c.Context(), uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, us)
}

func (h *SettingsHandler) ListBlockedUsers(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	limit := c.QueryInt("limit", 50)
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	users, err := h.settingsSvc.ListBlockedUsers(c.Context(), uid, limit)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"blocked_users": users})
}

func (h *SettingsHandler) BlockUser(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	targetID, err := uuid.Parse(c.Params("userId"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	if err := h.settingsSvc.BlockUser(c.Context(), uid, targetID); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"blocked": true})
}

func (h *SettingsHandler) UnblockUser(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	targetID, err := uuid.Parse(c.Params("userId"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	if err := h.settingsSvc.UnblockUser(c.Context(), uid, targetID); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"unblocked": true})
}

func (h *SettingsHandler) GetChatNotifications(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	ns, err := h.settingsSvc.GetNotificationSettings(c.Context(), uid, chatID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, ns)
}

func (h *SettingsHandler) UpdateChatNotifications(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	var req struct {
		MutedUntil  *time.Time `json:"muted_until"`
		Sound       string     `json:"sound"`
		ShowPreview bool       `json:"show_preview"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	ns, err := h.settingsSvc.UpdateNotificationSettings(
		c.Context(), uid, chatID,
		req.MutedUntil, req.Sound, req.ShowPreview,
	)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, ns)
}

func (h *SettingsHandler) DeleteChatNotifications(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	if err := h.settingsSvc.DeleteNotificationSettings(c.Context(), uid, chatID); err != nil {
		return response.Error(c, err)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *SettingsHandler) SubscribePush(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		Endpoint string `json:"endpoint"`
		P256DH   string `json:"p256dh"`
		Auth     string `json:"auth"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if !strings.HasPrefix(req.Endpoint, "https://") {
		return response.Error(c, apperror.BadRequest("endpoint must start with https://"))
	}
	// SSRF protection: only allow known Web Push service hostnames
	endpointURL, err := url.Parse(req.Endpoint)
	if err != nil || endpointURL.Host == "" {
		return response.Error(c, apperror.BadRequest("invalid push endpoint URL"))
	}
	// Block private/loopback IPs in the hostname
	host := endpointURL.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return response.Error(c, apperror.BadRequest("push endpoint must not point to a private address"))
		}
	} else {
		// Resolve hostname to guard against DNS rebinding attacks
		ips, err := net.LookupIP(host)
		if err != nil {
			return response.Error(c, apperror.BadRequest("cannot resolve push endpoint hostname"))
		}
		for _, ip := range ips {
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				return response.Error(c, apperror.BadRequest("push endpoint resolves to a private or loopback address"))
			}
		}
	}
	if req.P256DH == "" {
		return response.Error(c, apperror.BadRequest("p256dh is required"))
	}
	if req.Auth == "" {
		return response.Error(c, apperror.BadRequest("auth is required"))
	}

	ua := c.Get("User-Agent")
	sub := &model.PushSubscription{
		ID:        uuid.New(),
		UserID:    uid,
		Endpoint:  req.Endpoint,
		P256DH:    req.P256DH,
		Auth:      req.Auth,
		UserAgent: &ua,
	}
	if err := h.pushStore.Create(c.Context(), sub); err != nil {
		if errors.Is(err, model.ErrPushSubscriptionLimitReached) {
			return response.Error(c, apperror.TooManyRequests("maximum of 10 push subscriptions per user"))
		}
		h.logger.Error("create push subscription", "error", err, "user_id", uid)
		return response.Error(c, apperror.Internal("failed to save push subscription"))
	}

	return response.JSON(c, fiber.StatusCreated, sub)
}

func (h *SettingsHandler) UnsubscribePush(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if req.Endpoint == "" {
		return response.Error(c, apperror.BadRequest("endpoint is required"))
	}

	if err := h.pushStore.Delete(c.Context(), uid, req.Endpoint); err != nil {
		return response.Error(c, err)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *SettingsHandler) GetInternalPushSubscriptions(c *fiber.Ctx) error {
	if err := requireInternalRequest(c, h.internalSecret); err != nil {
		return response.Error(c, err)
	}

	userID, err := uuid.Parse(c.Params("userId"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	subscriptions, err := h.pushStore.ListByUser(c.Context(), userID)
	if err != nil {
		h.logger.Error("list internal push subscriptions", "error", err, "user_id", userID)
		return response.Error(c, apperror.Internal("failed to load push subscriptions"))
	}

	return response.JSON(c, fiber.StatusOK, subscriptions)
}

func (h *SettingsHandler) DeleteInternalPushSubscription(c *fiber.Ctx) error {
	if err := requireInternalRequest(c, h.internalSecret); err != nil {
		return response.Error(c, err)
	}

	userID, err := uuid.Parse(c.Params("userId"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	endpoint := c.Query("endpoint")
	if endpoint == "" {
		return response.Error(c, apperror.BadRequest("endpoint is required"))
	}

	if err := h.pushStore.Delete(c.Context(), userID, endpoint); err != nil {
		return response.Error(c, err)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *SettingsHandler) ListMutedUsers(c *fiber.Ctx) error {
	if err := requireInternalRequest(c, h.internalSecret); err != nil {
		return response.Error(c, err)
	}

	var req struct {
		ChatID  string   `json:"chat_id"`
		UserIDs []string `json:"user_ids"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}

	for _, userID := range req.UserIDs {
		if _, err := uuid.Parse(userID); err != nil {
			return response.Error(c, apperror.BadRequest("Invalid user ID"))
		}
	}

	mutedUserIDs, err := h.settingsSvc.GetMutedUserIDs(c.Context(), chatID, req.UserIDs)
	if err != nil {
		h.logger.Error("list muted users", "error", err, "chat_id", chatID)
		return response.Error(c, apperror.Internal("failed to load notification settings"))
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"muted_user_ids": mutedUserIDs})
}

func (h *SettingsHandler) GetGlobalNotifySettings(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	gs, err := h.settingsSvc.GetGlobalNotifySettings(c.Context(), uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, gs)
}

func (h *SettingsHandler) UpdateGlobalNotifySettings(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	// Read current settings first to merge with partial update
	current, _ := h.settingsSvc.GetGlobalNotifySettings(c.Context(), uid)

	var req model.GlobalNotifySettings
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	// Merge: use current values as defaults, overwrite only fields present in request body
	if current != nil {
		merged := *current
		// Parse raw body to detect which fields were actually sent
		var raw map[string]interface{}
		if err := json.Unmarshal(c.Body(), &raw); err == nil {
			if _, ok := raw["users_muted"]; ok {
				merged.UsersMuted = req.UsersMuted
			}
			if _, ok := raw["groups_muted"]; ok {
				merged.GroupsMuted = req.GroupsMuted
			}
			if _, ok := raw["users_preview"]; ok {
				merged.UsersPreview = req.UsersPreview
			}
			if _, ok := raw["groups_preview"]; ok {
				merged.GroupsPreview = req.GroupsPreview
			}
			req = merged
		}
	}

	if err := h.settingsSvc.UpdateGlobalNotifySettings(c.Context(), uid, &req); err != nil {
		return response.Error(c, err)
	}

	gs, err := h.settingsSvc.GetGlobalNotifySettings(c.Context(), uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, gs)
}

func (h *SettingsHandler) UpdateChatNotificationPriority(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("unauthorized"))
	}

	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("invalid chat ID"))
	}

	var req struct {
		PriorityOverride *string `json:"priority_override"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("invalid request body"))
	}

	if req.PriorityOverride != nil {
		valid := map[string]bool{"urgent": true, "important": true, "normal": true, "low": true}
		if !valid[*req.PriorityOverride] {
			return response.Error(c, apperror.BadRequest("priority_override must be one of: urgent, important, normal, low"))
		}
	}

	if err := h.settingsSvc.UpdateChatNotificationPriority(c.Context(), uid, chatID, req.PriorityOverride); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"priority_override": req.PriorityOverride})
}

func (h *SettingsHandler) ListNotificationExceptions(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	exceptions, err := h.settingsSvc.ListNotificationExceptions(c.Context(), uid)
	if err != nil {
		return response.Error(c, err)
	}

	if exceptions == nil {
		exceptions = []model.NotificationSettings{}
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"exceptions": exceptions})
}
