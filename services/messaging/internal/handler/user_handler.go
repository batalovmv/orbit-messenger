package handler

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/pkg/validator"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

type UserHandler struct {
	svc     *service.UserService
	chatSvc *service.ChatService
	logger  *slog.Logger
}

func NewUserHandler(svc *service.UserService, logger *slog.Logger, opts ...interface{}) *UserHandler {
	h := &UserHandler{svc: svc, logger: logger}
	for _, opt := range opts {
		if cs, ok := opt.(*service.ChatService); ok {
			h.chatSvc = cs
		}
	}
	return h
}

func (h *UserHandler) Register(app fiber.Router) {
	app.Get("/users/me", h.GetMe)
	app.Put("/users/me", h.UpdateProfile)
	app.Get("/users/:id/contacts", h.GetContactIDs)
	app.Get("/users/:id/common-chats", h.GetCommonChats)
	app.Get("/users/:id", h.GetUser)
	app.Get("/users", h.SearchUsers)
}

func (h *UserHandler) GetMe(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	u, err := h.svc.GetMe(c.Context(), uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, u)
}

func (h *UserHandler) GetUser(c *fiber.Ctx) error {
	targetID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	callerID, _ := getUserID(c)

	u, err := h.svc.GetUserForViewer(c.Context(), callerID, targetID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, u)
}

func (h *UserHandler) UpdateProfile(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		DisplayName       *string `json:"display_name"`
		Bio               *string `json:"bio"`
		Phone             *string `json:"phone"`
		AvatarURL         *string `json:"avatar_url"`
		CustomStatus      *string `json:"custom_status"`
		CustomStatusEmoji *string `json:"custom_status_emoji"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if req.DisplayName != nil {
		if vErr := validator.RequireString(*req.DisplayName, "display_name", 1, 64); vErr != nil {
			return response.Error(c, vErr)
		}
	}
	if req.Bio != nil {
		if vErr := validator.RequireString(*req.Bio, "bio", 0, 500); vErr != nil {
			return response.Error(c, vErr)
		}
	}
	if req.CustomStatus != nil {
		if vErr := validator.RequireString(*req.CustomStatus, "custom_status", 0, 128); vErr != nil {
			return response.Error(c, vErr)
		}
	}
	if req.CustomStatusEmoji != nil {
		if vErr := validator.RequireString(*req.CustomStatusEmoji, "custom_status_emoji", 0, 32); vErr != nil {
			return response.Error(c, vErr)
		}
	}

	if req.AvatarURL != nil && *req.AvatarURL != "" && !isValidAvatarURL(*req.AvatarURL) {
		return response.Error(c, apperror.BadRequest("Invalid avatar URL: must be https or /media/ path"))
	}
	if req.AvatarURL != nil && *req.AvatarURL != "" {
		normalized := normalizeAvatarURL(*req.AvatarURL)
		req.AvatarURL = &normalized
	}

	u, err := h.svc.UpdateProfile(c.Context(), uid, req.DisplayName,
		req.Bio, req.Phone, req.AvatarURL, req.CustomStatus, req.CustomStatusEmoji)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, u)
}

func (h *UserHandler) GetContactIDs(c *fiber.Ctx) error {
	callerID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	targetID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	if callerID != targetID {
		return response.Error(c, apperror.Forbidden("Cannot access another user's contacts"))
	}

	ids, err := h.svc.GetContactIDs(c.Context(), targetID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"contact_ids": ids})
}

func (h *UserHandler) SearchUsers(c *fiber.Ctx) error {
	callerID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Authentication required"))
	}

	query := c.Query("q")

	limit := c.QueryInt("limit", 20)
	if limit > 100 {
		limit = 100
	}

	users, err := h.svc.SearchUsers(c.Context(), callerID, query, limit)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"users": users})
}

func (h *UserHandler) GetCommonChats(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	targetID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	if h.chatSvc == nil {
		return response.Error(c, apperror.Internal("common chats not available"))
	}

	limit := c.QueryInt("limit", 100)
	chats, err := h.chatSvc.GetCommonChats(c.Context(), uid, targetID, limit)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"chats": chats,
		"count": len(chats),
	})
}
