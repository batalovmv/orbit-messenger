package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

// GIFStore manages saved GIFs for users.
type GIFStore interface {
	// ListSaved returns the user's saved GIFs, newest first.
	ListSaved(ctx context.Context, userID uuid.UUID, limit int) ([]model.SavedGIF, error)
	// Save saves a GIF for a user. Returns conflict error if already saved.
	Save(ctx context.Context, gif *model.SavedGIF) error
	// Remove removes a saved GIF by ID.
	Remove(ctx context.Context, userID, gifID uuid.UUID) error
	// RemoveByTenorID removes a saved GIF by Tenor ID.
	RemoveByTenorID(ctx context.Context, userID uuid.UUID, tenorID string) error
}

type gifStore struct {
	pool *pgxpool.Pool
}

func NewGIFStore(pool *pgxpool.Pool) GIFStore {
	return &gifStore{pool: pool}
}

func (s *gifStore) ListSaved(ctx context.Context, userID uuid.UUID, limit int) ([]model.SavedGIF, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, tenor_id, url, preview_url, width, height, created_at
		 FROM saved_gifs
		 WHERE user_id = $1
		 ORDER BY created_at DESC, id DESC
		 LIMIT $2`,
		userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list saved GIFs: %w", err)
	}
	defer rows.Close()

	gifs := make([]model.SavedGIF, 0)
	for rows.Next() {
		var gif model.SavedGIF
		if err := rows.Scan(
			&gif.ID,
			&gif.UserID,
			&gif.TenorID,
			&gif.URL,
			&gif.PreviewURL,
			&gif.Width,
			&gif.Height,
			&gif.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan saved GIF: %w", err)
		}
		gifs = append(gifs, gif)
	}

	return gifs, rows.Err()
}

func (s *gifStore) Save(ctx context.Context, gif *model.SavedGIF) error {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO saved_gifs (user_id, tenor_id, url, preview_url, width, height)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, created_at`,
		gif.UserID, gif.TenorID, gif.URL, gif.PreviewURL, gif.Width, gif.Height,
	).Scan(&gif.ID, &gif.CreatedAt)
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return apperror.Conflict("GIF already saved")
	}

	return fmt.Errorf("save GIF: %w", err)
}

func (s *gifStore) Remove(ctx context.Context, userID, gifID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM saved_gifs WHERE user_id = $1 AND id = $2`,
		userID, gifID,
	)
	if err != nil {
		return fmt.Errorf("remove GIF: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NotFound("Saved GIF not found")
	}
	return nil
}

func (s *gifStore) RemoveByTenorID(ctx context.Context, userID uuid.UUID, tenorID string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM saved_gifs WHERE user_id = $1 AND tenor_id = $2`,
		userID, tenorID,
	)
	if err != nil {
		return fmt.Errorf("remove GIF by Tenor ID: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NotFound("Saved GIF not found")
	}
	return nil
}
