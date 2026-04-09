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

// CallStore defines the interface for call persistence.
type CallStore interface {
	Create(ctx context.Context, call *model.Call) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.Call, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string, startedAt, endedAt *time.Time, durationSeconds *int) error
	GetActiveForChat(ctx context.Context, chatID uuid.UUID) (*model.Call, error)
	ListByUser(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]model.Call, string, bool, error)
	// Delete removes a call row. Used for rolling back a Create if participant setup fails.
	Delete(ctx context.Context, id uuid.UUID) error
	// IsUserInChat checks if a user is a member of a chat (shared DB with messaging service).
	IsUserInChat(ctx context.Context, chatID, userID uuid.UUID) (bool, error)
	// ExpireRinging marks all ringing calls older than threshold as missed and
	// returns their rows so the caller can publish call_ended events.
	ExpireRinging(ctx context.Context, threshold time.Duration) ([]model.Call, error)
}

type callStore struct {
	pool *pgxpool.Pool
}

// NewCallStore creates a new CallStore backed by PostgreSQL.
func NewCallStore(pool *pgxpool.Pool) CallStore {
	return &callStore{pool: pool}
}

const callSelectColumns = `c.id, c.type, c.mode, c.chat_id, c.initiator_id, c.status, c.started_at, c.ended_at, c.duration_seconds, c.created_at, c.updated_at`

type callScanner interface {
	Scan(dest ...any) error
}

func scanCall(s callScanner, c *model.Call) error {
	return s.Scan(
		&c.ID, &c.Type, &c.Mode, &c.ChatID, &c.InitiatorID, &c.Status,
		&c.StartedAt, &c.EndedAt, &c.DurationSeconds, &c.CreatedAt, &c.UpdatedAt,
	)
}

func (s *callStore) Create(ctx context.Context, call *model.Call) error {
	query := `INSERT INTO calls (id, type, mode, chat_id, initiator_id, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at`
	if call.ID == uuid.Nil {
		call.ID = uuid.New()
	}
	return s.pool.QueryRow(ctx, query,
		call.ID, call.Type, call.Mode, call.ChatID, call.InitiatorID, call.Status,
	).Scan(&call.CreatedAt, &call.UpdatedAt)
}

func (s *callStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Call, error) {
	query := `SELECT ` + callSelectColumns + ` FROM calls c WHERE c.id = $1`
	var c model.Call
	if err := scanCall(s.pool.QueryRow(ctx, query, id), &c); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get call by id: %w", err)
	}
	return &c, nil
}

func (s *callStore) UpdateStatus(ctx context.Context, id uuid.UUID, status string, startedAt, endedAt *time.Time, durationSeconds *int) error {
	query := `UPDATE calls SET status = $2, started_at = COALESCE($3, started_at), ended_at = COALESCE($4, ended_at),
		duration_seconds = COALESCE($5, duration_seconds), updated_at = NOW()
		WHERE id = $1`
	ct, err := s.pool.Exec(ctx, query, id, status, startedAt, endedAt, durationSeconds)
	if err != nil {
		return fmt.Errorf("update call status: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("update call status: call not found")
	}
	return nil
}

func (s *callStore) GetActiveForChat(ctx context.Context, chatID uuid.UUID) (*model.Call, error) {
	query := `SELECT ` + callSelectColumns + ` FROM calls c WHERE c.chat_id = $1 AND c.status IN ('ringing', 'active') LIMIT 1`
	var c model.Call
	if err := scanCall(s.pool.QueryRow(ctx, query, chatID), &c); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get active call for chat: %w", err)
	}
	return &c, nil
}

func (s *callStore) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM calls WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete call: %w", err)
	}
	return nil
}

func (s *callStore) IsUserInChat(ctx context.Context, chatID, userID uuid.UUID) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM chat_members WHERE chat_id = $1 AND user_id = $2)`,
		chatID, userID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("is user in chat: %w", err)
	}
	return exists, nil
}

func (s *callStore) ExpireRinging(ctx context.Context, threshold time.Duration) ([]model.Call, error) {
	query := `UPDATE calls SET status = 'missed', ended_at = NOW(), updated_at = NOW()
		WHERE status = 'ringing' AND created_at < NOW() - ($1 || ' seconds')::interval
		RETURNING id, type, mode, chat_id, initiator_id, status, started_at, ended_at, duration_seconds, created_at, updated_at`
	rows, err := s.pool.Query(ctx, query, int(threshold.Seconds()))
	if err != nil {
		return nil, fmt.Errorf("expire ringing: %w", err)
	}
	defer rows.Close()

	var expired []model.Call
	for rows.Next() {
		var c model.Call
		if err := scanCall(rows, &c); err != nil {
			return nil, fmt.Errorf("scan expired call: %w", err)
		}
		expired = append(expired, c)
	}
	return expired, rows.Err()
}

func (s *callStore) ListByUser(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]model.Call, string, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var args []any
	query := `SELECT ` + callSelectColumns + ` FROM calls c
		INNER JOIN call_participants cp ON cp.call_id = c.id
		WHERE cp.user_id = $1`
	args = append(args, userID)

	if cursor != "" {
		cursorID, err := uuid.Parse(cursor)
		if err != nil {
			return nil, "", false, fmt.Errorf("invalid cursor: %w", err)
		}
		query += ` AND c.created_at < (SELECT created_at FROM calls WHERE id = $2)`
		args = append(args, cursorID)
	}

	query += ` ORDER BY c.created_at DESC LIMIT $` + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", false, fmt.Errorf("list calls by user: %w", err)
	}
	defer rows.Close()

	var calls []model.Call
	for rows.Next() {
		var c model.Call
		if err := scanCall(rows, &c); err != nil {
			return nil, "", false, fmt.Errorf("scan call: %w", err)
		}
		calls = append(calls, c)
	}

	hasMore := len(calls) > limit
	if hasMore {
		calls = calls[:limit]
	}

	var nextCursor string
	if hasMore && len(calls) > 0 {
		nextCursor = calls[len(calls)-1].ID.String()
	}

	return calls, nextCursor, hasMore, nil
}
