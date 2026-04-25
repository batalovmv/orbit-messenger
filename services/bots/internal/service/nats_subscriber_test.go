// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/bots/internal/botapi"
)

func TestBuildBotUpdate_PopulatesDocumentForFileMedia(t *testing.T) {
	chatID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	botID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	mediaID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	senderID := uuid.MustParse("44444444-4444-4444-4444-444444444444")

	codec := botapi.NewFileIDCodec([]byte("test-secret-bytes"))

	payloadJSON, err := json.Marshal(map[string]any{
		"id":          "55555555-5555-5555-5555-555555555555",
		"chat_id":     chatID.String(),
		"sender_id":   senderID.String(),
		"sender_name": "Alice",
		"content":     "report Q1",
		"type":        "file",
		"created_at":  "2026-04-26T10:00:00Z",
		"media_attachments": []map[string]any{
			{
				"media_id":          mediaID.String(),
				"type":              "file",
				"mime_type":         "application/pdf",
				"original_filename": "report.pdf",
				"size_bytes":        12345,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	event := natsEvent{
		Event:    "new_message",
		Data:     payloadJSON,
		SenderID: senderID.String(),
	}
	parsed := parseMessagePayload(event.Data)
	update := buildBotUpdate(chatID, event, parsed, botID, codec)
	msg := update.Message
	if msg == nil {
		t.Fatal("nil message")
	}
	if msg.Document == nil {
		t.Fatal("expected Document populated")
	}
	if msg.Document.FileName != "report.pdf" {
		t.Fatalf("filename=%q", msg.Document.FileName)
	}
	if msg.Document.MimeType != "application/pdf" {
		t.Fatalf("mime=%q", msg.Document.MimeType)
	}
	if msg.Document.FileSize != 12345 {
		t.Fatalf("size=%d", msg.Document.FileSize)
	}
	if msg.Document.FileID == "" {
		t.Fatal("empty file_id")
	}
	gotMedia, gotChat, err := codec.Decode(msg.Document.FileID, botID)
	if err != nil {
		t.Fatalf("decode file_id: %v", err)
	}
	if gotMedia != mediaID || gotChat != chatID {
		t.Fatalf("file_id payload mismatch: %v / %v", gotMedia, gotChat)
	}
	if msg.Caption != "report Q1" {
		t.Fatalf("caption=%q (media should be in Caption, not Text)", msg.Caption)
	}
	if msg.Text != "" {
		t.Fatalf("text=%q (should be empty when media present)", msg.Text)
	}
	if msg.From == nil || msg.From.FirstName != "Alice" {
		t.Fatalf("from=%+v", msg.From)
	}
}

func TestBuildBotUpdate_MintsDifferentFileIDsPerBot(t *testing.T) {
	chatID := uuid.New()
	mediaID := uuid.New()
	codec := botapi.NewFileIDCodec([]byte("multi-bot-secret"))

	payloadJSON, _ := json.Marshal(map[string]any{
		"id":      uuid.NewString(),
		"chat_id": chatID.String(),
		"type":    "photo",
		"media_attachments": []map[string]any{
			{"media_id": mediaID.String(), "type": "photo"},
		},
	})
	event := natsEvent{Data: payloadJSON}
	parsed := parseMessagePayload(event.Data)

	botA := uuid.New()
	botB := uuid.New()
	updA := buildBotUpdate(chatID, event, parsed, botA, codec)
	updB := buildBotUpdate(chatID, event, parsed, botB, codec)
	if updA.Message.Photo[0].FileID == updB.Message.Photo[0].FileID {
		t.Fatal("file_id must differ between bots for the same media")
	}
	// file_unique_id is bot-independent.
	if updA.Message.Photo[0].FileUniqueID != updB.Message.Photo[0].FileUniqueID {
		t.Fatal("file_unique_id must be stable across bots")
	}
}

func TestBuildBotUpdate_PlainTextHasNoCaption(t *testing.T) {
	chatID := uuid.New()
	codec := botapi.NewFileIDCodec([]byte("plain-text-secret"))
	payloadJSON, _ := json.Marshal(map[string]any{
		"id":          uuid.NewString(),
		"chat_id":     chatID.String(),
		"sender_id":   uuid.NewString(),
		"sender_name": "Bob",
		"content":     "hello world",
		"type":        "text",
	})
	event := natsEvent{Data: payloadJSON}
	parsed := parseMessagePayload(event.Data)
	update := buildBotUpdate(chatID, event, parsed, uuid.New(), codec)
	if update.Message.Text != "hello world" {
		t.Fatalf("text=%q", update.Message.Text)
	}
	if update.Message.Caption != "" {
		t.Fatalf("caption should be empty for plain text, got %q", update.Message.Caption)
	}
	if update.Message.Document != nil || len(update.Message.Photo) > 0 {
		t.Fatal("no media should be populated for plain text")
	}
}

func TestBuildBotUpdate_PassesEntities(t *testing.T) {
	chatID := uuid.New()
	codec := botapi.NewFileIDCodec([]byte("entities-secret"))
	entitiesJSON := json.RawMessage(`[{"type":"MessageEntityBold","offset":0,"length":4}]`)
	payloadJSON, _ := json.Marshal(map[string]any{
		"id":       uuid.NewString(),
		"chat_id":  chatID.String(),
		"content":  "bold text",
		"type":     "text",
		"entities": entitiesJSON,
	})
	event := natsEvent{Data: payloadJSON}
	parsed := parseMessagePayload(event.Data)
	update := buildBotUpdate(chatID, event, parsed, uuid.New(), codec)
	if len(update.Message.Entities) != 1 {
		t.Fatalf("entities=%+v", update.Message.Entities)
	}
	if update.Message.Entities[0].Type != "MessageEntityBold" {
		t.Fatalf("type=%q", update.Message.Entities[0].Type)
	}
}
