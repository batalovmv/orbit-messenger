package handler

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
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
	app.Post("/stickers/documents", h.GetDocuments)
	app.Get("/stickers/recent", h.ListRecent)
	app.Post("/stickers/recent", h.AddRecent)
	app.Delete("/stickers/recent/:id", h.RemoveRecent)
	app.Delete("/stickers/recent", h.ClearRecent)
	app.Get("/stickers/sets/:id", h.GetPack)
	app.Post("/stickers/sets/import", h.ImportPack)
	app.Post("/stickers/sets/:id/install", h.Install)
	app.Delete("/stickers/sets/:id/install", h.Uninstall)

	app.Post("/admin/sticker-packs", h.CreatePack)
	app.Post("/admin/sticker-packs/:id/stickers", h.AddSticker)
	app.Put("/admin/sticker-packs/:id", h.UpdatePack)
	app.Delete("/admin/sticker-packs/:id", h.DeletePack)
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

func (h *StickerHandler) GetDocuments(c *fiber.Ctx) error {
	if _, err := getUserID(c); err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}

	var body struct {
		IDs []string `json:"ids"`
	}
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if len(body.IDs) == 0 {
		return response.JSON(c, 200, []model.Sticker{})
	}

	stickerIDs := make([]uuid.UUID, 0, len(body.IDs))
	seen := make(map[uuid.UUID]struct{}, len(body.IDs))
	for _, rawID := range body.IDs {
		stickerID, err := uuid.Parse(strings.TrimSpace(rawID))
		if err != nil {
			continue // skip invalid IDs — partial success for batch lookups
		}
		if _, ok := seen[stickerID]; ok {
			continue
		}
		seen[stickerID] = struct{}{}
		stickerIDs = append(stickerIDs, stickerID)
	}

	stickers, err := h.svc.GetByIDs(c.Context(), stickerIDs)
	if err != nil {
		return response.Error(c, err)
	}

	// FillPreviewURLs is already called in service layer — no need to call again.
	return response.JSON(c, 200, stickers)
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

func (h *StickerHandler) ImportPack(c *fiber.Ctx) error {
	if err := requireAdminRole(c); err != nil {
		return response.Error(c, err)
	}

	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		ShortName string `json:"short_name"`
		Source    string `json:"source"`
		URL       string `json:"url"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	source := strings.TrimSpace(req.ShortName)
	if source == "" {
		source = strings.TrimSpace(req.Source)
	}
	if source == "" {
		source = strings.TrimSpace(req.URL)
	}
	if source == "" {
		return response.Error(c, apperror.BadRequest("short_name or source is required"))
	}

	pack, err := h.svc.ImportTelegramPack(c.Context(), userID, source)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, pack)
}

func (h *StickerHandler) CreatePack(c *fiber.Ctx) error {
	if err := requireAdminRole(c); err != nil {
		return response.Error(c, err)
	}
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req struct {
		Name         string  `json:"name"`
		ShortName    string  `json:"short_name"`
		Description  *string `json:"description"`
		ThumbnailURL *string `json:"thumbnail_url"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	pack, err := h.svc.CreateAdminPack(c.Context(), &model.StickerPack{
		Title:        strings.TrimSpace(req.Name),
		ShortName:    req.ShortName,
		Description:  normalizeOptionalString(req.Description),
		AuthorID:     &userID,
		ThumbnailURL: normalizeOptionalString(req.ThumbnailURL),
	})
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, pack)
}

func (h *StickerHandler) AddSticker(c *fiber.Ctx) error {
	if err := requireAdminRole(c); err != nil {
		return response.Error(c, err)
	}

	packID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid pack ID"))
	}

	var req struct {
		Emoji      string `json:"emoji"`
		FileURL    string `json:"file_url"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		IsAnimated bool   `json:"is_animated"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	sticker, err := h.svc.AddStickerToPack(c.Context(), packID, &model.Sticker{
		Emoji:   normalizeOptionalString(&req.Emoji),
		FileURL: strings.TrimSpace(req.FileURL),
		Width:   intPtr(req.Width),
		Height:  intPtr(req.Height),
	}, req.IsAnimated)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, sticker)
}

func (h *StickerHandler) UpdatePack(c *fiber.Ctx) error {
	if err := requireAdminRole(c); err != nil {
		return response.Error(c, err)
	}

	packID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid pack ID"))
	}

	var req struct {
		Name         string  `json:"name"`
		ShortName    string  `json:"short_name"`
		Description  *string `json:"description"`
		ThumbnailURL *string `json:"thumbnail_url"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	pack, err := h.svc.UpdateAdminPack(c.Context(), &model.StickerPack{
		ID:           packID,
		Title:        strings.TrimSpace(req.Name),
		ShortName:    req.ShortName,
		Description:  normalizeOptionalString(req.Description),
		ThumbnailURL: normalizeOptionalString(req.ThumbnailURL),
	})
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, pack)
}

func (h *StickerHandler) DeletePack(c *fiber.Ctx) error {
	if err := requireAdminRole(c); err != nil {
		return response.Error(c, err)
	}

	packID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid pack ID"))
	}

	if err := h.svc.DeleteAdminPack(c.Context(), packID); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}

	return &trimmed
}

func intPtr(value int) *int {
	if value <= 0 {
		return nil
	}

	return &value
}
