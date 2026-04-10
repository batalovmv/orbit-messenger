package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/integrations/internal/model"
)

type DeliveryStore interface {
	Create(ctx context.Context, d *model.Delivery) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.Delivery, error)
	ListByConnector(ctx context.Context, connectorID uuid.UUID, limit, offset int) ([]model.Delivery, int, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string, lastError *string, nextRetryAt *time.Time, orbitMessageID *uuid.UUID) error
	GetPendingRetries(ctx context.Context, limit int) ([]model.Delivery, error)
	FindByCorrelation(ctx context.Context, connectorID uuid.UUID, correlationKey string) (*model.Delivery, error)
	FindByExternalID(ctx context.Context, connectorID uuid.UUID, externalEventID string) (*model.Delivery, error)
	MarkDeadLetter(ctx context.Context, id uuid.UUID, lastError string) error
}

type deliveryStore struct {
	pool *pgxpool.Pool
}

func NewDeliveryStore(pool *pgxpool.Pool) DeliveryStore {
	return &deliveryStore{pool: pool}
}

func (s *deliveryStore) Create(ctx context.Context, d *model.Delivery) error {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO integration_deliveries (
			connector_id, route_id, external_event_id, event_type, payload, status,
			orbit_message_id, correlation_key, attempt_count, max_attempts, last_error,
			next_retry_at, delivered_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, created_at
	`,
		d.ConnectorID, d.RouteID, d.ExternalEventID, d.EventType, d.Payload, d.Status,
		d.OrbitMessageID, d.CorrelationKey, d.AttemptCount, d.MaxAttempts, d.LastError,
		d.NextRetryAt, d.DeliveredAt,
	).Scan(&d.ID, &d.CreatedAt)
	if err != nil {
		return fmt.Errorf("create delivery: %w", err)
	}

	return nil
}

func (s *deliveryStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Delivery, error) {
	return s.getOne(ctx, `
		SELECT id, connector_id, route_id, external_event_id, event_type, payload, status,
		       orbit_message_id, correlation_key, attempt_count, max_attempts, last_error,
		       next_retry_at, delivered_at, created_at
		FROM integration_deliveries
		WHERE id = $1
	`, id)
}

func (s *deliveryStore) ListByConnector(ctx context.Context, connectorID uuid.UUID, limit, offset int) ([]model.Delivery, int, error) {
	return s.ListByConnectorFiltered(ctx, connectorID, nil, limit, offset)
}

func (s *deliveryStore) ListByConnectorFiltered(ctx context.Context, connectorID uuid.UUID, status *string, limit, offset int) ([]model.Delivery, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var statusArg any
	if status != nil && strings.TrimSpace(*status) != "" {
		trimmed := strings.TrimSpace(*status)
		statusArg = trimmed
	}

	var total int
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM integration_deliveries
		WHERE connector_id = $1
		  AND ($2::text IS NULL OR status = $2)
	`, connectorID, statusArg).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count deliveries: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, connector_id, route_id, external_event_id, event_type, payload, status,
		       orbit_message_id, correlation_key, attempt_count, max_attempts, last_error,
		       next_retry_at, delivered_at, created_at
		FROM integration_deliveries
		WHERE connector_id = $1
		  AND ($2::text IS NULL OR status = $2)
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
	`, connectorID, statusArg, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list deliveries: %w", err)
	}
	defer rows.Close()

	deliveries, err := scanDeliveries(rows)
	if err != nil {
		return nil, 0, err
	}

	return deliveries, total, nil
}

func (s *deliveryStore) UpdateStatus(ctx context.Context, id uuid.UUID, status string, lastError *string, nextRetryAt *time.Time, orbitMessageID *uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE integration_deliveries
		SET status = $1,
		    last_error = $2,
		    next_retry_at = $3,
		    orbit_message_id = COALESCE($4, orbit_message_id),
		    delivered_at = CASE WHEN $1 = 'delivered' THEN NOW() ELSE delivered_at END,
		    attempt_count = CASE
		        WHEN $1 = 'failed' THEN attempt_count + 1
		        WHEN $1 = 'pending' THEN 0
		        ELSE attempt_count
		    END
		WHERE id = $5
	`, status, lastError, nextRetryAt, orbitMessageID, id)
	if err != nil {
		return fmt.Errorf("update delivery status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrDeliveryNotFound
	}

	return nil
}

