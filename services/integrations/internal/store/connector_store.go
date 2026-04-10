package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/integrations/internal/model"
)

const connectorSelectColumns = `
	id, name, display_name, type, bot_id, config, is_active, created_by, created_at, updated_at
`

type ConnectorStore interface {
	Create(ctx context.Context, c *model.Connector) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.Connector, error)
	GetByName(ctx context.Context, name string) (*model.Connector, error)
	List(ctx context.Context, limit, offset int) ([]model.Connector, int, error)
	Update(ctx context.Context, c *model.Connector) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetSecretHash(ctx context.Context, id uuid.UUID) (string, error)
	SetSecretHash(ctx context.Context, id uuid.UUID, hash string) error
}

type connectorStore struct {
	pool *pgxpool.Pool
}

func NewConnectorStore(pool *pgxpool.Pool) ConnectorStore {
	return &connectorStore{pool: pool}
}

func (s *connectorStore) Create(ctx context.Context, c *model.Connector) error {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO integration_connectors (
			name, display_name, type, bot_id, config, secret_hash, is_active, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at
	`, c.Name, c.DisplayName, c.Type, c.BotID, c.Config, c.SecretHash, c.IsActive, c.CreatedBy).
		Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.ErrConnectorAlreadyExists
		}
		return fmt.Errorf("create connector: %w", err)
	}

	return nil
}

func (s *connectorStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Connector, error) {
	connector := &model.Connector{}
	err := s.pool.QueryRow(ctx, `
		SELECT `+connectorSelectColumns+`
		FROM integration_connectors
		WHERE id = $1
	`, id).Scan(
		&connector.ID,
		&connector.Name,
		&connector.DisplayName,
		&connector.Type,
		&connector.BotID,
		&connector.Config,
		&connector.IsActive,
		&connector.CreatedBy,
		&connector.CreatedAt,
		&connector.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get connector by id: %w", err)
	}

	return connector, nil
}

func (s *connectorStore) GetByName(ctx context.Context, name string) (*model.Connector, error) {
	connector := &model.Connector{}
	err := s.pool.QueryRow(ctx, `
		SELECT `+connectorSelectColumns+`
		FROM integration_connectors
		WHERE name = $1
	`, name).Scan(
		&connector.ID,
		&connector.Name,
		&connector.DisplayName,
		&connector.Type,
		&connector.BotID,
		&connector.Config,
		&connector.IsActive,
		&connector.CreatedBy,
		&connector.CreatedAt,
		&connector.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get connector by name: %w", err)
	}

	return connector, nil
}

func (s *connectorStore) List(ctx context.Context, limit, offset int) ([]model.Connector, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM integration_connectors`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count connectors: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT `+connectorSelectColumns+`
		FROM integration_connectors
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list connectors: %w", err)
	}
	defer rows.Close()

	connectors := make([]model.Connector, 0, limit)
	for rows.Next() {
		var connector model.Connector
		if err := rows.Scan(
			&connector.ID,
			&connector.Name,
			&connector.DisplayName,
			&connector.Type,
			&connector.BotID,
			&connector.Config,
			&connector.IsActive,
			&connector.CreatedBy,
			&connector.CreatedAt,
			&connector.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan connector: %w", err)
		}
		connectors = append(connectors, connector)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate connectors: %w", err)
	}

	return connectors, total, nil
}

func (s *connectorStore) Update(ctx context.Context, c *model.Connector) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE integration_connectors
		SET name = $1,
		    display_name = $2,
		    type = $3,
		    bot_id = $4,
		    config = $5,
		    is_active = $6,
		    updated_at = NOW()
		WHERE id = $7
	`, c.Name, c.DisplayName, c.Type, c.BotID, c.Config, c.IsActive, c.ID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.ErrConnectorAlreadyExists
		}
		return fmt.Errorf("update connector: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrConnectorNotFound
	}

	return nil
}

func (s *connectorStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM integration_connectors WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete connector: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrConnectorNotFound
	}

	return nil
}

func (s *connectorStore) GetSecretHash(ctx context.Context, id uuid.UUID) (string, error) {
	var hash *string
	err := s.pool.QueryRow(ctx, `
		SELECT secret_hash
		FROM integration_connectors
		WHERE id = $1
	`, id).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", model.ErrConnectorNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get connector secret hash: %w", err)
	}
	if hash == nil {
		return "", nil
	}

	return *hash, nil
}

func (s *connectorStore) SetSecretHash(ctx context.Context, id uuid.UUID, hash string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE integration_connectors
		SET secret_hash = $1, updated_at = NOW()
		WHERE id = $2
	`, hash, id)
	if err != nil {
		return fmt.Errorf("set connector secret hash: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrConnectorNotFound
	}

	return nil
}
