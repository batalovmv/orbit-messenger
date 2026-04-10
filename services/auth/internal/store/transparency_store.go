package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/auth/internal/model"
)

type TransparencyStore interface {
	Append(ctx context.Context, entry *model.KeyTransparencyEntry) error
	ListByUser(ctx context.Context, userID uuid.UUID, limit int) ([]model.KeyTransparencyEntry, error)
}

type transparencyStore struct {
	pool *pgxpool.Pool
}

func NewTransparencyStore(pool *pgxpool.Pool) TransparencyStore {
	return &transparencyStore{pool: pool}
}

func (s *transparencyStore) Append(ctx context.Context, entry *model.KeyTransparencyEntry) error {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO key_transparency_log (user_id, device_id, event_type, public_key_hash)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at`,
		entry.UserID, entry.DeviceID, entry.EventType, entry.PublicKeyHash,
	).Scan(&entry.ID, &entry.CreatedAt)
	if err != nil {
		return fmt.Errorf("append transparency log: %w", err)
	}
	return nil
}

func (s *transparencyStore) ListByUser(ctx context.Context, userID uuid.UUID, limit int) ([]model.KeyTransparencyEntry, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, device_id, event_type, public_key_hash, created_at
		 FROM key_transparency_log
		 WHERE user_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`,
		userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list transparency log: %w", err)
	}
	defer rows.Close()

	var entries []model.KeyTransparencyEntry
	for rows.Next() {
		var entry model.KeyTransparencyEntry
		if err := rows.Scan(
			&entry.ID,
			&entry.UserID,
			&entry.DeviceID,
			&entry.EventType,
			&entry.PublicKeyHash,
			&entry.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan transparency log: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate transparency log: %w", err)
	}
	return entries, nil
}
