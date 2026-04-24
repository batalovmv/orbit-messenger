// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mst-corp/orbit/services/ai/internal/model"
)

// NotificationStore persists user feedback on notification classification
// and serves accuracy stats.
type NotificationStore interface {
	SaveFeedback(ctx context.Context, userID, messageID, classified, override string) error
	GetStats(ctx context.Context, userID string, days int) (*model.NotificationStatsResponse, error)
}

type notificationStore struct {
	pool *pgxpool.Pool
}

func NewNotificationStore(pool *pgxpool.Pool) NotificationStore {
	return &notificationStore{pool: pool}
}

func (s *notificationStore) SaveFeedback(ctx context.Context, userID, messageID, classified, override string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO notification_priority_feedback (user_id, message_id, classified_priority, user_override_priority, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (user_id, message_id) DO UPDATE
		SET user_override_priority = $4
	`, userID, messageID, classified, override)
	if err != nil {
		return fmt.Errorf("insert notification feedback: %w", err)
	}
	return nil
}

func (s *notificationStore) GetStats(ctx context.Context, userID string, days int) (*model.NotificationStatsResponse, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT classified_priority,
		       COUNT(*) AS total,
		       COUNT(*) FILTER (WHERE classified_priority != user_override_priority) AS overridden
		FROM notification_priority_feedback
		WHERE user_id = $1 AND created_at > NOW() - ($2 || ' days')::interval
		GROUP BY classified_priority
	`, userID, fmt.Sprintf("%d", days))
	if err != nil {
		return nil, fmt.Errorf("query notification stats: %w", err)
	}
	defer rows.Close()

	resp := &model.NotificationStatsResponse{
		PerPriority: make(map[string]model.PriorityStats),
	}

	for rows.Next() {
		var priority string
		var total, overridden int
		if err := rows.Scan(&priority, &total, &overridden); err != nil {
			return nil, fmt.Errorf("scan notification stats: %w", err)
		}
		resp.TotalClassified += total
		resp.TotalOverridden += overridden
		resp.PerPriority[priority] = model.PriorityStats{
			Classified: total,
			Overridden: overridden,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notification stats: %w", err)
	}

	if resp.TotalClassified > 0 {
		resp.MatchRate = 1.0 - float64(resp.TotalOverridden)/float64(resp.TotalClassified)
	}

	return resp, nil
}
