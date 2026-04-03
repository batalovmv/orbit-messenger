package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/validator"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

var stickerPackShortNamePattern = regexp.MustCompile(`^[a-z0-9_]{3,64}$`)

// StickerService handles business logic for sticker packs.
type StickerService struct {
	stickers      store.StickerStore
	logger        *slog.Logger
	telegram      TelegramStickerClient
	mediaUploader StickerMediaUploader
}

// StickerServiceOption configures optional integrations.
type StickerServiceOption func(*StickerService)

// WithStickerImportClients wires Telegram import dependencies into StickerService.
func WithStickerImportClients(telegram TelegramStickerClient, mediaUploader StickerMediaUploader) StickerServiceOption {
	return func(service *StickerService) {
		service.telegram = telegram
		service.mediaUploader = mediaUploader
	}
}

// NewStickerService creates a new StickerService.
func NewStickerService(stickers store.StickerStore, logger *slog.Logger, options ...StickerServiceOption) *StickerService {
	if logger == nil {
		logger = slog.Default()
	}

	service := &StickerService{stickers: stickers, logger: logger}
	for _, option := range options {
		option(service)
	}

	return service
}

// GetByIDs returns stickers for document-based custom emoji resolution.
func (s *StickerService) GetByIDs(ctx context.Context, stickerIDs []uuid.UUID) ([]model.Sticker, error) {
	if len(stickerIDs) == 0 {
		return []model.Sticker{}, nil
	}

	stickers, err := s.stickers.GetByIDs(ctx, stickerIDs)
	if err != nil {
		return nil, fmt.Errorf("get stickers by ids: %w", err)
	}

	return stickers, nil
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

func (s *StickerService) CreateAdminPack(ctx context.Context, pack *model.StickerPack) (*model.StickerPack, error) {
	pack.Title = strings.TrimSpace(pack.Title)
	pack.ShortName = normalizeStickerPackShortName(pack.ShortName)

	if err := validatePackInput(pack); err != nil {
		return nil, err
	}

	existing, err := s.stickers.GetPackByShortName(ctx, pack.ShortName)
	if err != nil {
		return nil, fmt.Errorf("get sticker pack by short name: %w", err)
	}
	if existing != nil {
		return nil, apperror.Conflict("Sticker pack short_name already exists")
	}

	pack.IsOfficial = true
	pack.IsFeatured = true
	if err := s.stickers.CreatePack(ctx, pack, nil); err != nil {
		return nil, s.mapWriteError("create sticker pack", err)
	}

	s.logger.Info("sticker pack created", "pack_id", pack.ID, "short_name", pack.ShortName)
	return pack, nil
}

func (s *StickerService) AddStickerToPack(ctx context.Context, packID uuid.UUID, sticker *model.Sticker, isAnimated bool) (*model.Sticker, error) {
	pack, err := s.stickers.GetPack(ctx, packID)
	if err != nil {
		return nil, fmt.Errorf("get sticker pack: %w", err)
	}
	if pack == nil {
		return nil, apperror.NotFound("Sticker pack not found")
	}

	sticker.PackID = packID
	sticker.FileType = inferStickerFileType(sticker.FileURL, isAnimated)
	if err := validateStickerInput(sticker); err != nil {
		return nil, err
	}

	if err := s.stickers.AddSticker(ctx, packID, sticker); err != nil {
		return nil, s.mapWriteError("add sticker to pack", err)
	}

	s.logger.Info("sticker added to pack", "pack_id", packID, "sticker_id", sticker.ID, "position", sticker.Position)
	return sticker, nil
}

func (s *StickerService) UpdateAdminPack(ctx context.Context, pack *model.StickerPack) (*model.StickerPack, error) {
	existing, err := s.stickers.GetPack(ctx, pack.ID)
	if err != nil {
		return nil, fmt.Errorf("get sticker pack: %w", err)
	}
	if existing == nil {
		return nil, apperror.NotFound("Sticker pack not found")
	}

	pack.Title = strings.TrimSpace(pack.Title)
	pack.ShortName = normalizeStickerPackShortName(pack.ShortName)
	pack.AuthorID = existing.AuthorID
	pack.IsOfficial = existing.IsOfficial
	pack.IsAnimated = existing.IsAnimated
	pack.IsFeatured = true
	pack.StickerCount = existing.StickerCount

	if err := validatePackInput(pack); err != nil {
		return nil, err
	}

	if existing.ShortName != pack.ShortName {
		conflict, err := s.stickers.GetPackByShortName(ctx, pack.ShortName)
		if err != nil {
			return nil, fmt.Errorf("get sticker pack by short name: %w", err)
		}
		if conflict != nil && conflict.ID != pack.ID {
			return nil, apperror.Conflict("Sticker pack short_name already exists")
		}
	}

	if err := s.stickers.UpdatePack(ctx, pack); err != nil {
		return nil, s.mapWriteError("update sticker pack", err)
	}

	updated, err := s.stickers.GetPack(ctx, pack.ID)
	if err != nil {
		return nil, fmt.Errorf("reload sticker pack: %w", err)
	}
	if updated == nil {
		return nil, apperror.NotFound("Sticker pack not found")
	}

	s.logger.Info("sticker pack updated", "pack_id", updated.ID, "short_name", updated.ShortName)
	return updated, nil
}

func (s *StickerService) DeleteAdminPack(ctx context.Context, packID uuid.UUID) error {
	pack, err := s.stickers.GetPack(ctx, packID)
	if err != nil {
		return fmt.Errorf("get sticker pack: %w", err)
	}
	if pack == nil {
		return apperror.NotFound("Sticker pack not found")
	}

	if err := s.stickers.DeletePack(ctx, packID); err != nil {
		return s.mapWriteError("delete sticker pack", err)
	}

	s.logger.Info("sticker pack deleted", "pack_id", packID, "short_name", pack.ShortName)
	return nil
}

func (s *StickerService) mapWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return apperror.Conflict("Sticker pack already exists")
	}

	return fmt.Errorf("%s: %w", operation, err)
}

