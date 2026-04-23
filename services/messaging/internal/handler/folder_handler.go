// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"log/slog"
	"strconv"

	"github.com/gofiber/fiber/v2"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/pkg/validator"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

// FolderHandler handles HTTP endpoints for chat folders.
type FolderHandler struct {
	svc    *service.FolderService
	logger *slog.Logger
}

// NewFolderHandler creates a FolderHandler backed by the given service.
func NewFolderHandler(svc *service.FolderService, logger *slog.Logger) *FolderHandler {
	return &FolderHandler{svc: svc, logger: logger}
}

// Register mounts all folder routes onto the given router.
func (h *FolderHandler) Register(app fiber.Router) {
	// PUT /folders/order must be registered before /folders/:id to avoid
	// "order" being captured as an integer param.
	app.Put("/folders/order", h.UpdateFolderOrder)
	app.Get("/folders", h.ListFolders)
	app.Post("/folders", h.CreateFolder)
	app.Get("/folders/:id", h.GetFolder)
	app.Put("/folders/:id", h.UpdateFolder)
	app.Delete("/folders/:id", h.DeleteFolder)
}

// folderRequest is the shared body for POST /folders and PUT /folders/:id.
type folderRequest struct {
	Title           string   `json:"title"`
	Emoticon        string   `json:"emoticon"`
	Color           *int     `json:"color"`
	IncludedChatIDs []string `json:"included_chat_ids"`
	ExcludedChatIDs []string `json:"excluded_chat_ids"`
	PinnedChatIDs   []string `json:"pinned_chat_ids"`
}

// folderOrderRequest is the body for PUT /folders/order.
type folderOrderRequest struct {
	FolderIDs []int `json:"folder_ids"`
}

// folderResponse is the JSON shape returned for a single folder.
type folderResponse struct {
	ID              int      `json:"id"`
	Title           string   `json:"title"`
	Emoticon        string   `json:"emoticon,omitempty"`
	Color           *int     `json:"color,omitempty"`
	Position        int      `json:"position"`
	IncludedChatIDs []string `json:"included_chat_ids"`
	ExcludedChatIDs []string `json:"excluded_chat_ids"`
	PinnedChatIDs   []string `json:"pinned_chat_ids"`
}

func toFolderResponse(f *store.ChatFolder) folderResponse {
	emoticon := ""
	if f.Emoticon != nil {
		emoticon = *f.Emoticon
	}
	included := f.IncludedChatIDs
	if included == nil {
		included = []string{}
	}
	excluded := f.ExcludedChatIDs
	if excluded == nil {
		excluded = []string{}
	}
	pinned := f.PinnedChatIDs
	if pinned == nil {
		pinned = []string{}
	}
	return folderResponse{
		ID:              f.ID,
		Title:           f.Title,
		Emoticon:        emoticon,
		Color:           f.Color,
		Position:        f.Position,
		IncludedChatIDs: included,
		ExcludedChatIDs: excluded,
		PinnedChatIDs:   pinned,
	}
}

func parseFolderID(c *fiber.Ctx) (int, error) {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil || id <= 0 {
		return 0, apperror.BadRequest("invalid folder ID")
	}
	return id, nil
}

func validateFolderRequest(req *folderRequest) error {
	if appErr := validator.RequireString(req.Title, "title", 1, 64); appErr != nil {
		return appErr
	}
	if len(req.IncludedChatIDs) > 200 {
		return apperror.BadRequest("included_chat_ids exceeds maximum of 200")
	}
	if len(req.ExcludedChatIDs) > 200 {
		return apperror.BadRequest("excluded_chat_ids exceeds maximum of 200")
	}
	if len(req.PinnedChatIDs) > 200 {
		return apperror.BadRequest("pinned_chat_ids exceeds maximum of 200")
	}
	for _, id := range req.IncludedChatIDs {
		if !validator.IsValidUUID(id) {
			return apperror.BadRequest("invalid UUID in included_chat_ids: " + id)
		}
	}
	for _, id := range req.ExcludedChatIDs {
		if !validator.IsValidUUID(id) {
			return apperror.BadRequest("invalid UUID in excluded_chat_ids: " + id)
		}
	}
	for _, id := range req.PinnedChatIDs {
		if !validator.IsValidUUID(id) {
			return apperror.BadRequest("invalid UUID in pinned_chat_ids: " + id)
		}
	}
	return nil
}

