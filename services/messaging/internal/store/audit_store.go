// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

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
	// Stream invokes emit once per matching row in id-DESC order, up to
	// hardCap rows. The Cursor and Limit fields of filter are ignored —
	// streaming is single-shot, scoped by the other filter fields. Returns
	// the number of rows emitted (≤ hardCap) and any iteration error.
	// Aborts cleanly if ctx is cancelled or emit returns an error.
	Stream(ctx context.Context, filter AuditFilter, hardCap int, emit func(model.AuditEntry) error) (int, error)
}

// AuditFilter defines query parameters for listing audit log entries.
//
// Q is a free-text search applied with ILIKE across action, target_type,
// target_id, the joined actor display name, and the JSONB-as-text rendering
// of details. At 150 users on-prem this is fast enough without a tsvector
// or pg_trgm index; revisit if the table grows past ~100k rows.
type AuditFilter struct {
	ActorID    *uuid.UUID
	Action     *string
	TargetType *string
	TargetID   *string
	Since      *time.Time
	Until      *time.Time
	Q          string
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
	if q := strings.TrimSpace(filter.Q); q != "" {
		// Free-text search across denormalised audit columns + joined actor
		// display name + the JSONB-as-text rendering of details. ILIKE is
		// sufficient at 150-user scale (see audit_store doc); the parameter
		// is bound, not interpolated, so this is injection-safe.
		pattern := "%" + escapeLike(q) + "%"
		conditions = append(conditions, fmt.Sprintf(
			"(a.action ILIKE $%d OR a.target_type ILIKE $%d OR a.target_id ILIKE $%d "+
				"OR COALESCE(u.display_name,'') ILIKE $%d OR a.details::text ILIKE $%d "+
				"OR host(a.ip_address) ILIKE $%d)",
			argIdx, argIdx, argIdx, argIdx, argIdx, argIdx))
		args = append(args, pattern)
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

// escapeLike escapes the three characters that have meaning inside an SQL
// LIKE/ILIKE pattern (\, %, _). The result is then wrapped with %…% by the
// caller so user input cannot smuggle wildcards.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// Stream walks audit_log entries matching the filter and invokes emit per
// row, capped at hardCap. Cursor/Limit on the filter are ignored. Used by
// the CSV export endpoint where we don't want to buffer the whole table in
// memory or paginate.
//
// A SET LOCAL statement_timeout is applied within a single-tx connection to
// bound DB load — long ILIKE+jsonb scans can otherwise pin a backend forever
// if the operator vendors a pathological filter.
func (s *auditStore) Stream(ctx context.Context, filter AuditFilter, hardCap int, emit func(model.AuditEntry) error) (int, error) {
	if hardCap <= 0 {
		return 0, fmt.Errorf("audit stream: hardCap must be positive")
	}
	if emit == nil {
		return 0, fmt.Errorf("audit stream: emit must not be nil")
	}

	// Reuse the same WHERE-builder shape as List, with Cursor/Limit elided.
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
	if q := strings.TrimSpace(filter.Q); q != "" {
		pattern := "%" + escapeLike(q) + "%"
		conditions = append(conditions, fmt.Sprintf(
			"(a.action ILIKE $%d OR a.target_type ILIKE $%d OR a.target_id ILIKE $%d "+
				"OR COALESCE(u.display_name,'') ILIKE $%d OR a.details::text ILIKE $%d "+
				"OR host(a.ip_address) ILIKE $%d)",
			argIdx, argIdx, argIdx, argIdx, argIdx, argIdx))
		args = append(args, pattern)
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
	args = append(args, hardCap)

	// Acquire a dedicated connection so SET LOCAL stays scoped to this stream.
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquire audit stream conn: %w", err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin audit stream tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read-only tx, rollback is best-effort

	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '60s'"); err != nil {
		return 0, fmt.Errorf("set audit stream timeout: %w", err)
	}

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("query audit stream: %w", err)
	}
	defer rows.Close()

	emitted := 0
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return emitted, err
		}
		var e model.AuditEntry
		if err := rows.Scan(
			&e.ID, &e.ActorID, &e.Action, &e.TargetType, &e.TargetID,
			&e.Details, &e.IPAddress, &e.UserAgent, &e.CreatedAt,
			&e.ActorName,
		); err != nil {
			return emitted, fmt.Errorf("scan audit stream row: %w", err)
		}
		if err := emit(e); err != nil {
			return emitted, err
		}
		emitted++
	}
	if err := rows.Err(); err != nil {
		return emitted, err
	}
	return emitted, nil
}