func (s *deliveryStore) GetPendingRetries(ctx context.Context, limit int) ([]model.Delivery, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	// Atomically claim rows by setting status='processing' in a single UPDATE RETURNING.
	// This prevents concurrent workers from picking up the same delivery.
	rows, err := s.pool.Query(ctx, `
		UPDATE integration_deliveries
		SET status = 'processing'
		WHERE id IN (
			SELECT id FROM integration_deliveries
			WHERE status IN ('pending', 'failed')
			  AND next_retry_at <= NOW()
			ORDER BY next_retry_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, connector_id, route_id, external_event_id, event_type, payload, 'processing'::text,
		          orbit_message_id, correlation_key, attempt_count, max_attempts, last_error,
		          next_retry_at, delivered_at, created_at
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("get pending delivery retries: %w", err)
	}
	defer rows.Close()

	return scanDeliveries(rows)
}

func (s *deliveryStore) FindByCorrelation(ctx context.Context, connectorID uuid.UUID, correlationKey string) (*model.Delivery, error) {
	return s.getOne(ctx, `
		SELECT id, connector_id, route_id, external_event_id, event_type, payload, status,
		       orbit_message_id, correlation_key, attempt_count, max_attempts, last_error,
		       next_retry_at, delivered_at, created_at
		FROM integration_deliveries
		WHERE connector_id = $1 AND correlation_key = $2
		ORDER BY created_at DESC
		LIMIT 1
	`, connectorID, correlationKey)
}

func (s *deliveryStore) FindByExternalID(ctx context.Context, connectorID uuid.UUID, externalEventID string) (*model.Delivery, error) {
	return s.getOne(ctx, `
		SELECT id, connector_id, route_id, external_event_id, event_type, payload, status,
		       orbit_message_id, correlation_key, attempt_count, max_attempts, last_error,
		       next_retry_at, delivered_at, created_at
		FROM integration_deliveries
		WHERE connector_id = $1 AND external_event_id = $2
		ORDER BY created_at DESC
		LIMIT 1
	`, connectorID, externalEventID)
}

func (s *deliveryStore) MarkDeadLetter(ctx context.Context, id uuid.UUID, lastError string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE integration_deliveries
		SET status = 'dead_letter',
		    last_error = $1,
		    next_retry_at = NULL
		WHERE id = $2
	`, lastError, id)
	if err != nil {
		return fmt.Errorf("mark delivery dead letter: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrDeliveryNotFound
	}

	return nil
}

func (s *deliveryStore) getOne(ctx context.Context, query string, args ...any) (*model.Delivery, error) {
	delivery := &model.Delivery{}
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&delivery.ID,
		&delivery.ConnectorID,
		&delivery.RouteID,
		&delivery.ExternalEventID,
		&delivery.EventType,
		&delivery.Payload,
		&delivery.Status,
		&delivery.OrbitMessageID,
		&delivery.CorrelationKey,
		&delivery.AttemptCount,
		&delivery.MaxAttempts,
		&delivery.LastError,
		&delivery.NextRetryAt,
		&delivery.DeliveredAt,
		&delivery.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get delivery: %w", err)
	}
	return delivery, nil
}

func scanDeliveries(rows pgx.Rows) ([]model.Delivery, error) {
	deliveries := make([]model.Delivery, 0)
	for rows.Next() {
		var delivery model.Delivery
		if err := rows.Scan(
			&delivery.ID,
			&delivery.ConnectorID,
			&delivery.RouteID,
			&delivery.ExternalEventID,
			&delivery.EventType,
			&delivery.Payload,
			&delivery.Status,
			&delivery.OrbitMessageID,
			&delivery.CorrelationKey,
			&delivery.AttemptCount,
			&delivery.MaxAttempts,
			&delivery.LastError,
			&delivery.NextRetryAt,
			&delivery.DeliveredAt,
			&delivery.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan delivery: %w", err)
		}
		deliveries = append(deliveries, delivery)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deliveries: %w", err)
	}

	return deliveries, nil
}
