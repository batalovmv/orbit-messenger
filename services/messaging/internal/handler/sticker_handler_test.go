package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

func newStickerApp(ss *mockStickerStore) *fiber.App {
	app := fiber.New()
	svc := service.NewStickerService(ss, nil)
	h := NewStickerHandler(svc, nil)
	h.Register(app)
	return app
}

func makeJSONRequest(method, target string, body any) (*http.Request, error) {
	if body == nil {
		return http.NewRequest(method, target, nil)
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(method, target, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// --- User routes ---

func TestListFeatured_MissingUserID(t *testing.T) {
	app := newStickerApp(&mockStickerStore{})
	req, _ := http.NewRequest(http.MethodGet, "/stickers/featured", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestSearchStickers_MissingQuery(t *testing.T) {
	app := newStickerApp(&mockStickerStore{})
	req, _ := http.NewRequest(http.MethodGet, "/stickers/search", nil)
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestSearchStickers_EmptyQuery(t *testing.T) {
	app := newStickerApp(&mockStickerStore{})
	req, _ := http.NewRequest(http.MethodGet, "/stickers/search?q=", nil)
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGetPack_MissingUserID(t *testing.T) {
	app := newStickerApp(&mockStickerStore{})
	req, _ := http.NewRequest(http.MethodGet, "/stickers/sets/"+uuid.New().String(), nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestGetPack_InvalidID(t *testing.T) {
	app := newStickerApp(&mockStickerStore{})
	req, _ := http.NewRequest(http.MethodGet, "/stickers/sets/bad-id", nil)
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestInstall_MissingUserID(t *testing.T) {
	app := newStickerApp(&mockStickerStore{})
	req, _ := http.NewRequest(http.MethodPost, "/stickers/sets/"+uuid.New().String()+"/install", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestInstall_InvalidPackID(t *testing.T) {
	app := newStickerApp(&mockStickerStore{})
	req, _ := http.NewRequest(http.MethodPost, "/stickers/sets/bad-id/install", nil)
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUninstall_MissingUserID(t *testing.T) {
	app := newStickerApp(&mockStickerStore{})
	req, _ := http.NewRequest(http.MethodDelete, "/stickers/sets/"+uuid.New().String()+"/install", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestListInstalled_MissingUserID(t *testing.T) {
	app := newStickerApp(&mockStickerStore{})
	req, _ := http.NewRequest(http.MethodGet, "/stickers/installed", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestListRecent_MissingUserID(t *testing.T) {
	app := newStickerApp(&mockStickerStore{})
	req, _ := http.NewRequest(http.MethodGet, "/stickers/recent", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// --- Admin routes ---

func TestCreateStickerPack_AdminSuccess(t *testing.T) {
	var createdPack *model.StickerPack
	adminID := uuid.New()

	app := newStickerApp(&mockStickerStore{
		getPackByShortNameFn: func(_ context.Context, shortName string) (*model.StickerPack, error) {
			if shortName != "orbit_ops" {
				t.Fatalf("unexpected short_name: %s", shortName)
			}
			return nil, nil
		},
		createPackFn: func(_ context.Context, pack *model.StickerPack, stickers []model.Sticker) error {
			if pack.Title != "Orbit Ops" {
				t.Fatalf("unexpected title: %s", pack.Title)
			}
			if pack.Description == nil || *pack.Description != "Ops pack" {
				t.Fatalf("unexpected description: %#v", pack.Description)
			}
			if pack.AuthorID == nil || *pack.AuthorID != adminID {
				t.Fatalf("unexpected author id: %#v", pack.AuthorID)
			}
			if len(stickers) != 0 {
				t.Fatalf("expected no stickers on pack create, got %d", len(stickers))
			}
			pack.ID = uuid.New()
			createdPack = pack
			return nil
		},
	})

	req, err := makeJSONRequest(http.MethodPost, "/admin/sticker-packs", map[string]any{
		"name":          "Orbit Ops",
		"short_name":    "orbit_ops",
		"description":   "Ops pack",
		"thumbnail_url": "https://cdn.example.com/orbit-ops.svg",
	})
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-User-ID", adminID.String())
	req.Header.Set("X-User-Role", "admin")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if createdPack == nil || !createdPack.IsOfficial || !createdPack.IsFeatured {
		t.Fatalf("expected official featured pack to be created")
	}
}

func TestCreateStickerPack_NonAdminForbidden(t *testing.T) {
	app := newStickerApp(&mockStickerStore{})

	req, err := makeJSONRequest(http.MethodPost, "/admin/sticker-packs", map[string]any{
		"name":       "Orbit Ops",
		"short_name": "orbit_ops",
	})
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-User-ID", uuid.New().String())
	req.Header.Set("X-User-Role", "member")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestCreateStickerPack_InvalidBody(t *testing.T) {
	app := newStickerApp(&mockStickerStore{})

	req, err := makeJSONRequest(http.MethodPost, "/admin/sticker-packs", map[string]any{
		"name":       "Orbit Ops",
		"short_name": "BAD SHORT NAME",
	})
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-User-ID", uuid.New().String())
	req.Header.Set("X-User-Role", "admin")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAddSticker_AdminSuccess(t *testing.T) {
	packID := uuid.New()
	var addedSticker *model.Sticker

	app := newStickerApp(&mockStickerStore{
		getPackFn: func(_ context.Context, id uuid.UUID) (*model.StickerPack, error) {
			if id != packID {
				t.Fatalf("unexpected pack id: %s", id)
			}
			return &model.StickerPack{ID: packID, Title: "Orbit Ops", ShortName: "orbit_ops"}, nil
		},
		addStickerFn: func(_ context.Context, id uuid.UUID, sticker *model.Sticker) error {
			if id != packID {
				t.Fatalf("unexpected pack id: %s", id)
			}
			if sticker.FileType != "svg" {
				t.Fatalf("expected svg file type, got %s", sticker.FileType)
			}
			sticker.ID = uuid.New()
			sticker.Position = 4
			addedSticker = sticker
			return nil
		},
	})

	req, err := makeJSONRequest(http.MethodPost, "/admin/sticker-packs/"+packID.String()+"/stickers", map[string]any{
		"emoji":       "📡",
		"file_url":    "https://cdn.example.com/stickers/ping.svg",
		"width":       512,
		"height":      512,
		"is_animated": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-User-ID", uuid.New().String())
	req.Header.Set("X-User-Role", "admin")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if addedSticker == nil || addedSticker.Position != 4 {
		t.Fatalf("expected sticker to be added")
	}
}

func TestAddSticker_MissingUserID(t *testing.T) {
	app := newStickerApp(&mockStickerStore{})
	req, err := makeJSONRequest(http.MethodPost, "/admin/sticker-packs/"+uuid.New().String()+"/stickers", map[string]any{
		"emoji":    "📡",
		"file_url": "https://cdn.example.com/stickers/ping.svg",
		"width":    512,
		"height":   512,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAddSticker_InvalidBody(t *testing.T) {
	app := newStickerApp(&mockStickerStore{
		getPackFn: func(_ context.Context, id uuid.UUID) (*model.StickerPack, error) {
			return &model.StickerPack{ID: id, Title: "Orbit Ops", ShortName: "orbit_ops"}, nil
		},
	})

	req, err := makeJSONRequest(http.MethodPost, "/admin/sticker-packs/"+uuid.New().String()+"/stickers", map[string]any{
		"emoji":    "📡",
		"file_url": "not-a-url",
		"width":    0,
		"height":   512,
	})
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-User-ID", uuid.New().String())
	req.Header.Set("X-User-Role", "admin")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUpdateStickerPack_AdminSuccess(t *testing.T) {
	packID := uuid.New()
	var updatedPack *model.StickerPack

	app := newStickerApp(&mockStickerStore{
		getPackFn: func(_ context.Context, id uuid.UUID) (*model.StickerPack, error) {
			if id != packID {
				t.Fatalf("unexpected pack id: %s", id)
			}
			return &model.StickerPack{
				ID:         packID,
				Title:      "Orbit Ops",
				ShortName:  "orbit_ops",
				IsOfficial: true,
			}, nil
		},
		getPackByShortNameFn: func(_ context.Context, shortName string) (*model.StickerPack, error) {
			if shortName != "orbit_ops_team" {
				t.Fatalf("unexpected short_name: %s", shortName)
			}
			return nil, nil
		},
		updatePackFn: func(_ context.Context, pack *model.StickerPack) error {
			if pack.ID != packID {
				t.Fatalf("unexpected pack id: %s", pack.ID)
			}
			if pack.Title != "Orbit Ops Team" {
				t.Fatalf("unexpected title: %s", pack.Title)
			}
			updatedPack = pack
			return nil
		},
	})

	req, err := makeJSONRequest(http.MethodPut, "/admin/sticker-packs/"+packID.String(), map[string]any{
		"name":          "Orbit Ops Team",
		"short_name":    "orbit_ops_team",
		"description":   "Updated pack",
		"thumbnail_url": "https://cdn.example.com/orbit-ops-team.svg",
	})
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-User-ID", uuid.New().String())
	req.Header.Set("X-User-Role", "admin")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if updatedPack == nil || !updatedPack.IsFeatured {
		t.Fatalf("expected featured pack to be updated")
	}
}

func TestUpdateStickerPack_InvalidBody(t *testing.T) {
	packID := uuid.New()

	app := newStickerApp(&mockStickerStore{
		getPackFn: func(_ context.Context, id uuid.UUID) (*model.StickerPack, error) {
			return &model.StickerPack{
				ID:         id,
				Title:      "Orbit Ops",
				ShortName:  "orbit_ops",
				IsOfficial: true,
			}, nil
		},
	})

	req, err := makeJSONRequest(http.MethodPut, "/admin/sticker-packs/"+packID.String(), map[string]any{
		"name":       "Orbit Ops Team",
		"short_name": "BAD SHORT NAME",
	})
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-User-ID", uuid.New().String())
	req.Header.Set("X-User-Role", "admin")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestDeleteStickerPack_AdminSuccess(t *testing.T) {
	packID := uuid.New()
	deleted := false

	app := newStickerApp(&mockStickerStore{
		getPackFn: func(_ context.Context, id uuid.UUID) (*model.StickerPack, error) {
			if id != packID {
				t.Fatalf("unexpected pack id: %s", id)
			}
			return &model.StickerPack{
				ID:        packID,
				Title:     "Orbit Ops",
				ShortName: "orbit_ops",
			}, nil
		},
		deletePackFn: func(_ context.Context, id uuid.UUID) error {
			if id != packID {
				t.Fatalf("unexpected pack id: %s", id)
			}
			deleted = true
			return nil
		},
	})

	req, err := http.NewRequest(http.MethodDelete, "/admin/sticker-packs/"+packID.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-User-ID", uuid.New().String())
	req.Header.Set("X-User-Role", "admin")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !deleted {
		t.Fatalf("expected pack to be deleted")
	}
}

func TestDeleteStickerPack_NonAdminForbidden(t *testing.T) {
	req, err := http.NewRequest(http.MethodDelete, "/admin/sticker-packs/"+uuid.New().String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-User-ID", uuid.New().String())
	req.Header.Set("X-User-Role", "member")

	resp, err := newStickerApp(&mockStickerStore{}).Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}
