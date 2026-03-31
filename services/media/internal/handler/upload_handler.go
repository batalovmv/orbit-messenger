package handler

import (
	"crypto/subtle"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/media/internal/model"
	"github.com/mst-corp/orbit/services/media/internal/service"
)

// UploadHandler handles media upload endpoints.
type UploadHandler struct {
	svc            *service.MediaService
	logger         *slog.Logger
	internalSecret string
}

// NewUploadHandler creates upload handler.
func NewUploadHandler(svc *service.MediaService, logger *slog.Logger, internalSecret string) *UploadHandler {
	return &UploadHandler{svc: svc, logger: logger, internalSecret: internalSecret}
}

// requireInternalToken validates that X-User-ID is only trusted with a valid X-Internal-Token.
func (h *UploadHandler) requireInternalToken(c *fiber.Ctx) error {
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

// Register sets up upload routes.
func (h *UploadHandler) Register(app *fiber.App) {
	upload := app.Group("", h.requireInternalToken)
	upload.Post("/media/upload", h.Upload)
	upload.Post("/media/upload/chunked/init", h.ChunkedInit)
	upload.Post("/media/upload/chunked/:uploadId", h.ChunkedUploadPart)
	upload.Post("/media/upload/chunked/:uploadId/complete", h.ChunkedComplete)
}

// Upload handles simple file upload via multipart/form-data.
func (h *UploadHandler) Upload(c *fiber.Ctx) error {
	uid, err := uuid.Parse(c.Get("X-User-ID"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	file, err := c.FormFile("file")
	if err != nil {
		return response.Error(c, apperror.BadRequest("No file provided"))
	}

	if file.Size > model.SimpleUploadLimit {
		return response.Error(c, apperror.BadRequest("File too large for simple upload, use chunked upload"))
	}

	// Read file
	f, err := file.Open()
	if err != nil {
		return response.Error(c, apperror.Internal("Failed to read file"))
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return response.Error(c, apperror.Internal("Failed to read file"))
	}

	// Always detect MIME from content — never trust client Content-Type for type validation
	mimeType := http.DetectContentType(data)
	// For generic types, fall back to the declared Content-Type as a hint
	if mimeType == "application/octet-stream" {
		if declared := file.Header.Get("Content-Type"); declared != "" {
			mimeType = declared
		}
	}

	mediaType := c.FormValue("type", "")
	isOneTime := c.FormValue("is_one_time", "false") == "true"

	media, err := h.svc.Upload(c.Context(), uid, data, file.Filename, mimeType, mediaType, isOneTime)
	if err != nil {
		return h.mapChunkedError(c, err, "upload")
	}

	resp := h.svc.BuildMediaResponse(c.Context(), media)
	return c.Status(fiber.StatusCreated).JSON(resp)
}

// ChunkedInit starts a chunked upload.
func (h *UploadHandler) ChunkedInit(c *fiber.Ctx) error {
	uid, err := uuid.Parse(c.Get("X-User-ID"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	var req struct {
		Filename  string `json:"filename"`
		MimeType  string `json:"mime_type"`
		MediaType string `json:"media_type"`
		TotalSize int64  `json:"total_size"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if req.Filename == "" || req.MimeType == "" || req.TotalSize <= 0 {
		return response.Error(c, apperror.BadRequest("filename, mime_type, and total_size are required"))
	}

	meta, err := h.svc.InitChunkedUpload(c.Context(), uid, req.Filename, req.MimeType, req.MediaType, req.TotalSize)
	if err != nil {
		return h.mapChunkedError(c, err, "chunked init")
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"upload_id":    meta.ID,
		"chunk_size":   meta.ChunkSize,
		"total_chunks": meta.TotalChunks,
	})
}

// ChunkedUploadPart uploads a single chunk.
func (h *UploadHandler) ChunkedUploadPart(c *fiber.Ctx) error {
	uid, err := uuid.Parse(c.Get("X-User-ID"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	uploadID := c.Params("uploadId")
	if uploadID == "" {
		return response.Error(c, apperror.BadRequest("Missing upload ID"))
	}

	partStr := c.Get("X-Part-Number")
	if partStr == "" {
		partStr = c.Query("part_number", "1")
	}
	partNumber, err := strconv.Atoi(partStr)
	if err != nil || partNumber < 1 || partNumber > 10000 {
		return response.Error(c, apperror.BadRequest("Invalid part number (must be 1-10000)"))
	}

	data := c.Body()
	if len(data) == 0 {
		return response.Error(c, apperror.BadRequest("Empty chunk"))
	}
	if len(data) > model.ChunkSize {
		return response.Error(c, apperror.BadRequest("Chunk exceeds maximum size"))
	}

	uploaded, total, err := h.svc.UploadChunk(c.Context(), uploadID, uid, partNumber, data)
	if err != nil {
		return h.mapChunkedError(c, err, "chunk upload")
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"uploaded_chunks": uploaded,
		"total_chunks":    total,
	})
}

// ChunkedComplete finishes a chunked upload.
func (h *UploadHandler) ChunkedComplete(c *fiber.Ctx) error {
	uid, err := uuid.Parse(c.Get("X-User-ID"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	uploadID := c.Params("uploadId")
	if uploadID == "" {
		return response.Error(c, apperror.BadRequest("Missing upload ID"))
	}

	var req struct {
		IsOneTime bool `json:"is_one_time"`
	}
	c.BodyParser(&req) // optional body

	media, err := h.svc.CompleteChunkedUpload(c.Context(), uploadID, uid, req.IsOneTime)
	if err != nil {
		return h.mapChunkedError(c, err, "chunked complete")
	}

	resp := h.svc.BuildMediaResponse(c.Context(), media)
	return c.Status(fiber.StatusCreated).JSON(resp)
}

// mapChunkedError converts service errors to HTTP responses.
func (h *UploadHandler) mapChunkedError(c *fiber.Ctx, err error, operation string) error {
	switch {
	case errors.Is(err, model.ErrFileTooLarge):
		return response.Error(c, apperror.BadRequest("File too large"))
	case errors.Is(err, model.ErrMIMENotAllowed):
		return response.Error(c, apperror.BadRequest("MIME type not allowed"))
	case errors.Is(err, model.ErrUploadNotFound):
		return response.Error(c, apperror.NotFound("Upload not found or expired"))
	case errors.Is(err, model.ErrUploadForbidden):
		return response.Error(c, apperror.Forbidden("Not the upload owner"))
	case errors.Is(err, model.ErrInvalidPartNum):
		return response.Error(c, apperror.BadRequest("Invalid part number (must be 1-10000)"))
	default:
		h.logger.Error(operation+" failed", "error", err)
		return response.Error(c, apperror.Internal("Internal server error"))
	}
}
