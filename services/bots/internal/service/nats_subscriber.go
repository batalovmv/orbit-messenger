// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/bots/internal/botapi"
	"github.com/mst-corp/orbit/services/bots/internal/model"
	"github.com/mst-corp/orbit/services/bots/internal/store"
	"github.com/nats-io/nats.go"
)

// BotFatherInterceptor is implemented by botfather.BotFather to handle DM messages.
type BotFatherInterceptor interface {
	HandleIfBotFatherDM(ctx context.Context, chatID uuid.UUID, senderID string, content string, messageType string, attachments []IncomingAttachment) bool
	UserID() uuid.UUID
}

// IncomingAttachment is the minimal media payload BotFather needs for /setuserpic.
type IncomingAttachment struct {
	MediaID  string
	Type     string
	MimeType string
	URL      string
}

type BotNATSSubscriber struct {
	nc            *nats.Conn
	installations store.InstallationStore
	webhookWorker *WebhookWorker
	updateQueue   *UpdateQueue
	fileIDCodec   *botapi.FileIDCodec
	botFather     BotFatherInterceptor
	logger        *slog.Logger
}

type natsEvent struct {
	Event     string          `json:"event"`
	Data      json.RawMessage `json:"data"`
	MemberIDs []string        `json:"member_ids"`
	SenderID  string          `json:"sender_id,omitempty"`
	Timestamp string          `json:"timestamp"`
}

func NewBotNATSSubscriber(
	nc *nats.Conn,
	installations store.InstallationStore,
	webhookWorker *WebhookWorker,
	updateQueue *UpdateQueue,
	fileIDCodec *botapi.FileIDCodec,
	logger *slog.Logger,
) *BotNATSSubscriber {
	return &BotNATSSubscriber{
		nc:            nc,
		installations: installations,
		webhookWorker: webhookWorker,
		updateQueue:   updateQueue,
		fileIDCodec:   fileIDCodec,
		logger:        logger,
	}
}

// SetBotFather sets the BotFather interceptor for DM message handling.
func (s *BotNATSSubscriber) SetBotFather(bf BotFatherInterceptor) {
	s.botFather = bf
}

func (s *BotNATSSubscriber) Start() error {
	if s.nc == nil {
		return fmt.Errorf("nats connection is not configured")
	}

	if _, err := s.nc.Subscribe("orbit.chat.*.message.new", s.handleEvent); err != nil {
		return fmt.Errorf("subscribe message events: %w", err)
	}
	if _, err := s.nc.Subscribe("orbit.chat.*.member.*", s.handleEvent); err != nil {
		return fmt.Errorf("subscribe member events: %w", err)
	}

	return nil
}

