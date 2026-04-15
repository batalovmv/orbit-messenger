package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mst-corp/orbit/services/ai/internal/model"
)

// UsageStore records per-request AI usage and serves /ai/usage queries.
type UsageStore interface {
	Record(ctx context.Context, rec model.UsageRecord) error
	GetUserStats(ctx context.Context, userID uuid.UUID, since time.Time) (*model.UsageStats, error)
}

type usageStore struct {
	pool *pgxpool.Pool
}

func NewUsageStore(pool *pgxpool.Pool) UsageStore {
	return &usageStore{pool: pool}
}

// Record is called asynchronously from the service layer after each
// successful AI call — we never block the user-facing response on it.
func (s *usageStore) Record(ctx context.Context, rec model.UsageRecord) error {
	uid, err := uuid.Parse(rec.UserID)
	if err != nil {
		return fmt.Errorf("parse user id: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO ai_usage (user_id, endpoint, model, input_tokens, output_tokens, cost_cents, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, uid, rec.Endpoint, rec.Model, rec.InputTokens, rec.OutputTokens, rec.CostCents, rec.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert ai usage: %w", err)
	}
	return nil
}

// GetUserStats returns aggregated counts and a small sample of the most
// recent individual records for the given user, limited to events after
// `since`.
func (s *usageStore) GetUserStats(ctx context.Context, userID uuid.UUID, since time.Time) (*model.UsageStats, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT endpoint, model, input_tokens, output_tokens, cost_cents, created_at
		FROM ai_usage
		WHERE user_id = $1 AND created_at >= $2
		ORDER BY created_at DESC
		LIMIT 1000
	`, userID, since)
	if err != nil {
		return nil, fmt.Errorf("query ai usage: %w", err)
	}
	defer rows.Close()

	stats := &model.UsageStats{
		ByEndpoint:    make(map[string]int),
		Cost:          make(map[string]int),
		PeriodStart:   since,
		RecentSamples: make([]model.UsageSample, 0, 20),
	}

	count := 0
	for rows.Next() {
		var sample model.UsageSample
		if err := rows.Scan(
			&sample.Endpoint,
			&sample.Model,
			&sample.InputTokens,
			&sample.OutputTokens,
			&sample.CostCents,
			&sample.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan ai usage: %w", err)
		}

		stats.TotalRequests++
		stats.ByEndpoint[sample.Endpoint]++
		stats.InputTokens += sample.InputTokens
		stats.OutputTokens += sample.OutputTokens
		stats.Cost[sample.Endpoint] += sample.CostCents

		if count < 20 {
			stats.RecentSamples = append(stats.RecentSamples, sample)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ai usage: %w", err)
	}

	return stats, nil
}