// ListFolders handles GET /folders.
func (h *FolderHandler) ListFolders(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	folders, err := h.svc.List(c.Context(), uid)
	if err != nil {
		return response.Error(c, err)
	}

	out := make([]folderResponse, len(folders))
	for i, f := range folders {
		out[i] = toFolderResponse(f)
	}
	return response.JSON(c, fiber.StatusOK, out)
}

// CreateFolder handles POST /folders.
func (h *FolderHandler) CreateFolder(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req folderRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("invalid request body"))
	}
	if err := validateFolderRequest(&req); err != nil {
		return response.Error(c, err)
	}

	var emoticon *string
	if req.Emoticon != "" {
		emoticon = &req.Emoticon
	}

	f := &store.ChatFolder{
		UserID:          uid,
		Title:           req.Title,
		Emoticon:        emoticon,
		Color:           req.Color,
		IncludedChatIDs: req.IncludedChatIDs,
		ExcludedChatIDs: req.ExcludedChatIDs,
		PinnedChatIDs:   req.PinnedChatIDs,
	}

	if err := h.svc.Create(c.Context(), f); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, toFolderResponse(f))
}

// GetFolder handles GET /folders/:id.
func (h *FolderHandler) GetFolder(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	folderID, err := parseFolderID(c)
	if err != nil {
		return response.Error(c, err)
	}

	folder, err := h.svc.Get(c.Context(), folderID, uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, toFolderResponse(folder))
}

// UpdateFolder handles PUT /folders/:id.
func (h *FolderHandler) UpdateFolder(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	folderID, err := parseFolderID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req folderRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("invalid request body"))
	}
	if err := validateFolderRequest(&req); err != nil {
		return response.Error(c, err)
	}

	var emoticon *string
	if req.Emoticon != "" {
		emoticon = &req.Emoticon
	}

	f := &store.ChatFolder{
		ID:              folderID,
		UserID:          uid,
		Title:           req.Title,
		Emoticon:        emoticon,
		Color:           req.Color,
		IncludedChatIDs: req.IncludedChatIDs,
		ExcludedChatIDs: req.ExcludedChatIDs,
		PinnedChatIDs:   req.PinnedChatIDs,
	}

	if err := h.svc.Update(c.Context(), f); err != nil {
		return response.Error(c, err)
	}

	// Re-fetch to get updated position and timestamps.
	updated, err := h.svc.Get(c.Context(), folderID, uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, toFolderResponse(updated))
}

// DeleteFolder handles DELETE /folders/:id.
func (h *FolderHandler) DeleteFolder(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	folderID, err := parseFolderID(c)
	if err != nil {
		return response.Error(c, err)
	}

	if err := h.svc.Delete(c.Context(), folderID, uid); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

// UpdateFolderOrder handles PUT /folders/order.
func (h *FolderHandler) UpdateFolderOrder(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req folderOrderRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("invalid request body"))
	}
	if len(req.FolderIDs) == 0 {
		return response.Error(c, apperror.BadRequest("folder_ids must not be empty"))
	}
	if len(req.FolderIDs) > 100 {
		return response.Error(c, apperror.BadRequest("folder_ids exceeds maximum of 100"))
	}

	seen := make(map[int]struct{}, len(req.FolderIDs))
	for _, id := range req.FolderIDs {
		if _, dup := seen[id]; dup {
			return response.Error(c, apperror.BadRequest("duplicate folder ID in folder_ids"))
		}
		seen[id] = struct{}{}
	}

	if err := h.svc.UpdateOrder(c.Context(), uid, req.FolderIDs); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}
