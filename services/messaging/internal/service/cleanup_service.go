package service

import (
	"context"
	"log/slog"
	"time"
)

type CleanupStore interface {
	DeleteExpired(ctx context.Context) (int64, error)
}

type CleanupService struct {
	store    CleanupStore
	interval time.Duration
}

func NewCleanupService(store CleanupStore, interval time.Duration) *CleanupService {
	return &CleanupService{store: store, interval: interval}
}

func (s *CleanupService) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	slog.Info("disappearing message cleanup started", "interval", s.interval)

	for {
		select {
		case <-ctx.Done():
			slog.Info("disappearing message cleanup stopped")
			return
		case <-ticker.C:
			count, err := s.store.DeleteExpired(ctx)
			if err != nil {
				slog.Error("cleanup expired messages failed", "error", err)
				continue
			}
			if count > 0 {
				slog.Info("cleaned up expired messages", "count", count)
			}
		}
	}
}
