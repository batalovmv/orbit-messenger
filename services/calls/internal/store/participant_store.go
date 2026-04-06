package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mst-corp/orbit/services/calls/internal/model"
)

// ParticipantStore defines the interface for call participant persistence.
type ParticipantStore interface {
	Add(ctx context.Context, p *model.CallParticipant) error
	Remove(ctx context.Context, callID, userID uuid.UUID) error
	UpdateMute(ctx context.Context, callID, userID uuid.UUID, isMuted bool) error
	UpdateScreenShare(ctx context.Context, callID, userID uuid.UUID, isSharing bool) error
	ListByCall(ctx context.Context, callID uuid.UUID) ([]model.CallParticipant, error)
	IsParticipant(ctx context.Context, callID, userID uuid.UUID) (bool, error)
}

type participantStore struct {
	pool *pgxpool.Pool
}

// NewParticipantStore creates a new ParticipantStore backed by PostgreSQL.
func NewParticipantStore(pool *pgxpool.Pool) ParticipantStore {
	return &participantStore{pool: pool}
}

func (s *participantStore) Add(ctx context.Context, p *model.CallParticipant) error {
	query := `INSERT INTO call_participants (call_id, user_id, joined_at) VALUES ($1, $2, NOW())
		ON CONFLICT (call_id, user_id) DO UPDATE SET left_at = NULL, joined_at = NOW()`
	_, err := s.pool.Exec(ctx, query, p.CallID, p.UserID)
	if err != nil {
		return fmt.Errorf("add participant: %w", err)
	}
	return nil
}

func (s *participantStore) Remove(ctx context.Context, callID, userID uuid.UUID) error {
	query := `UPDATE call_participants SET left_at = $3 WHERE call_id = $1 AND user_id = $2 AND left_at IS NULL`
	now := time.Now()
	ct, err := s.pool.Exec(ctx, query, callID, userID, now)
	if err != nil {
		return fmt.Errorf("remove participant: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("remove participant: not found or already left")
	}
	return nil
}

func (s *participantStore) UpdateMute(ctx context.Context, callID, userID uuid.UUID, isMuted bool) error {
	query := `UPDATE call_participants SET is_muted = $3 WHERE call_id = $1 AND user_id = $2 AND left_at IS NULL`
	_, err := s.pool.Exec(ctx, query, callID, userID, isMuted)
	if err != nil {
		return fmt.Errorf("update mute: %w", err)
	}
	return nil
}

func (s *participantStore) UpdateScreenShare(ctx context.Context, callID, userID uuid.UUID, isSharing bool) error {
	query := `UPDATE call_participants SET is_screen_sharing = $3 WHERE call_id = $1 AND user_id = $2 AND left_at IS NULL`
	_, err := s.pool.Exec(ctx, query, callID, userID, isSharing)
	if err != nil {
		return fmt.Errorf("update screen share: %w", err)
	}
	return nil
}

func (s *participantStore) ListByCall(ctx context.Context, callID uuid.UUID) ([]model.CallParticipant, error) {
	query := `SELECT cp.call_id, cp.user_id, cp.joined_at, cp.left_at, cp.is_muted, cp.is_camera_off, cp.is_screen_sharing,
		u.display_name, u.avatar_url
		FROM call_participants cp
		LEFT JOIN users u ON u.id = cp.user_id
		WHERE cp.call_id = $1 AND cp.left_at IS NULL
		ORDER BY cp.joined_at`

	rows, err := s.pool.Query(ctx, query, callID)
	if err != nil {
		return nil, fmt.Errorf("list participants: %w", err)
	}
	defer rows.Close()

	var participants []model.CallParticipant
	for rows.Next() {
		var p model.CallParticipant
		if err := rows.Scan(
			&p.CallID, &p.UserID, &p.JoinedAt, &p.LeftAt, &p.IsMuted, &p.IsCameraOff, &p.IsScreenSharing,
			&p.DisplayName, &p.AvatarURL,
		); err != nil {
			return nil, fmt.Errorf("scan participant: %w", err)
		}
		participants = append(participants, p)
	}
	return participants, nil
}

func (s *participantStore) IsParticipant(ctx context.Context, callID, userID uuid.UUID) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM call_participants WHERE call_id = $1 AND user_id = $2 AND left_at IS NULL)`
	var exists bool
	if err := s.pool.QueryRow(ctx, query, callID, userID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("is participant: %w", err)
	}
	return exists, nil
}
