// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mst-corp/orbit/services/bots/internal/model"
)

// AuditStore persists bot admin action audit records.
type AuditStore struct {
	db *pgxpool.Pool
}

// NewAuditStore creates a new AuditStore backed by the given pool.
func NewAuditStore(db *pgxpool.Pool) *AuditStore {
	return &AuditStore{db: db}
}

// Log inserts a new audit log entry. Non-fatal — caller should log errors but not fail the request.
func (s *AuditStore) Log(ctx context.Context, entry model.AuditLogEntry) error {
	details := entry.Details
	if details == nil {
		details = json.RawMessage("{}")
	}
	_, err := s.db.Exec(ctx,
		`INSERT INTO bot_audit_log (actor_id, bot_id, action, details, source_ip, user_agent)
		 VALUES ($1, $2, $3, $4, $5::inet, $6)`,
		entry.ActorID, entry.BotID, entry.Action, []byte(details), entry.SourceIP, entry.UserAgent,
	)
	return err
}

// ListByBot returns audit entries for a specific bot, newest first.
func (s *AuditStore) ListByBot(ctx context.Context, botID uuid.UUID, limit int) ([]model.AuditLogEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(ctx,
		`SELECT id, actor_id, bot_id, action, details, source_ip::text, user_agent, created_at
		 FROM bot_audit_log WHERE bot_id = $1 ORDER BY created_at DESC LIMIT $2`,
		botID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAuditRows(rows)
}

// ListByActor returns audit entries for a specific actor, newest first.
func (s *AuditStore) ListByActor(ctx context.Context, actorID uuid.UUID, limit int) ([]model.AuditLogEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(ctx,
		`SELECT id, actor_id, bot_id, action, details, source_ip::text, user_agent, created_at
		 FROM bot_audit_log WHERE actor_id = $1 ORDER BY created_at DESC LIMIT $2`,
		actorID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAuditRows(rows)
}

func scanAuditRows(rows pgx.Rows) ([]model.AuditLogEntry, error) {
	var entries []model.AuditLogEntry
	for rows.Next() {
		var e model.AuditLogEntry
		var details []byte
		if err := rows.Scan(&e.ID, &e.ActorID, &e.BotID, &e.Action, &details, &e.SourceIP, &e.UserAgent, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Details = json.RawMessage(details)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
