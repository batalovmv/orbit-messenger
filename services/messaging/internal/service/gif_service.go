package service

import (
	"context"
	"log/slog"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

const (
	defaultGIFSearchLimit = 20
	maxGIFSearchLimit     = 50
	defaultSavedGIFLimit  = 50
	maxSavedGIFLimit      = 200
)

// TenorClient is an interface for the Tenor GIF API.
type TenorClient interface {
	Search(ctx context.Context, query string, limit int, pos string) ([]model.TenorGIF, string, error)
	Trending(ctx context.Context, limit int, pos string) ([]model.TenorGIF, string, error)
}

// GIFService handles business logic for GIF search and saved GIFs.
type GIFService struct {
	gifs   store.GIFStore
	tenor  TenorClient
	logger *slog.Logger
}

// NewGIFService creates a new GIFService.
func NewGIFService(gifs store.GIFStore, tenor TenorClient, logger *slog.Logger) *GIFService {
	if logger == nil {
		logger = slog.Default()
	}
	return &GIFService{gifs: gifs, tenor: tenor, logger: logger}
}

// SaveGIFInput is a convenience struct for creating SavedGIF from handler input.
type SaveGIFInput struct {
	UserID     uuid.UUID
	TenorID    string
	URL        string
	PreviewURL *string
	Width      *int
	Height     *int
}

// ToModel converts SaveGIFInput to a model.SavedGIF.
func (i *SaveGIFInput) ToModel() *model.SavedGIF {
	return &model.SavedGIF{
		UserID:     i.UserID,
		TenorID:    i.TenorID,
		URL:        i.URL,
		PreviewURL: i.PreviewURL,
		Width:      i.Width,
		Height:     i.Height,
	}
}

// Search searches GIFs via the Tenor API.
func (s *GIFService) Search(ctx context.Context, query string, limit int, pos string) ([]model.TenorGIF, string, error) {
	if s.tenor == nil {
		return nil, "", apperror.Internal("Tenor client is not configured")
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return nil, "", apperror.BadRequest("Query is required")
	}

	return s.tenor.Search(ctx, query, normalizeGIFSearchLimit(limit), strings.TrimSpace(pos))
}

// Trending returns trending GIFs from Tenor.
func (s *GIFService) Trending(ctx context.Context, limit int, pos string) ([]model.TenorGIF, string, error) {
	if s.tenor == nil {
		return nil, "", apperror.Internal("Tenor client is not configured")
	}

	return s.tenor.Trending(ctx, normalizeGIFSearchLimit(limit), strings.TrimSpace(pos))
}

// ListSaved returns the user's saved GIFs.
func (s *GIFService) ListSaved(ctx context.Context, userID uuid.UUID, limit int) ([]model.SavedGIF, error) {
	if s.gifs == nil {
		return nil, apperror.Internal("GIF store is not configured")
	}
	if userID == uuid.Nil {
		return nil, apperror.BadRequest("User ID is required")
	}

	return s.gifs.ListSaved(ctx, userID, normalizeSavedGIFLimit(limit))
}

// Save saves a GIF for a user.
func (s *GIFService) Save(ctx context.Context, gif *model.SavedGIF) error {
	if s.gifs == nil {
		return apperror.Internal("GIF store is not configured")
	}
	if gif == nil {
		return apperror.BadRequest("GIF payload is required")
	}
	if gif.UserID == uuid.Nil {
		return apperror.BadRequest("User ID is required")
	}

	gif.TenorID = strings.TrimSpace(gif.TenorID)
	gif.URL = strings.TrimSpace(gif.URL)

	if gif.TenorID == "" {
		return apperror.BadRequest("tenor_id is required")
	}
	if gif.URL == "" {
		return apperror.BadRequest("url is required")
	}
	if err := validateGIFURL(gif.URL); err != nil {
		return err
	}

	if gif.PreviewURL != nil {
		trimmed := strings.TrimSpace(*gif.PreviewURL)
		if trimmed == "" {
			gif.PreviewURL = nil
		} else {
			if err := validateGIFURL(trimmed); err != nil {
				return err
			}
			gif.PreviewURL = &trimmed
		}
	}

	if gif.Width != nil && *gif.Width <= 0 {
		return apperror.BadRequest("width must be positive")
	}
	if gif.Height != nil && *gif.Height <= 0 {
		return apperror.BadRequest("height must be positive")
	}

	return s.gifs.Save(ctx, gif)
}

// Remove removes a saved GIF.
func (s *GIFService) Remove(ctx context.Context, userID, gifID uuid.UUID) error {
	if s.gifs == nil {
		return apperror.Internal("GIF store is not configured")
	}
	if userID == uuid.Nil {
		return apperror.BadRequest("User ID is required")
	}
	if gifID == uuid.Nil {
		return apperror.BadRequest("GIF ID is required")
	}

	return s.gifs.Remove(ctx, userID, gifID)
}

func normalizeGIFSearchLimit(limit int) int {
	if limit <= 0 || limit > maxGIFSearchLimit {
		return defaultGIFSearchLimit
	}
	return limit
}

func normalizeSavedGIFLimit(limit int) int {
	if limit <= 0 || limit > maxSavedGIFLimit {
		return defaultSavedGIFLimit
	}
	return limit
}

func validateGIFURL(rawURL string) error {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return apperror.BadRequest("url must be a valid absolute URL")
	}

	switch parsed.Scheme {
	case "http", "https":
		return nil
	default:
		return apperror.BadRequest("url must use http or https")
	}
}
