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

// GIFHandler handles HTTP requests for GIF search and saved GIFs.
type GIFHandler struct {
	svc    *service.GIFService
	logger *slog.Logger
}

// NewGIFHandler creates a new GIFHandler.
func NewGIFHandler(svc *service.GIFService, logger *slog.Logger) *GIFHandler {
	return &GIFHandler{svc: svc, logger: logger}
}

// Register registers GIF routes.
func (h *GIFHandler) Register(app fiber.Router) {
	app.Get("/gifs/search", h.Search)
	app.Get("/gifs/trending", h.Trending)
	app.Get("/gifs/saved", h.ListSaved)
	app.Post("/gifs/saved", h.Save)
	app.Delete("/gifs/saved/:id", h.Remove)
}

func (h *GIFHandler) Search(c *fiber.Ctx) error {
	q := c.Query("q")
	if q == "" {
		return response.Error(c, apperror.BadRequest("Query parameter 'q' is required"))
	}
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	pos := c.Query("pos")
	gifs, nextPos, err := h.svc.Search(c.Context(), q, limit, pos)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, fiber.Map{"data": gifs, "next_pos": nextPos})
}

func (h *GIFHandler) Trending(c *fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	pos := c.Query("pos")
	gifs, nextPos, err := h.svc.Trending(c.Context(), limit, pos)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, fiber.Map{"data": gifs, "next_pos": nextPos})
}

func (h *GIFHandler) ListSaved(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	gifs, err := h.svc.ListSaved(c.Context(), userID, limit)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, gifs)
}

func (h *GIFHandler) Save(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	var body struct {
		TenorID    string  `json:"tenor_id"`
		URL        string  `json:"url"`
		PreviewURL *string `json:"preview_url"`
		Width      *int    `json:"width"`
		Height     *int    `json:"height"`
	}
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	if body.TenorID == "" || body.URL == "" {
		return response.Error(c, apperror.BadRequest("tenor_id and url are required"))
	}
	gif := &service.SaveGIFInput{
		UserID:     userID,
		TenorID:    body.TenorID,
		URL:        body.URL,
		PreviewURL: body.PreviewURL,
		Width:      body.Width,
		Height:     body.Height,
	}
	if err := h.svc.Save(c.Context(), gif.ToModel()); err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 201, fiber.Map{"ok": true})
}

func (h *GIFHandler) Remove(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	gifID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid GIF ID"))
	}
	if err := h.svc.Remove(c.Context(), userID, gifID); err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, fiber.Map{"ok": true})
}
