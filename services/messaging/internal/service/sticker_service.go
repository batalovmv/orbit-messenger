package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

// StickerService handles business logic for sticker packs.
type StickerService struct {
	stickers store.StickerStore
	logger   *slog.Logger
}

// NewStickerService creates a new StickerService.
func NewStickerService(stickers store.StickerStore, logger *slog.Logger) *StickerService {
	return &StickerService{stickers: stickers, logger: logger}
}

// GetPack returns a sticker pack with its stickers.
func (s *StickerService) GetPack(ctx context.Context, packID, userID uuid.UUID) (*model.StickerPack, error) {
	pack, err := s.stickers.GetPack(ctx, packID)
	if err != nil {
		return nil, fmt.Errorf("get sticker pack: %w", err)
	}
	if pack == nil {
		return nil, apperror.NotFound("Sticker pack not found")
	}

	installed, err := s.stickers.IsInstalled(ctx, userID, packID)
	if err != nil {
		return nil, fmt.Errorf("check sticker pack install status: %w", err)
	}
	pack.IsInstalled = installed

	return pack, nil
}

// ListFeatured returns recommended sticker packs.
func (s *StickerService) ListFeatured(ctx context.Context, userID uuid.UUID, limit int) ([]model.StickerPack, error) {
	packs, err := s.stickers.ListFeatured(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list featured sticker packs: %w", err)
	}

	for i := range packs {
		installed, err := s.stickers.IsInstalled(ctx, userID, packs[i].ID)
		if err != nil {
			return nil, fmt.Errorf("check featured sticker pack install status: %w", err)
		}
		packs[i].IsInstalled = installed
	}

	return packs, nil
}

// Search searches sticker packs by query.
func (s *StickerService) Search(ctx context.Context, query string, limit int) ([]model.StickerPack, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, apperror.BadRequest("Search query is required")
	}

	packs, err := s.stickers.Search(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search sticker packs: %w", err)
	}
	return packs, nil
}

// ListInstalled returns the user's installed sticker packs.
func (s *StickerService) ListInstalled(ctx context.Context, userID uuid.UUID) ([]model.StickerPack, error) {
	packs, err := s.stickers.ListInstalled(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list installed sticker packs: %w", err)
	}

	for i := range packs {
		packs[i].IsInstalled = true
	}

	return packs, nil
}

// Install installs a sticker pack for a user.
func (s *StickerService) Install(ctx context.Context, userID, packID uuid.UUID) error {
	pack, err := s.stickers.GetPack(ctx, packID)
	if err != nil {
		return fmt.Errorf("get sticker pack: %w", err)
	}
	if pack == nil {
		return apperror.NotFound("Sticker pack not found")
	}

	installed, err := s.stickers.IsInstalled(ctx, userID, packID)
	if err != nil {
		return fmt.Errorf("check sticker pack install status: %w", err)
	}
	if installed {
		return apperror.Conflict("Sticker pack already installed")
	}

	if err := s.stickers.Install(ctx, userID, packID); err != nil {
		return fmt.Errorf("install sticker pack: %w", err)
	}
	return nil
}

// Uninstall removes a sticker pack from a user's collection.
func (s *StickerService) Uninstall(ctx context.Context, userID, packID uuid.UUID) error {
	installed, err := s.stickers.IsInstalled(ctx, userID, packID)
	if err != nil {
		return fmt.Errorf("check sticker pack install status: %w", err)
	}
	if !installed {
		return apperror.NotFound("Sticker pack not installed")
	}

	if err := s.stickers.Uninstall(ctx, userID, packID); err != nil {
		return fmt.Errorf("uninstall sticker pack: %w", err)
	}
	return nil
}

// ListRecent returns the user's recently used stickers.
func (s *StickerService) ListRecent(ctx context.Context, userID uuid.UUID, limit int) ([]model.Sticker, error) {
	stickers, err := s.stickers.ListRecent(ctx, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent stickers: %w", err)
	}
	return stickers, nil
}

// AddRecent adds a sticker to the user's recent list.
func (s *StickerService) AddRecent(ctx context.Context, userID, stickerID uuid.UUID) error {
	if err := s.stickers.AddRecent(ctx, userID, stickerID); err != nil {
		return fmt.Errorf("add recent sticker: %w", err)
	}
	return nil
}

func (s *StickerService) RemoveRecent(ctx context.Context, userID, stickerID uuid.UUID) error {
	if err := s.stickers.RemoveRecent(ctx, userID, stickerID); err != nil {
		return fmt.Errorf("remove recent sticker: %w", err)
	}
	return nil
}

func (s *StickerService) ClearRecent(ctx context.Context, userID uuid.UUID) error {
	if err := s.stickers.ClearRecent(ctx, userID); err != nil {
		return fmt.Errorf("clear recent stickers: %w", err)
	}
	return nil
}
