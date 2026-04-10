package handler

import (
	"crypto/subtle"
	"log/slog"
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/pkg/validator"
	"github.com/mst-corp/orbit/services/bots/internal/model"
	"github.com/mst-corp/orbit/services/bots/internal/service"
)

var botUsernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

type BotHandler struct {
	svc    *service.BotService
	logger *slog.Logger
}

func NewBotHandler(svc *service.BotService, logger *slog.Logger) *BotHandler {
	return &BotHandler{svc: svc, logger: logger}
}

func (h *BotHandler) Register(router fiber.Router) {
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
	router.Get("/chats/:chatId/bots", h.listChatBots)
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
		Username         *string `json:"username"`
		DisplayName      *string `json:"display_name"`
		Description      *string `json:"description"`
		ShortDescription *string `json:"short_description"`
		IsInline         *bool   `json:"is_inline"`
		WebhookURL       *string `json:"webhook_url"`
		IsActive         *bool   `json:"is_active"`
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

	bot, err := h.svc.UpdateBot(c.Context(), userID, getUserRole(c), botID, service.UpdateBotInput{
		Username:         req.Username,
		DisplayName:      req.DisplayName,
		Description:      req.Description,
		ShortDescription: req.ShortDescription,
		IsInline:         req.IsInline,
		WebhookURL:       req.WebhookURL,
		IsActive:         req.IsActive,
	})
	if err != nil {
		return response.Error(c, err)
	}

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
