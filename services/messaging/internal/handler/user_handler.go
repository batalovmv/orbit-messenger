package handler

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

type UserHandler struct {
	svc    *service.UserService
	logger *slog.Logger
}

func NewUserHandler(svc *service.UserService, logger *slog.Logger) *UserHandler {
	return &UserHandler{svc: svc, logger: logger}
}

func (h *UserHandler) Register(app fiber.Router) {
	app.Get("/users/me", h.GetMe)
	app.Put("/users/me", h.UpdateProfile)
	app.Get("/users/:id/contacts", h.GetContactIDs)
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

	// Check if requesting own profile (full data) or someone else's (strip PII)
	callerID, _ := getUserID(c)

	u, err := h.svc.GetUser(c.Context(), targetID)
	if err != nil {
		return response.Error(c, err)
	}

	// Strip PII for non-self lookups
	if callerID != targetID {
		u.Email = ""
		u.Phone = nil
	}

	return response.JSON(c, fiber.StatusOK, u)
}

func (h *UserHandler) UpdateProfile(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		DisplayName       string  `json:"display_name"`
		Bio               *string `json:"bio"`
		Phone             *string `json:"phone"`
		AvatarURL         *string `json:"avatar_url"`
		CustomStatus      *string `json:"custom_status"`
		CustomStatusEmoji *string `json:"custom_status_emoji"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	u, err := h.svc.UpdateProfile(c.Context(), uid, req.DisplayName,
		req.Bio, req.Phone, req.AvatarURL, req.CustomStatus, req.CustomStatusEmoji)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, u)
}

func (h *UserHandler) GetContactIDs(c *fiber.Ctx) error {
	targetID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	ids, err := h.svc.GetContactIDs(c.Context(), targetID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"contact_ids": ids})
}

func (h *UserHandler) SearchUsers(c *fiber.Ctx) error {
	query := c.Query("q")

	limit := c.QueryInt("limit", 20)
	if limit > 100 {
		limit = 100
	}
	callerID, _ := getUserID(c)

	users, err := h.svc.SearchUsers(c.Context(), query, limit)
	if err != nil {
		return response.Error(c, err)
	}

	// Strip PII for non-self results (consistent with GetUser)
	for i := range users {
		if users[i].ID != callerID {
			users[i].Email = ""
			users[i].Phone = nil
		}
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"users": users})
}
