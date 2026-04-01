package handler

import (
	"log/slog"
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
	settingsSvc  *service.SettingsService
	pushStore    store.PushSubscriptionStore
	logger       *slog.Logger
}

func NewSettingsHandler(
	settingsSvc *service.SettingsService,
	pushStore store.PushSubscriptionStore,
	logger *slog.Logger,
) *SettingsHandler {
	return &SettingsHandler{
		settingsSvc: settingsSvc,
		pushStore:   pushStore,
		logger:      logger,
	}
}

func (h *SettingsHandler) Register(app fiber.Router) {
	app.Get("/users/me/settings/privacy", h.GetPrivacySettings)
	app.Put("/users/me/settings/privacy", h.UpdatePrivacySettings)
	app.Get("/users/me/settings/appearance", h.GetUserSettings)
	app.Put("/users/me/settings/appearance", h.UpdateUserSettings)
	app.Get("/users/me/blocked", h.ListBlockedUsers)
	app.Post("/users/me/blocked/:userId", h.BlockUser)
	app.Delete("/users/me/blocked/:userId", h.UnblockUser)
	app.Put("/chats/:id/notifications", h.UpdateChatNotifications)
	app.Get("/chats/:id/notifications", h.GetChatNotifications)
	app.Delete("/chats/:id/notifications", h.DeleteChatNotifications)
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
		DNDUntil    *string `json:"dnd_until"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	us, err := h.settingsSvc.UpdateUserSettings(
		c.Context(), uid,
		req.Theme, req.Language, req.FontSize, req.SendByEnter,
		req.DNDFrom, req.DNDUntil,
	)
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
	if req.P256DH == "" {
		return response.Error(c, apperror.BadRequest("p256dh is required"))
	}
	if req.Auth == "" {
		return response.Error(c, apperror.BadRequest("auth is required"))
	}

	count, err := h.pushStore.CountByUser(c.Context(), uid)
	if err != nil {
		h.logger.Error("count push subscriptions", "error", err, "user_id", uid)
		return response.Error(c, apperror.Internal("failed to check subscription count"))
	}
	if count >= 10 {
		return response.Error(c, apperror.TooManyRequests("maximum of 10 push subscriptions per user"))
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
