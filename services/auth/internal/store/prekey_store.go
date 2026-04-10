package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/auth/internal/model"
)

type PreKeyStore interface {
	UploadBatch(ctx context.Context, userID, deviceID uuid.UUID, keys []model.OneTimePreKey) (int, error)
	ConsumeOne(ctx context.Context, userID uuid.UUID) (*model.OneTimePreKey, error)
	CountRemaining(ctx context.Context, userID uuid.UUID) (int, error)
	DeleteByDevice(ctx context.Context, userID, deviceID uuid.UUID) error
}

type preKeyStore struct {
	pool *pgxpool.Pool
}

func NewPreKeyStore(pool *pgxpool.Pool) PreKeyStore {
	return &preKeyStore{pool: pool}
}

func (s *preKeyStore) UploadBatch(ctx context.Context, userID, deviceID uuid.UUID, keys []model.OneTimePreKey) (int, error) {
	if len(keys) > 100 {
		return 0, fmt.Errorf("upload prekey batch: batch size exceeds 100")
	}
	if len(keys) == 0 {
		return 0, nil
	}

	var batch pgx.Batch
	for _, key := range keys {
		batch.Queue(
			`INSERT INTO one_time_prekeys (user_id, device_id, key_id, public_key)
			 VALUES ($1, $2, $3, $4)`,
			userID, deviceID, key.KeyID, key.PublicKey,
		)
	}

	results := s.pool.SendBatch(ctx, &batch)
	defer results.Close()

	inserted := 0
	for range keys {
		if _, err := results.Exec(); err != nil {
			return inserted, fmt.Errorf("upload prekey batch: %w", err)
		}
		inserted++
	}
	return inserted, nil
}

func (s *preKeyStore) ConsumeOne(ctx context.Context, userID uuid.UUID) (*model.OneTimePreKey, error) {
	key := &model.OneTimePreKey{}
	err := s.pool.QueryRow(ctx,
		`UPDATE one_time_prekeys
		 SET used = true
		 WHERE id = (
			 SELECT id
			 FROM one_time_prekeys
			 WHERE user_id = $1 AND used = false
			 ORDER BY id ASC
			 LIMIT 1
			 FOR UPDATE SKIP LOCKED
		 )
		 RETURNING id, user_id, device_id, key_id, public_key`,
		userID,
	).Scan(
		&key.ID,
		&key.UserID,
		&key.DeviceID,
		&key.KeyID,
		&key.PublicKey,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("consume prekey: %w", err)
	}
	return key, nil
}

func (s *preKeyStore) CountRemaining(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM one_time_prekeys WHERE user_id = $1 AND used = false`,
		userID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count remaining prekeys: %w", err)
	}
	return count, nil
}

func (s *preKeyStore) DeleteByDevice(ctx context.Context, userID, deviceID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM one_time_prekeys WHERE user_id = $1 AND device_id = $2`,
		userID, deviceID,
	)
	if err != nil {
		return fmt.Errorf("delete prekeys by device: %w", err)
	}
	return nil
}