func validatePackInput(pack *model.StickerPack) error {
	if err := validator.RequireString(pack.Title, "name", 1, 100); err != nil {
		return err
	}
	if err := validator.RequireString(pack.ShortName, "short_name", 3, 64); err != nil {
		return err
	}
	if !stickerPackShortNamePattern.MatchString(pack.ShortName) {
		return apperror.BadRequest("short_name must contain only lowercase letters, numbers, and underscores")
	}
	if pack.Description != nil && len(strings.TrimSpace(*pack.Description)) > 500 {
		return apperror.BadRequest("description is too long")
	}
	if pack.ThumbnailURL != nil && !isAllowedStickerURL(*pack.ThumbnailURL) {
		return apperror.BadRequest("thumbnail_url must be a valid http(s) or data URL")
	}

	return nil
}

func validateStickerInput(sticker *model.Sticker) error {
	if sticker.Emoji == nil || strings.TrimSpace(*sticker.Emoji) == "" {
		return apperror.BadRequest("emoji is required")
	}
	if err := validator.RequireString(strings.TrimSpace(*sticker.Emoji), "emoji", 1, 16); err != nil {
		return err
	}
	if err := validator.RequireString(strings.TrimSpace(sticker.FileURL), "file_url", 1, 2048); err != nil {
		return err
	}
	if !isAllowedStickerURL(sticker.FileURL) {
		return apperror.BadRequest("file_url must be a valid http(s) or data URL")
	}
	switch sticker.FileType {
	case "webp", "tgs", "webm", "svg":
	default:
		return apperror.BadRequest("unsupported sticker file type")
	}
	if sticker.Width == nil || sticker.Height == nil || *sticker.Width <= 0 || *sticker.Height <= 0 {
		return apperror.BadRequest("width and height must be positive")
	}

	return nil
}

func normalizeStickerPackShortName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "-", "_")
	return strings.ToLower(value)
}

func inferStickerFileType(fileURL string, isAnimated bool) string {
	lowerURL := strings.ToLower(strings.TrimSpace(fileURL))
	if hintedType := extractStickerFormatHint(lowerURL); hintedType != "" {
		return hintedType
	}

	if strings.HasPrefix(lowerURL, "data:") {
		switch {
		case strings.HasPrefix(lowerURL, "data:image/svg+xml"):
			return "svg"
		case strings.HasPrefix(lowerURL, "data:video/webm"):
			return "webm"
		case strings.HasPrefix(lowerURL, "data:application/x-tgsticker"):
			return "tgs"
		case strings.HasPrefix(lowerURL, "data:image/webp"):
			return "webp"
		}
	}

	if parsed, err := url.Parse(lowerURL); err == nil {
		switch ext := path.Ext(parsed.Path); ext {
		case ".svg":
			return "svg"
		case ".webm":
			return "webm"
		case ".tgs":
			return "tgs"
		case ".webp":
			return "webp"
		}
	}

	if isAnimated {
		return "tgs"
	}

	return "webp"
}

func extractStickerFormatHint(raw string) string {
	if raw == "" {
		return ""
	}

	matches := regexp.MustCompile(`[?#&]orbit-format=(tgs|webm|webp|svg|png|jpe?g)\b`).FindStringSubmatch(raw)
	if len(matches) < 2 {
		return ""
	}

	switch matches[1] {
	case "tgs":
		return "tgs"
	case "webm":
		return "webm"
	default:
		return "webp"
	}
}

func isAllowedStickerURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}

	if strings.HasPrefix(strings.ToLower(raw), "data:") {
		return strings.HasPrefix(strings.ToLower(raw), "data:image/svg+xml") ||
			strings.HasPrefix(strings.ToLower(raw), "data:image/webp") ||
			strings.HasPrefix(strings.ToLower(raw), "data:video/webm") ||
			strings.HasPrefix(strings.ToLower(raw), "data:application/x-tgsticker")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}

	if parsed.Scheme == "" && parsed.Host == "" {
		return strings.HasPrefix(parsed.Path, "/media/")
	}

	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
