package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

// AuditStore provides append-only access to the audit_log table.
// No Update or Delete methods — the table is protected by DB triggers.
type AuditStore interface {
	Log(ctx context.Context, entry *model.AuditEntry) error
	List(ctx context.Context, filter AuditFilter) ([]model.AuditEntry, string, bool, error)
}

// AuditFilter defines query parameters for listing audit log entries.
type AuditFilter struct {
	ActorID    *uuid.UUID
	Action     *string
	TargetType *string
	TargetID   *string
	Since      *time.Time
	Until      *time.Time
	Cursor     string
	Limit      int
}

type auditStore struct {
	pool *pgxpool.Pool
}

func NewAuditStore(pool *pgxpool.Pool) AuditStore {
	return &auditStore{pool: pool}
}

// Log writes an audit entry. Returns an error if the write fails —
// callers MUST treat this as fail-closed (reject the privileged action).
func (s *auditStore) Log(ctx context.Context, entry *model.AuditEntry) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO audit_log (actor_id, action, target_type, target_id, details, ip_address, user_agent)
		 VALUES ($1, $2, $3, $4, $5, $6::inet, $7)
		 RETURNING id, created_at`,
		entry.ActorID, entry.Action, entry.TargetType, entry.TargetID,
		entry.Details, entry.IPAddress, entry.UserAgent,
	).Scan(&entry.ID, &entry.CreatedAt)
}

// List returns audit log entries matching the filter, with cursor-based pagination.
func (s *auditStore) List(ctx context.Context, filter AuditFilter) ([]model.AuditEntry, string, bool, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var conditions []string
	var args []interface{}
	argIdx := 1

	if filter.ActorID != nil {
		conditions = append(conditions, fmt.Sprintf("a.actor_id = $%d", argIdx))
		args = append(args, *filter.ActorID)
		argIdx++
	}
	if filter.Action != nil {
		conditions = append(conditions, fmt.Sprintf("a.action = $%d", argIdx))
		args = append(args, *filter.Action)
		argIdx++
	}
	if filter.TargetType != nil {
		conditions = append(conditions, fmt.Sprintf("a.target_type = $%d", argIdx))
		args = append(args, *filter.TargetType)
		argIdx++
	}
	if filter.TargetID != nil {
		conditions = append(conditions, fmt.Sprintf("a.target_id = $%d", argIdx))
		args = append(args, *filter.TargetID)
		argIdx++
	}
	if filter.Since != nil {
		conditions = append(conditions, fmt.Sprintf("a.created_at >= $%d", argIdx))
		args = append(args, *filter.Since)
		argIdx++
	}
	if filter.Until != nil {
		conditions = append(conditions, fmt.Sprintf("a.created_at <= $%d", argIdx))
		args = append(args, *filter.Until)
		argIdx++
	}
	if filter.Cursor != "" {
		// Cursor is the string representation of the last seen audit_log.id (BIGSERIAL int64)
		conditions = append(conditions, fmt.Sprintf("a.id < $%d", argIdx))
		args = append(args, filter.Cursor)
		argIdx++
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	query := fmt.Sprintf(
		`SELECT a.id, a.actor_id, a.action, a.target_type, a.target_id,
		        a.details, a.ip_address::text, a.user_agent, a.created_at,
		        COALESCE(u.display_name, '') AS actor_name
		 FROM audit_log a
		 LEFT JOIN users u ON u.id = a.actor_id
		 %s
		 ORDER BY a.id DESC
		 LIMIT $%d`, where, argIdx,
	)
	args = append(args, limit+1) // fetch one extra to detect has_more

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", false, fmt.Errorf("list audit log: %w", err)
	}
	defer rows.Close()

	var entries []model.AuditEntry
	for rows.Next() {
		var e model.AuditEntry
		if err := rows.Scan(
			&e.ID, &e.ActorID, &e.Action, &e.TargetType, &e.TargetID,
			&e.Details, &e.IPAddress, &e.UserAgent, &e.CreatedAt,
			&e.ActorName,
		); err != nil {
			return nil, "", false, fmt.Errorf("scan audit entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, "", false, err
	}

	hasMore := len(entries) > limit
	if hasMore {
		entries = entries[:limit]
	}

	cursor := ""
	if len(entries) > 0 {
		cursor = fmt.Sprintf("%d", entries[len(entries)-1].ID)
	}

	return entries, cursor, hasMore, nil
}
