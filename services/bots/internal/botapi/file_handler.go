// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botapi

import (
	"errors"
	"io"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/validator"
	"github.com/mst-corp/orbit/services/bots/internal/client"
	"github.com/mst-corp/orbit/services/bots/internal/model"
)

// getFile validates a file_id and returns the metadata bots need to download.
//
//	GET /bot/<token>/getFile?file_id=<id>
//
// Validation:
//  1. file_id has a valid HMAC signature bound to this bot.
//  2. The bot is currently installed in the chat encoded in the file_id.
//  3. The media exists and is accessible to the bot user.
//
// Response.file_path is just the file_id echoed back. Bots assemble the
// download URL as `<base>/api/v1/bot/<token>/file/<file_path>` (route below).
func (h *BotAPIHandler) getFile(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}
	if h.fileIDCodec == nil {
		return botError(c, apperror.Internal("file_id codec not configured"))
	}
	if h.mediaClient == nil {
		return botError(c, apperror.Internal("media service not configured"))
	}

	fileID := strings.TrimSpace(c.Query("file_id"))
	if fileID == "" {
		return botError(c, apperror.BadRequest("file_id is required"))
	}

	mediaID, chatID, err := h.fileIDCodec.Decode(fileID, bot.ID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid file_id"))
	}

	installed, err := h.svc.IsBotInstalled(c.Context(), bot.ID, chatID)
	if err != nil {
		return botError(c, err)
	}
	if !installed {
		return botError(c, apperror.Forbidden("Bot is not installed in the source chat"))
	}

	info, err := h.mediaClient.GetInfo(c.Context(), bot.UserID, mediaID)
	if err != nil {
		var ce *client.ClientError
		if errors.As(err, &ce) && ce.StatusCode == fiber.StatusNotFound {
			return botError(c, apperror.NotFound("File not found"))
		}
		return botError(c, err)
	}

	return botSuccess(c, APIFile{
		FileID:       fileID,
		FileUniqueID: h.fileIDCodec.EncodeUnique(mediaID),
		FileSize:     info.SizeBytes,
		FilePath:     fileID,
	})
}

// downloadFile streams the file payload back to the bot.
//
//	GET /bot/<token>/file/<file_id>
//
// Re-validates the file_id signature and bot installation as a defence in
// depth — bots that lose tokens or move between chats shouldn't be able to
// download files outside their visibility window.
func (h *BotAPIHandler) downloadFile(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}
	if h.fileIDCodec == nil {
		return botError(c, apperror.Internal("file_id codec not configured"))
	}
	if h.mediaClient == nil {
		return botError(c, apperror.Internal("media service not configured"))
	}

	fileID := strings.TrimSpace(c.Params("file_id"))
	mediaID, chatID, err := h.fileIDCodec.Decode(fileID, bot.ID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid file_id"))
	}

	installed, err := h.svc.IsBotInstalled(c.Context(), bot.ID, chatID)
	if err != nil {
		return botError(c, err)
	}
	if !installed {
		return botError(c, apperror.Forbidden("Bot is not installed in the source chat"))
	}

	body, headers, statusCode, err := h.mediaClient.StreamFile(c.Context(), bot.UserID, mediaID, c.Get("Range"))
	if err != nil {
		var ce *client.ClientError
		if errors.As(err, &ce) {
			switch ce.StatusCode {
			case fiber.StatusUnauthorized:
				return botError(c, apperror.Unauthorized(ce.Message))
			case fiber.StatusForbidden:
				return botError(c, apperror.Forbidden(ce.Message))
			case fiber.StatusNotFound:
				return botError(c, apperror.NotFound("File not found"))
			default:
				return botError(c, apperror.Internal(ce.Message))
			}
		}
		return botError(c, err)
	}
	defer body.Close()

	if ct := headers.Get("Content-Type"); ct != "" {
		c.Set("Content-Type", ct)
	}
	if cl := headers.Get("Content-Length"); cl != "" {
		c.Set("Content-Length", cl)
	}
	if cr := headers.Get("Content-Range"); cr != "" {
		c.Set("Content-Range", cr)
	}
	if ar := headers.Get("Accept-Ranges"); ar != "" {
		c.Set("Accept-Ranges", ar)
	}
	c.Status(statusCode)
	if _, err := io.Copy(c, body); err != nil {
		return err
	}
	return nil
}

