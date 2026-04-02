package handler

import (
	"log/slog"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

func newStickerApp(ss *mockStickerStore) *fiber.App {
	app := fiber.New()
	svc := service.NewStickerService(ss, slog.Default())
	h := NewStickerHandler(svc, slog.Default())
	h.Register(app)
	return app
}

// --- ListFeatured ---

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

// --- Search ---

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

// --- GetPack ---

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

// --- Install ---

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

// --- Uninstall ---

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

// --- ListInstalled ---

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

// --- ListRecent ---

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
