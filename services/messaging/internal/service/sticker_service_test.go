package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

func newTestStickerService(ss *mockStickerStore) *StickerService {
	return NewStickerService(ss, slog.Default())
}

func stickerAssertAppError(t *testing.T, err error, wantStatus int) {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError with status %d, got %T: %v", wantStatus, err, err)
	}
	if appErr.Status != wantStatus {
		t.Fatalf("expected status %d, got %d: %s", wantStatus, appErr.Status, appErr.Message)
	}
}

// --- GetPack ---

func TestGetPack_NotFound(t *testing.T) {
	ss := &mockStickerStore{
		getPackFn: func(ctx context.Context, packID uuid.UUID) (*model.StickerPack, error) {
			return nil, nil
		},
	}
	svc := newTestStickerService(ss)

	_, err := svc.GetPack(context.Background(), uuid.New(), uuid.New())
	stickerAssertAppError(t, err, 404)
}

func TestGetPack_Success_ChecksInstallStatus(t *testing.T) {
	packID := uuid.New()
	userID := uuid.New()

	ss := &mockStickerStore{
		getPackFn: func(ctx context.Context, pID uuid.UUID) (*model.StickerPack, error) {
			return &model.StickerPack{
				ID:        packID,
				Title:     "MST Memes",
				ShortName: "mst_memes",
				Stickers: []model.Sticker{
					{ID: uuid.New(), PackID: packID, FileURL: "https://r2.example.com/sticker.webp", FileType: "webp"},
				},
			}, nil
		},
		isInstalledFn: func(ctx context.Context, uID, pID uuid.UUID) (bool, error) {
			return true, nil
		},
	}
	svc := newTestStickerService(ss)

	pack, err := svc.GetPack(context.Background(), packID, userID)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if pack == nil {
		t.Fatal("expected pack, got nil")
	}
	if !pack.IsInstalled {
		t.Fatal("expected IsInstalled=true")
	}
	if len(pack.Stickers) != 1 {
		t.Fatalf("expected 1 sticker, got %d", len(pack.Stickers))
	}
}

// --- Install ---

func TestInstall_PackNotFound(t *testing.T) {
	ss := &mockStickerStore{
		getPackFn: func(ctx context.Context, packID uuid.UUID) (*model.StickerPack, error) {
			return nil, nil
		},
	}
	svc := newTestStickerService(ss)

	err := svc.Install(context.Background(), uuid.New(), uuid.New())
	stickerAssertAppError(t, err, 404)
}

func TestInstall_AlreadyInstalled(t *testing.T) {
	packID := uuid.New()
	userID := uuid.New()

	ss := &mockStickerStore{
		getPackFn: func(ctx context.Context, pID uuid.UUID) (*model.StickerPack, error) {
			return &model.StickerPack{ID: packID}, nil
		},
		isInstalledFn: func(ctx context.Context, uID, pID uuid.UUID) (bool, error) {
			return true, nil
		},
	}
	svc := newTestStickerService(ss)

	err := svc.Install(context.Background(), userID, packID)
	stickerAssertAppError(t, err, 409)
}

func TestInstall_Success(t *testing.T) {
	packID := uuid.New()
	userID := uuid.New()

	ss := &mockStickerStore{
		getPackFn: func(ctx context.Context, pID uuid.UUID) (*model.StickerPack, error) {
			return &model.StickerPack{ID: packID}, nil
		},
		isInstalledFn: func(ctx context.Context, uID, pID uuid.UUID) (bool, error) {
			return false, nil
		},
	}
	svc := newTestStickerService(ss)

	err := svc.Install(context.Background(), userID, packID)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

// --- Uninstall ---

func TestUninstall_NotInstalled(t *testing.T) {
	packID := uuid.New()
	userID := uuid.New()

	ss := &mockStickerStore{
		isInstalledFn: func(ctx context.Context, uID, pID uuid.UUID) (bool, error) {
			return false, nil
		},
	}
	svc := newTestStickerService(ss)

	err := svc.Uninstall(context.Background(), userID, packID)
	stickerAssertAppError(t, err, 404)
}

// --- ListFeatured ---

func TestListFeatured_ReturnsResults(t *testing.T) {
	ss := &mockStickerStore{
		listFeaturedFn: func(ctx context.Context, limit int) ([]model.StickerPack, error) {
			return []model.StickerPack{
				{ID: uuid.New(), Title: "Official", IsOfficial: true},
				{ID: uuid.New(), Title: "Community"},
			}, nil
		},
	}
	svc := newTestStickerService(ss)

	packs, err := svc.ListFeatured(context.Background(), uuid.New(), 50)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(packs) != 2 {
		t.Fatalf("expected 2 packs, got %d", len(packs))
	}
}

// --- Search ---

func TestSearch_EmptyQuery(t *testing.T) {
	svc := newTestStickerService(&mockStickerStore{})

	_, err := svc.Search(context.Background(), "", 50)
	stickerAssertAppError(t, err, 400)
}

func TestSearch_Success(t *testing.T) {
	ss := &mockStickerStore{
		searchFn: func(ctx context.Context, query string, limit int) ([]model.StickerPack, error) {
			return []model.StickerPack{{ID: uuid.New(), Title: "Found pack"}}, nil
		},
	}
	svc := newTestStickerService(ss)

	packs, err := svc.Search(context.Background(), "meme", 50)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(packs) != 1 {
		t.Fatalf("expected 1 pack, got %d", len(packs))
	}
}

func TestGetByIDs_Success(t *testing.T) {
	stickerID := uuid.New()
	ss := &mockStickerStore{
		getByIDsFn: func(ctx context.Context, stickerIDs []uuid.UUID) ([]model.Sticker, error) {
			if len(stickerIDs) != 1 || stickerIDs[0] != stickerID {
				t.Fatalf("unexpected sticker ids: %#v", stickerIDs)
			}

			return []model.Sticker{{
				ID:       stickerID,
				PackID:   uuid.New(),
				FileURL:  "https://cdn.example.com/stickers/custom.webp",
				FileType: "webp",
				Position: 0,
			}}, nil
		},
	}
	svc := newTestStickerService(ss)

	stickers, err := svc.GetByIDs(context.Background(), []uuid.UUID{stickerID})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(stickers) != 1 || stickers[0].ID != stickerID {
		t.Fatalf("unexpected stickers: %#v", stickers)
	}
}