// EncodeFileID is a convenience wrapper used by NATS update builders that
// need to mint file_ids for incoming media without exposing the codec.
func (h *BotAPIHandler) EncodeFileID(mediaID, chatID, botID uuid.UUID) string {
	if h.fileIDCodec == nil {
		return ""
	}
	return h.fileIDCodec.Encode(mediaID, chatID, botID)
}

// EncodeFileUniqueID is the bot-independent stable identifier used in the
// `file_unique_id` field of media entities embedded in updates.
func (h *BotAPIHandler) EncodeFileUniqueID(mediaID uuid.UUID) string {
	if h.fileIDCodec == nil {
		return ""
	}
	return h.fileIDCodec.EncodeUnique(mediaID)
}

// sendMediaByFileID handles the JSON variant of sendDocument/sendPhoto/...
// where the bot supplies a previously-issued file_id instead of re-uploading
// bytes. The bot must still be installed in the chat encoded into the
// file_id; the destination chat may differ.
func (h *BotAPIHandler) sendMediaByFileID(c *fiber.Ctx, fieldName, msgType string, bot *model.Bot) error {
	if h.fileIDCodec == nil {
		return botError(c, apperror.Internal("file_id codec not configured"))
	}

	var req SendDocumentByFileIDRequest
	if err := c.BodyParser(&req); err != nil {
		return botError(c, apperror.BadRequest("Invalid request body"))
	}
	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return botError(c, err)
	}
	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid chat_id"))
	}
	if err := h.svc.CheckBotScope(c.Context(), bot.ID, chatID, model.ScopePostMessages); err != nil {
		return botError(c, err)
	}

	fileID := strings.TrimSpace(pickFileIDField(req, fieldName))
	if fileID == "" {
		return botError(c, apperror.BadRequest("Missing "+fieldName+" file_id"))
	}
	mediaID, sourceChatID, err := h.fileIDCodec.Decode(fileID, bot.ID)
	if err != nil {
		return botError(c, apperror.BadRequest("Invalid file_id"))
	}
	installed, err := h.svc.IsBotInstalled(c.Context(), bot.ID, sourceChatID)
	if err != nil {
		return botError(c, err)
	}
	if !installed {
		return botError(c, apperror.Forbidden("Bot is not installed in the source chat"))
	}

	if err := ValidateReplyMarkup(req.ReplyMarkup); err != nil {
		return botError(c, err)
	}

	finalCaption, entities, err := resolveTextAndEntities(req.Caption, req.ParseMode, req.CaptionEntities)
	if err != nil {
		return botError(c, err)
	}
	entitiesJSON, err := encodeEntities(entities)
	if err != nil {
		return botError(c, err)
	}

	var replyToID *uuid.UUID
	if req.ReplyToMessageID != nil && strings.TrimSpace(*req.ReplyToMessageID) != "" {
		parsed, parseErr := uuid.Parse(*req.ReplyToMessageID)
		if parseErr != nil {
			return botError(c, apperror.BadRequest("Invalid reply_to_message_id"))
		}
		replyToID = &parsed
	}

	message, err := h.msgClient.SendMessage(c.Context(), bot.UserID, chatID, finalCaption, msgType, client.SendMessageOptions{
		ReplyMarkup: req.ReplyMarkup,
		ReplyToID:   replyToID,
		Entities:    entitiesJSON,
		MediaIDs:    []string{mediaID.String()},
	})
	if err != nil {
		return botError(c, err)
	}
	return botSuccess(c, message)
}

// pickFileIDField returns the value of the field that matches the media kind
// for the current sendXXX endpoint. Bots typically send `document`/`photo`/
// etc. but we accept the generic `document` field as a fallback.
func pickFileIDField(req SendDocumentByFileIDRequest, fieldName string) string {
	switch fieldName {
	case "document":
		return req.Document
	case "photo":
		return req.Photo
	case "video":
		return req.Video
	case "audio":
		return req.Audio
	case "voice":
		return req.Voice
	}
	return ""
}

