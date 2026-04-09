package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/media/internal/model"
	"github.com/mst-corp/orbit/services/media/internal/store"
)

type quotaStore struct {
	userStorageBytes int64
}

func (q *quotaStore) Create(ctx context.Context, m *model.Media) error { return nil }
func (q *quotaStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Media, error) {
	return nil, nil
}
func (q *quotaStore) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]*model.Media, error) {
	return nil, nil
}
func (q *quotaStore) Delete(ctx context.Context, id uuid.UUID) error { return nil }
func (q *quotaStore) DeleteByUploader(ctx context.Context, id, uploaderID uuid.UUID) (string, *string, *string, error) {
	return "", nil, nil, nil
}
func (q *quotaStore) UpdateProcessingStatus(ctx context.Context, id uuid.UUID, status string) error {
	return nil
}
func (q *quotaStore) UpdateProcessingResult(ctx context.Context, id uuid.UUID, thumbnailKey, mediumKey *string, width, height *int, duration *float64, waveform []byte) error {
	return nil
}
func (q *quotaStore) GetByMessageIDs(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]*store.MessageMediaRow, error) {
	return nil, nil
}
func (q *quotaStore) LinkToMessage(ctx context.Context, messageID, mediaID uuid.UUID, position int, isSpoiler bool) error {
	return nil
}
func (q *quotaStore) CleanupOrphaned(ctx context.Context, maxAgeHours int) ([]string, error) {
	return nil, nil
}
func (q *quotaStore) GetUserStorageBytes(ctx context.Context, userID uuid.UUID) (int64, error) {
	return q.userStorageBytes, nil
}
func (q *quotaStore) CanAccess(ctx context.Context, mediaID, userID uuid.UUID) (bool, error) {
	return true, nil
}

func TestEnsureUserStorageAvailable_DisabledByDefault(t *testing.T) {
	svc := NewMediaService(&quotaStore{userStorageBytes: 10 * 1024 * 1024}, nil, nil, nil).WithMaxUserStorageBytes(0)
	if err := svc.ensureUserStorageAvailable(context.Background(), uuid.New(), 50*1024*1024); err != nil {
		t.Fatalf("expected unlimited storage when quota disabled, got %v", err)
	}
}

func TestUpload_OverQuotaRejected(t *testing.T) {
	svc := NewMediaService(&quotaStore{userStorageBytes: 0}, nil, nil, nil).WithMaxUserStorageBytes(1024 * 1024)

	_, err := svc.Upload(context.Background(), uuid.New(), make([]byte, 2*1024*1024), "big.jpg", "image/jpeg", "photo", false)
	appErr, ok := err.(*apperror.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T (%v)", err, err)
	}
	if appErr.Status != 429 {
		t.Fatalf("expected 429, got %d", appErr.Status)
	}
}

func TestInitChunkedUpload_OverQuotaRejected(t *testing.T) {
	svc := NewMediaService(&quotaStore{userStorageBytes: 512 * 1024}, nil, nil, nil).WithMaxUserStorageBytes(1024 * 1024)

	_, err := svc.InitChunkedUpload(context.Background(), uuid.New(), "video.mp4", "video/mp4", "video", 1024*1024)
	appErr, ok := err.(*apperror.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T (%v)", err, err)
	}
	if appErr.Status != 429 {
		t.Fatalf("expected 429, got %d", appErr.Status)
	}
}
