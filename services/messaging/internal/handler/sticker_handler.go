package handler

import (
	"log/slog"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

// StickerHandler handles HTTP requests for stickers.
type StickerHandler struct {
	svc    *service.StickerService
	logger *slog.Logger
}

// NewStickerHandler creates a new StickerHandler.
func NewStickerHandler(svc *service.StickerService, logger *slog.Logger) *StickerHandler {
	return &StickerHandler{svc: svc, logger: logger}
}

// Register registers sticker routes.
func (h *StickerHandler) Register(app fiber.Router) {
	app.Get("/stickers/featured", h.ListFeatured)
	app.Get("/stickers/search", h.Search)
	app.Get("/stickers/installed", h.ListInstalled)
	app.Get("/stickers/recent", h.ListRecent)
	app.Post("/stickers/recent", h.AddRecent)
	app.Delete("/stickers/recent/:id", h.RemoveRecent)
	app.Delete("/stickers/recent", h.ClearRecent)
	app.Get("/stickers/sets/:id", h.GetPack)
	app.Post("/stickers/sets/:id/install", h.Install)
	app.Delete("/stickers/sets/:id/install", h.Uninstall)
}

func (h *StickerHandler) ListFeatured(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	packs, err := h.svc.ListFeatured(c.Context(), userID, limit)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, packs)
}

func (h *StickerHandler) Search(c *fiber.Ctx) error {
	q := c.Query("q")
	if q == "" {
		return response.Error(c, apperror.BadRequest("Query parameter 'q' is required"))
	}
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	packs, err := h.svc.Search(c.Context(), q, limit)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, packs)
}

func (h *StickerHandler) GetPack(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	packID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid pack ID"))
	}
	pack, err := h.svc.GetPack(c.Context(), packID, userID)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, pack)
}

func (h *StickerHandler) ListInstalled(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	packs, err := h.svc.ListInstalled(c.Context(), userID)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, packs)
}

func (h *StickerHandler) ListRecent(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	limit, _ := strconv.Atoi(c.Query("limit", "30"))
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	stickers, err := h.svc.ListRecent(c.Context(), userID, limit)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, stickers)
}

func (h *StickerHandler) AddRecent(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	var body struct {
		StickerID string `json:"sticker_id"`
	}
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	stickerID, err := uuid.Parse(body.StickerID)
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid sticker ID"))
	}
	if err := h.svc.AddRecent(c.Context(), userID, stickerID); err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, fiber.Map{"ok": true})
}

func (h *StickerHandler) RemoveRecent(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	stickerID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid sticker ID"))
	}
	if err := h.svc.RemoveRecent(c.Context(), userID, stickerID); err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, fiber.Map{"ok": true})
}

func (h *StickerHandler) ClearRecent(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	if err := h.svc.ClearRecent(c.Context(), userID); err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, fiber.Map{"ok": true})
}

func (h *StickerHandler) Install(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	packID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid pack ID"))
	}
	if err := h.svc.Install(c.Context(), userID, packID); err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, fiber.Map{"ok": true})
}

func (h *StickerHandler) Uninstall(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	packID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid pack ID"))
	}
	if err := h.svc.Uninstall(c.Context(), userID, packID); err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, fiber.Map{"ok": true})
}
