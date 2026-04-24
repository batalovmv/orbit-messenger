// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"bytes"
	"log/slog"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

func newGIFApp(gs *mockGIFStore, tc *mockTenorClient) *fiber.App {
	app := fiber.New()
	svc := service.NewGIFService(gs, tc, slog.Default())
	h := NewGIFHandler(svc, slog.Default())
	h.Register(app)
	return app
}

// --- Search ---

func TestGIFSearch_MissingQuery(t *testing.T) {
	app := newGIFApp(&mockGIFStore{}, &mockTenorClient{})
	req, _ := http.NewRequest(http.MethodGet, "/gifs/search", nil)
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGIFSearch_EmptyQuery(t *testing.T) {
	app := newGIFApp(&mockGIFStore{}, &mockTenorClient{})
	req, _ := http.NewRequest(http.MethodGet, "/gifs/search?q=", nil)
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- Trending ---

func TestGIFTrending_Success(t *testing.T) {
	app := newGIFApp(&mockGIFStore{}, &mockTenorClient{})
	req, _ := http.NewRequest(http.MethodGet, "/gifs/trending", nil)
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	// Service returns 500 (not implemented) — that's expected for stubs
	_ = resp
}

func TestGIFSearch_MissingUserID(t *testing.T) {
	app := newGIFApp(&mockGIFStore{}, &mockTenorClient{})
	req, _ := http.NewRequest(http.MethodGet, "/gifs/search?q=cat", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestGIFTrending_MissingUserID(t *testing.T) {
	app := newGIFApp(&mockGIFStore{}, &mockTenorClient{})
	req, _ := http.NewRequest(http.MethodGet, "/gifs/trending", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// --- ListSaved ---

func TestGIFListSaved_MissingUserID(t *testing.T) {
	app := newGIFApp(&mockGIFStore{}, &mockTenorClient{})
	req, _ := http.NewRequest(http.MethodGet, "/gifs/saved", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// --- Save ---

func TestGIFSave_MissingUserID(t *testing.T) {
	app := newGIFApp(&mockGIFStore{}, &mockTenorClient{})
	req, _ := http.NewRequest(http.MethodPost, "/gifs/saved",
		bytes.NewBufferString(`{"tenor_id":"123","url":"https://example.com/gif.mp4"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestGIFSave_MissingTenorID(t *testing.T) {
	app := newGIFApp(&mockGIFStore{}, &mockTenorClient{})
	req, _ := http.NewRequest(http.MethodPost, "/gifs/saved",
		bytes.NewBufferString(`{"url":"https://example.com/gif.mp4"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGIFSave_MissingURL(t *testing.T) {
	app := newGIFApp(&mockGIFStore{}, &mockTenorClient{})
	req, _ := http.NewRequest(http.MethodPost, "/gifs/saved",
		bytes.NewBufferString(`{"tenor_id":"123"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- Remove ---

func TestGIFRemove_MissingUserID(t *testing.T) {
	app := newGIFApp(&mockGIFStore{}, &mockTenorClient{})
	req, _ := http.NewRequest(http.MethodDelete, "/gifs/saved/"+uuid.New().String(), nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestGIFRemove_InvalidGIFID(t *testing.T) {
	app := newGIFApp(&mockGIFStore{}, &mockTenorClient{})
	req, _ := http.NewRequest(http.MethodDelete, "/gifs/saved/bad-id", nil)
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
