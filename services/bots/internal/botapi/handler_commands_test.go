// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/bots/internal/model"
)

type botAPICommandStore struct {
	commands []model.BotCommand
	deleted  bool
}

func (s *botAPICommandStore) SetCommands(ctx context.Context, botID uuid.UUID, commands []model.BotCommand) error {
	s.commands = append([]model.BotCommand(nil), commands...)
	return nil
}

func (s *botAPICommandStore) GetCommands(ctx context.Context, botID uuid.UUID) ([]model.BotCommand, error) {
	return append([]model.BotCommand(nil), s.commands...), nil
}

func (s *botAPICommandStore) DeleteAllForBot(ctx context.Context, botID uuid.UUID) error {
	s.deleted = true
	s.commands = nil
	return nil
}

func newBotAPICommandTestApp(t *testing.T, commandStore *botAPICommandStore) (*fiber.App, *model.Bot) {
	t.Helper()

	bot := &model.Bot{
		ID:          uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		UserID:      uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
		DisplayName: "Command Bot",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		IsActive:    true,
	}
	svc := &rateLimitBotService{bots: map[string]*model.Bot{"bot-cmd": bot}}
	h := &BotAPIHandler{
		svc:          svc,
		commandStore: commandStore,
		fileIDCodec:  NewFileIDCodec([]byte("command-handler-test-secret")),
	}

	app := fiber.New(fiber.Config{ErrorHandler: response.FiberErrorHandler})
	group := app.Group("/bot/:token", TokenAuthMiddleware(h.svc))
	h.Register(group)
	return app, bot
}

func TestSetMyCommands_StoresValidatedCommands(t *testing.T) {
	store := &botAPICommandStore{}
	app, bot := newBotAPICommandTestApp(t, store)

	body, _ := json.Marshal(map[string]any{"commands": []map[string]string{
		{"command": " start ", "description": " Start bot "},
		{"command": "status_1", "description": "Show status"},
	}})
	req := httptest.NewRequest(http.MethodPost, "/bot/bot-cmd/setMyCommands", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	if len(store.commands) != 2 {
		t.Fatalf("commands=%d, want 2", len(store.commands))
	}
	if store.commands[0].BotID != bot.ID || store.commands[0].Command != "start" || store.commands[0].Description != "Start bot" {
		t.Fatalf("stored command not normalised: %#v", store.commands[0])
	}

	req = httptest.NewRequest(http.MethodGet, "/bot/bot-cmd/getMyCommands", nil)
	resp, err = app.Test(req, -1)
	if err != nil {
		t.Fatalf("get app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d, want 200", resp.StatusCode)
	}
}

func TestSetMyCommands_RejectsInvalidCommand(t *testing.T) {
	store := &botAPICommandStore{}
	app, _ := newBotAPICommandTestApp(t, store)

	body, _ := json.Marshal(map[string]any{"commands": []map[string]string{
		{"command": "Bad-Command", "description": "Nope"},
	}})
	req := httptest.NewRequest(http.MethodPost, "/bot/bot-cmd/setMyCommands", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
	if len(store.commands) != 0 {
		t.Fatalf("invalid command was stored: %#v", store.commands)
	}
}

func TestDeleteMyCommands_ClearsStore(t *testing.T) {
	store := &botAPICommandStore{commands: []model.BotCommand{{Command: "start", Description: "Start"}}}
	app, _ := newBotAPICommandTestApp(t, store)

	req := httptest.NewRequest(http.MethodPost, "/bot/bot-cmd/deleteMyCommands", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	if !store.deleted || len(store.commands) != 0 {
		t.Fatalf("commands not deleted: deleted=%v commands=%#v", store.deleted, store.commands)
	}
}