func (s *BotNATSSubscriber) handleEvent(msg *nats.Msg) {
	var event natsEvent
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		s.logger.Error("failed to decode bot nats event", "error", err, "subject", msg.Subject)
		return
	}

	chatID := extractChatID(msg.Subject)
	if chatID == uuid.Nil {
		s.logger.Warn("failed to extract chat id from subject", "subject", msg.Subject)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// BotFather interception: handle DM messages before normal bot delivery.
	// Check if BotFather's userID is in the event's member_ids list (meaning it's a participant in this chat).
	if s.botFather != nil && strings.HasSuffix(msg.Subject, ".message.new") {
		bfUID := s.botFather.UserID().String()
		isBFChat := false
		for _, mid := range event.MemberIDs {
			if mid == bfUID {
				isBFChat = true
				break
			}
		}
		if isBFChat {
			var payload struct {
				SenderID         string `json:"sender_id"`
				Content          string `json:"content"`
				Type             string `json:"type"`
				MediaAttachments []struct {
					MediaID  string `json:"media_id"`
					Type     string `json:"type"`
					MimeType string `json:"mime_type"`
					URL      string `json:"url"`
				} `json:"media_attachments,omitempty"`
			}
			if err := json.Unmarshal(event.Data, &payload); err == nil {
				senderID := payload.SenderID
				if senderID == "" {
					senderID = event.SenderID
				}
				msgType := payload.Type
				if msgType == "" {
					msgType = "text"
				}
				attachments := make([]IncomingAttachment, 0, len(payload.MediaAttachments))
				for _, a := range payload.MediaAttachments {
					attachments = append(attachments, IncomingAttachment{
						MediaID: a.MediaID, Type: a.Type, MimeType: a.MimeType, URL: a.URL,
					})
				}
				if s.botFather.HandleIfBotFatherDM(ctx, chatID, senderID, payload.Content, msgType, attachments) {
					return
				}
			}
		}
	}

	bots, err := s.installations.ListChatsWithWebhookBots(ctx, chatID)
	if err != nil {
		s.logger.Error("failed to list installed bots for chat", "chat_id", chatID, "error", err)
		return
	}

	// Decode payload once; per-bot update builder reuses it to mint
	// bot-specific file_ids without re-parsing JSON for each delivery.
	parsed := parseMessagePayload(event.Data)

	for _, info := range bots {
		if event.SenderID != "" && info.UserID.String() == event.SenderID {
			continue
		}

		// Enforce scopes: only deliver message events to bots with ScopeReadMessages.
		// Legacy installations (scopes=0) get full access for backward compatibility.
		if info.Scopes != 0 && info.Scopes&model.ScopeReadMessages == 0 {
			continue
		}

		update := buildBotUpdate(chatID, event, parsed, info.BotID, s.fileIDCodec)

		if info.WebhookURL != "" {
			secretHash := ""
			if info.SecretHash != nil {
				secretHash = *info.SecretHash
			}
			if err := s.webhookWorker.Enqueue(info.BotID, info.WebhookURL, secretHash, update); err != nil {
				s.logger.Error("enqueue webhook delivery failed", "bot_id", info.BotID, "error", err)
			}
			continue
		}

		if err := s.updateQueue.Push(info.BotID, update); err != nil {
			s.logger.Error("push bot update failed", "bot_id", info.BotID, "error", err)
		}
	}
}

func extractChatID(subject string) uuid.UUID {
	parts := strings.Split(subject, ".")
	if len(parts) < 3 {
		return uuid.Nil
	}
	chatID, err := uuid.Parse(parts[2])
	if err != nil {
		return uuid.Nil
	}
	return chatID
}

// messagePayload mirrors the messaging service "new_message" payload shape we
// rely on. Only fields that map onto a Bot API Update are decoded; unknown
// fields are ignored.
type messagePayload struct {
	ID               string                `json:"id"`
	ChatID           string                `json:"chat_id"`
	SenderID         string                `json:"sender_id"`
	SenderName       string                `json:"sender_name"`
	SenderAvatarURL  *string               `json:"sender_avatar_url"`
	Content          *string               `json:"content"`
	Type             string                `json:"type"`
	Entities         json.RawMessage       `json:"entities"`
	CreatedAt        string                `json:"created_at"`
	MediaAttachments []natsMediaAttachment `json:"media_attachments"`
}

type natsMediaAttachment struct {
	MediaID          string   `json:"media_id"`
	Type             string   `json:"type"`
	MimeType         string   `json:"mime_type"`
	OriginalFilename string   `json:"original_filename"`
	SizeBytes        int64    `json:"size_bytes"`
	Width            *int     `json:"width"`
	Height           *int     `json:"height"`
	DurationSeconds  *float64 `json:"duration_seconds"`
}

func parseMessagePayload(raw json.RawMessage) messagePayload {
	var p messagePayload
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	return p
}

