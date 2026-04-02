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

func newTestGIFService(gs *mockGIFStore, tc *mockTenorClient) *GIFService {
	return NewGIFService(gs, tc, slog.Default())
}

func gifAssertAppError(t *testing.T, err error, wantStatus int) {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError with status %d, got %T: %v", wantStatus, err, err)
	}
	if appErr.Status != wantStatus {
		t.Fatalf("expected status %d, got %d: %s", wantStatus, appErr.Status, appErr.Message)
	}
}

// --- Search ---

func TestGIFServiceSearch_EmptyQuery(t *testing.T) {
	svc := newTestGIFService(&mockGIFStore{}, &mockTenorClient{})

	_, _, err := svc.Search(context.Background(), "", 20, "")
	gifAssertAppError(t, err, 400)
}

func TestGIFServiceSearch_ProxiesToTenor(t *testing.T) {
	tc := &mockTenorClient{
		searchFn: func(ctx context.Context, query string, limit int, pos string) ([]model.TenorGIF, string, error) {
			return []model.TenorGIF{
				{TenorID: "123", URL: "https://tenor.com/gif.mp4", Width: 480, Height: 360},
			}, "next_pos", nil
		},
	}
	svc := newTestGIFService(&mockGIFStore{}, tc)

	gifs, nextPos, err := svc.Search(context.Background(), "celebration", 20, "")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(gifs) != 1 {
		t.Fatalf("expected 1 gif, got %d", len(gifs))
	}
	if nextPos != "next_pos" {
		t.Fatalf("expected next_pos, got %s", nextPos)
	}
}

// --- Trending ---

func TestGIFServiceTrending_ProxiesToTenor(t *testing.T) {
	tc := &mockTenorClient{
		trendingFn: func(ctx context.Context, limit int, pos string) ([]model.TenorGIF, string, error) {
			return []model.TenorGIF{
				{TenorID: "456", URL: "https://tenor.com/trend.mp4"},
			}, "", nil
		},
	}
	svc := newTestGIFService(&mockGIFStore{}, tc)

	gifs, _, err := svc.Trending(context.Background(), 20, "")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(gifs) != 1 {
		t.Fatalf("expected 1 gif, got %d", len(gifs))
	}
}

// --- Save ---

func TestGIFServiceSave_MissingTenorID(t *testing.T) {
	svc := newTestGIFService(&mockGIFStore{}, &mockTenorClient{})

	err := svc.Save(context.Background(), &model.SavedGIF{
		UserID: uuid.New(),
		URL:    "https://example.com/gif.mp4",
	})
	gifAssertAppError(t, err, 400)
}

func TestGIFServiceSave_MissingURL(t *testing.T) {
	svc := newTestGIFService(&mockGIFStore{}, &mockTenorClient{})

	err := svc.Save(context.Background(), &model.SavedGIF{
		UserID:  uuid.New(),
		TenorID: "123",
	})
	gifAssertAppError(t, err, 400)
}

func TestGIFServiceSave_Success(t *testing.T) {
	svc := newTestGIFService(&mockGIFStore{}, &mockTenorClient{})

	err := svc.Save(context.Background(), &model.SavedGIF{
		UserID:  uuid.New(),
		TenorID: "123",
		URL:     "https://tenor.com/gif.mp4",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

// --- Remove ---

func TestGIFServiceRemove_Success(t *testing.T) {
	svc := newTestGIFService(&mockGIFStore{}, &mockTenorClient{})

	err := svc.Remove(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}
