package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/auth/internal/model"
)

// ErrNotFound is returned when a record is not found.
var ErrNotFound = errors.New("not found")

type SessionStore interface {
	Create(ctx context.Context, s *model.Session) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.Session, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]model.Session, error)
	DeleteByID(ctx context.Context, id uuid.UUID, userID uuid.UUID) error
	GetByTokenHash(ctx context.Context, hash string) (*model.Session, error)
	DeleteByTokenHash(ctx context.Context, hash string) error
	// DeleteAndReturnByTokenHash atomically deletes a session and returns it.
	// Returns (nil, nil) if no session matched.
	DeleteAndReturnByTokenHash(ctx context.Context, hash string) (*model.Session, error)
	DeleteAllByUser(ctx context.Context, userID uuid.UUID) error
}

type sessionStore struct {
	pool *pgxpool.Pool
}

func NewSessionStore(pool *pgxpool.Pool) SessionStore {
	return &sessionStore{pool: pool}
}

func (s *sessionStore) Create(ctx context.Context, sess *model.Session) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO sessions (user_id, token_hash, ip_address, user_agent, expires_at)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, created_at`,
		sess.UserID, sess.TokenHash, sess.IPAddress, sess.UserAgent, sess.ExpiresAt,
	).Scan(&sess.ID, &sess.CreatedAt)
}

func (s *sessionStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Session, error) {
	sess := &model.Session{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, device_id, token_hash, ip_address::TEXT, user_agent, expires_at, created_at
		 FROM sessions WHERE id = $1`, id,
	).Scan(&sess.ID, &sess.UserID, &sess.DeviceID, &sess.TokenHash,
		&sess.IPAddress, &sess.UserAgent, &sess.ExpiresAt, &sess.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return sess, err
}

func (s *sessionStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]model.Session, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, device_id, token_hash, ip_address::TEXT, user_agent, expires_at, created_at
		 FROM sessions WHERE user_id = $1 ORDER BY created_at DESC LIMIT 100`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []model.Session
	for rows.Next() {
		var sess model.Session
		if err := rows.Scan(&sess.ID, &sess.UserID, &sess.DeviceID, &sess.TokenHash,
			&sess.IPAddress, &sess.UserAgent, &sess.ExpiresAt, &sess.CreatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *sessionStore) DeleteByID(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM sessions WHERE id = $1 AND user_id = $2`, id, userID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *sessionStore) GetByTokenHash(ctx context.Context, hash string) (*model.Session, error) {
	sess := &model.Session{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, device_id, token_hash, ip_address::TEXT, user_agent, expires_at, created_at
		 FROM sessions WHERE token_hash = $1`, hash,
	).Scan(&sess.ID, &sess.UserID, &sess.DeviceID, &sess.TokenHash,
		&sess.IPAddress, &sess.UserAgent, &sess.ExpiresAt, &sess.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return sess, err
}

func (s *sessionStore) DeleteByTokenHash(ctx context.Context, hash string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, hash)
	return err
}

func (s *sessionStore) DeleteAndReturnByTokenHash(ctx context.Context, hash string) (*model.Session, error) {
	sess := &model.Session{}
	err := s.pool.QueryRow(ctx,
		`DELETE FROM sessions WHERE token_hash = $1
		 RETURNING id, user_id, device_id, token_hash, ip_address::TEXT, user_agent, expires_at, created_at`, hash,
	).Scan(&sess.ID, &sess.UserID, &sess.DeviceID, &sess.TokenHash,
		&sess.IPAddress, &sess.UserAgent, &sess.ExpiresAt, &sess.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return sess, err
}

func (s *sessionStore) DeleteAllByUser(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	return err
}
