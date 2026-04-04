package handler

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
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
	svc            *service.MediaService
	logger         *slog.Logger
	internalSecret string
}

// NewMediaHandler creates a media handler.
func NewMediaHandler(svc *service.MediaService, logger *slog.Logger, internalSecret string) *MediaHandler {
	return &MediaHandler{svc: svc, logger: logger, internalSecret: internalSecret}
}

// requireInternalToken validates that X-User-ID is only trusted with a valid X-Internal-Token.
func (h *MediaHandler) requireInternalToken(c *fiber.Ctx) error {
	userID := c.Get("X-User-ID")
	if userID == "" {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	token := c.Get("X-Internal-Token")
	if h.internalSecret == "" || token == "" ||
		subtle.ConstantTimeCompare([]byte(token), []byte(h.internalSecret)) != 1 {
		return response.Error(c, apperror.Unauthorized("Invalid internal token"))
	}
	return c.Next()
}

// Register sets up media routes.
func (h *MediaHandler) Register(app *fiber.App) {
	media := app.Group("", h.requireInternalToken)
	media.Get("/media/:id", h.Get)
	media.Get("/media/:id/thumbnail", h.GetThumbnail)
	media.Get("/media/:id/medium", h.GetMedium)
	media.Get("/media/:id/info", h.GetInfo)
	media.Delete("/media/:id", h.Delete)
}

// Get streams the original file from S3 storage.
func (h *MediaHandler) Get(c *fiber.Ctx) error {
	return h.streamVariant(c, "original")
}

// GetThumbnail streams the thumbnail from S3 storage.
func (h *MediaHandler) GetThumbnail(c *fiber.Ctx) error {
	return h.streamVariant(c, "thumbnail")
}

// GetMedium streams the medium-resolution variant from S3 storage.
func (h *MediaHandler) GetMedium(c *fiber.Ctx) error {
	return h.streamVariant(c, "medium")
}

// streamVariant fetches a media variant (original/thumbnail/medium) from S3 and streams it.
// Supports HTTP Range requests for progressive video playback.
func (h *MediaHandler) streamVariant(c *fiber.Ctx, variant string) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid media ID"))
	}

	r2Key, err := h.svc.GetR2Key(c.Context(), id, variant)
	if err != nil {
		if errors.Is(err, model.ErrNoThumbnail) {
			return response.Error(c, apperror.NotFound("Thumbnail not available"))
		}
		if errors.Is(err, model.ErrNoMedium) {
			return response.Error(c, apperror.NotFound("Medium resolution not available"))
		}
		return h.mapError(c, err, "get r2 key for "+variant)
	}

	rangeHeader := c.Get("Range")

	if rangeHeader != "" {
		rr, err := h.svc.StreamFileRange(c.Context(), r2Key, rangeHeader)
		if err != nil {
			return h.mapError(c, err, "stream range "+variant)
		}
		defer rr.Body.Close()

		c.Set("Content-Type", rr.ContentType)
		c.Set("Cache-Control", "public, max-age=31536000, immutable")
		c.Set("Accept-Ranges", "bytes")
		c.Set("Content-Length", fmt.Sprintf("%d", rr.PartSize))
		if rr.ContentRange != "" {
			c.Set("Content-Range", rr.ContentRange)
		}
		c.Status(fiber.StatusPartialContent)
		if _, err := io.Copy(c.Response().BodyWriter(), rr.Body); err != nil {
			return h.mapError(c, err, "stream copy range "+variant)
		}
		return nil
	}

	body, contentType, err := h.svc.StreamFile(c.Context(), r2Key)
	if err != nil {
		return h.mapError(c, err, "stream "+variant)
	}
	defer body.Close()

	c.Set("Content-Type", contentType)
	c.Set("Cache-Control", "public, max-age=31536000, immutable")
	c.Set("Accept-Ranges", "bytes")

	// Stream directly from S3 to response — never buffer entire file in memory.
	// Files can be up to 2 GB; io.ReadAll would OOM on concurrent large downloads.
	c.Status(fiber.StatusOK)
	if _, err := io.Copy(c.Response().BodyWriter(), body); err != nil {
		return h.mapError(c, err, "stream copy "+variant)
	}
	return nil
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
	return response.JSON(c, fiber.StatusOK, resp)
}

// Delete removes a media file. Only the uploader can delete.
func (h *MediaHandler) Delete(c *fiber.Ctx) error {
	uid, err := uuid.Parse(c.Get("X-User-ID"))
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
