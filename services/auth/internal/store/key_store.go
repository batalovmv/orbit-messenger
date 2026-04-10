package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/auth/internal/model"
)

type KeyStore interface {
	Upsert(ctx context.Context, keys *model.UserDeviceKeys) error
	GetByUserAndDevice(ctx context.Context, userID, deviceID uuid.UUID) (*model.UserDeviceKeys, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]model.UserDeviceKeys, error)
	DeleteByDevice(ctx context.Context, userID, deviceID uuid.UUID) error
	GetIdentityKey(ctx context.Context, userID uuid.UUID) ([]byte, error)
}

type keyStore struct {
	pool *pgxpool.Pool
}

func NewKeyStore(pool *pgxpool.Pool) KeyStore {
	return &keyStore{pool: pool}
}

func (s *keyStore) Upsert(ctx context.Context, keys *model.UserDeviceKeys) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_keys (
			user_id, device_id, identity_key, signed_prekey,
			signed_prekey_signature, signed_prekey_id
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, device_id) DO UPDATE SET
			signed_prekey = $4,
			signed_prekey_signature = $5,
			signed_prekey_id = $6,
			updated_at = NOW()`,
		keys.UserID,
		keys.DeviceID,
		keys.IdentityKey,
		keys.SignedPreKey,
		keys.SignedPreKeySignature,
		keys.SignedPreKeyID,
	)
	if err != nil {
		return fmt.Errorf("upsert user keys: %w", err)
	}
	return nil
}

func (s *keyStore) GetByUserAndDevice(ctx context.Context, userID, deviceID uuid.UUID) (*model.UserDeviceKeys, error) {
	keys := &model.UserDeviceKeys{}
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, device_id, identity_key, signed_prekey,
		        signed_prekey_signature, signed_prekey_id, created_at, updated_at
		 FROM user_keys
		 WHERE user_id = $1 AND device_id = $2`,
		userID, deviceID,
	).Scan(
		&keys.UserID,
		&keys.DeviceID,
		&keys.IdentityKey,
		&keys.SignedPreKey,
		&keys.SignedPreKeySignature,
		&keys.SignedPreKeyID,
		&keys.CreatedAt,
		&keys.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user keys: %w", err)
	}
	return keys, nil
}

func (s *keyStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]model.UserDeviceKeys, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_id, device_id, identity_key, signed_prekey,
		        signed_prekey_signature, signed_prekey_id, created_at, updated_at
		 FROM user_keys
		 WHERE user_id = $1
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list user keys: %w", err)
	}
	defer rows.Close()

	var keys []model.UserDeviceKeys
	for rows.Next() {
		var item model.UserDeviceKeys
		if err := rows.Scan(
			&item.UserID,
			&item.DeviceID,
			&item.IdentityKey,
			&item.SignedPreKey,
			&item.SignedPreKeySignature,
			&item.SignedPreKeyID,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan user keys: %w", err)
		}
		keys = append(keys, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user keys: %w", err)
	}
	return keys, nil
}

func (s *keyStore) DeleteByDevice(ctx context.Context, userID, deviceID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM user_keys WHERE user_id = $1 AND device_id = $2`,
		userID, deviceID,
	)
	if err != nil {
		return fmt.Errorf("delete user keys by device: %w", err)
	}
	return nil
}

func (s *keyStore) GetIdentityKey(ctx context.Context, userID uuid.UUID) ([]byte, error) {
	var identityKey []byte
	err := s.pool.QueryRow(ctx,
		`SELECT identity_key
		 FROM user_keys
		 WHERE user_id = $1
		 LIMIT 1`,
		userID,
	).Scan(&identityKey)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get identity key: %w", err)
	}
	return identityKey, nil
}