func buildBotUpdate(chatID uuid.UUID, event natsEvent, payload messagePayload, botID uuid.UUID, codec *botapi.FileIDCodec) botapi.Update {
	message := &botapi.APIMessage{
		MessageID: uuid.NewString(),
		ChatID:    chatID.String(),
		FromID:    event.SenderID,
		Date:      time.Now().Unix(),
	}

	if payload.ID != "" {
		message.MessageID = payload.ID
	}
	if payload.ChatID != "" {
		message.ChatID = payload.ChatID
	}
	if payload.SenderID != "" {
		message.FromID = payload.SenderID
	}
	if payload.SenderName != "" {
		message.FromName = payload.SenderName
	}
	if payload.CreatedAt != "" {
		if createdAt, err := time.Parse(time.RFC3339, payload.CreatedAt); err == nil {
			message.Date = createdAt.Unix()
		}
	}

	content := ""
	if payload.Content != nil {
		content = *payload.Content
	}
	hasMedia := len(payload.MediaAttachments) > 0
	if hasMedia {
		message.Caption = content
	} else {
		message.Text = content
	}

	if len(payload.Entities) > 0 {
		var ents []botapi.OrbitEntity
		if err := json.Unmarshal(payload.Entities, &ents); err == nil && len(ents) > 0 {
			if hasMedia {
				message.CaptionEntities = ents
			} else {
				message.Entities = ents
			}
		}
	}

	if message.FromID != "" {
		fromUUID, err := uuid.Parse(message.FromID)
		if err == nil {
			message.From = &botapi.APIUser{
				ID:        fromUUID.String(),
				FirstName: payload.SenderName,
			}
		}
	}

	populateMedia(message, payload, chatID, botID, codec)

	return botapi.Update{Message: message}
}

// populateMedia attaches the first media item that matches each Bot API field.
// Multiple items of the same kind are collapsed into the first one because TG
// Update.message has only one slot per kind (photo[] is special-cased: it
// holds size variants of a single photo, not a gallery).
func populateMedia(message *botapi.APIMessage, payload messagePayload, chatID, botID uuid.UUID, codec *botapi.FileIDCodec) {
	if codec == nil {
		return
	}
	for _, att := range payload.MediaAttachments {
		mediaID, err := uuid.Parse(att.MediaID)
		if err != nil {
			continue
		}
		fileID := codec.Encode(mediaID, chatID, botID)
		fileUniqueID := codec.EncodeUnique(mediaID)

		switch att.Type {
		case "photo":
			if message.Photo != nil {
				continue
			}
			ps := botapi.APIPhotoSize{
				FileID:       fileID,
				FileUniqueID: fileUniqueID,
				FileSize:     att.SizeBytes,
			}
			if att.Width != nil {
				ps.Width = *att.Width
			}
			if att.Height != nil {
				ps.Height = *att.Height
			}
			message.Photo = []botapi.APIPhotoSize{ps}
		case "video":
			if message.Video != nil {
				continue
			}
			v := &botapi.APIVideo{
				FileID:       fileID,
				FileUniqueID: fileUniqueID,
				MimeType:     att.MimeType,
				FileSize:     att.SizeBytes,
			}
			if att.Width != nil {
				v.Width = *att.Width
			}
			if att.Height != nil {
				v.Height = *att.Height
			}
			if att.DurationSeconds != nil {
				v.Duration = int(*att.DurationSeconds)
			}
			message.Video = v
		case "audio":
			if message.Audio != nil {
				continue
			}
			a := &botapi.APIAudio{
				FileID:       fileID,
				FileUniqueID: fileUniqueID,
				MimeType:     att.MimeType,
				FileSize:     att.SizeBytes,
			}
			if att.DurationSeconds != nil {
				a.Duration = int(*att.DurationSeconds)
			}
			message.Audio = a
		case "voice":
			if message.Voice != nil {
				continue
			}
			vc := &botapi.APIVoice{
				FileID:       fileID,
				FileUniqueID: fileUniqueID,
				MimeType:     att.MimeType,
				FileSize:     att.SizeBytes,
			}
			if att.DurationSeconds != nil {
				vc.Duration = int(*att.DurationSeconds)
			}
			message.Voice = vc
		default:
			// `file`, `gif`, `videonote`, `sticker`, etc. are mapped to
			// document for now — Bot API has dedicated fields for some of
			// these but document is the safe fallback the SDKs accept.
			if message.Document != nil {
				continue
			}
			message.Document = &botapi.APIDocument{
				FileID:       fileID,
				FileUniqueID: fileUniqueID,
				FileName:     att.OriginalFilename,
				MimeType:     att.MimeType,
				FileSize:     att.SizeBytes,
			}
		}
	}
}
