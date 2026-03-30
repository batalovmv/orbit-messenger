package handler

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/media/internal/model"
	"github.com/mst-corp/orbit/services/media/internal/service"
)

// MediaHandler handles download, info, and delete endpoints.
type MediaHandler struct {
	svc    *service.MediaService
	logger *slog.Logger
}

// NewMediaHandler creates a media handler.
func NewMediaHandler(svc *service.MediaService, logger *slog.Logger) *MediaHandler {
	return &MediaHandler{svc: svc, logger: logger}
}

// Register sets up media routes.
func (h *MediaHandler) Register(app *fiber.App) {
	app.Get("/media/:id", h.Get)
	app.Get("/media/:id/thumbnail", h.GetThumbnail)
	app.Get("/media/:id/info", h.GetInfo)
	app.Delete("/media/:id", h.Delete)
}

// Get redirects to presigned R2 URL for the original file.
func (h *MediaHandler) Get(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid media ID"))
	}

	url, err := h.svc.GetPresignedURL(c.Context(), id)
	if err != nil {
		return h.mapError(c, err, "get presigned URL")
	}

	return c.Redirect(url, fiber.StatusFound)
}

// GetThumbnail redirects to presigned thumbnail URL.
func (h *MediaHandler) GetThumbnail(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid media ID"))
	}

	url, err := h.svc.GetThumbnailURL(c.Context(), id)
	if err != nil {
		if errors.Is(err, model.ErrNoThumbnail) {
			return response.Error(c, apperror.NotFound("Thumbnail not available"))
		}
		return h.mapError(c, err, "get thumbnail URL")
	}

	return c.Redirect(url, fiber.StatusFound)
}

// GetInfo returns media metadata as JSON.
func (h *MediaHandler) GetInfo(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid media ID"))
	}

	media, err := h.svc.GetInfo(c.Context(), id)
	if err != nil {
		return h.mapError(c, err, "get info")
	}

	resp := h.svc.BuildMediaResponse(c.Context(), media)
	return c.JSON(resp)
}

// Delete removes a media file. Only the uploader can delete.
func (h *MediaHandler) Delete(c *fiber.Ctx) error {
	userID := c.Get("X-User-ID")
	if userID == "" {
		return response.Error(c, apperror.Unauthorized("Missing user ID"))
	}
	uid, err := uuid.Parse(userID)
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	mediaID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid media ID"))
	}

	if err := h.svc.Delete(c.Context(), mediaID, uid); err != nil {
		return h.mapError(c, err, "delete")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// mapError converts sentinel errors to HTTP responses.
func (h *MediaHandler) mapError(c *fiber.Ctx, err error, operation string) error {
	switch {
	case errors.Is(err, model.ErrMediaNotFound):
		return response.Error(c, apperror.NotFound("Media not found"))
	case errors.Is(err, model.ErrNotUploader):
		return response.Error(c, apperror.Forbidden("Only the uploader can perform this action"))
	case errors.Is(err, model.ErrFileTooLarge):
		return response.Error(c, apperror.BadRequest("File too large"))
	case errors.Is(err, model.ErrMIMENotAllowed):
		return response.Error(c, apperror.BadRequest("MIME type not allowed"))
	default:
		h.logger.Error(operation+" failed", "error", err)
		return response.Error(c, apperror.Internal("Internal server error"))
	}
}
