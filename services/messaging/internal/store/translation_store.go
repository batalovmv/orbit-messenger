package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

type TranslationStore interface {
	Get(ctx context.Context, messageID uuid.UUID, lang string) (*model.MessageTranslation, error)
	GetBatch(ctx context.Context, messageIDs []uuid.UUID, lang string) (map[uuid.UUID]*model.MessageTranslation, error)
	Upsert(ctx context.Context, t *model.MessageTranslation) error
}

type translationStore struct {
	pool *pgxpool.Pool
}

func NewTranslationStore(pool *pgxpool.Pool) TranslationStore {
	return &translationStore{pool: pool}
}

func (s *translationStore) Get(ctx context.Context, messageID uuid.UUID, lang string) (*model.MessageTranslation, error) {
	t := &model.MessageTranslation{}
	err := s.pool.QueryRow(ctx,
		`SELECT message_id, lang, translated_text, created_at
		 FROM message_translations
		 WHERE message_id = $1 AND lang = $2`,
		messageID, lang,
	).Scan(&t.MessageID, &t.Lang, &t.Text, &t.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, apperror.NotFound("translation not found")
	}
	if err != nil {
		return nil, fmt.Errorf("translationStore.Get: %w", err)
	}
	return t, nil
}

func (s *translationStore) GetBatch(ctx context.Context, messageIDs []uuid.UUID, lang string) (map[uuid.UUID]*model.MessageTranslation, error) {
	if len(messageIDs) == 0 {
		return make(map[uuid.UUID]*model.MessageTranslation), nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT message_id, lang, translated_text, created_at
		 FROM message_translations
		 WHERE message_id = ANY($1) AND lang = $2`,
		messageIDs, lang,
	)
	if err != nil {
		return nil, fmt.Errorf("translationStore.GetBatch: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID]*model.MessageTranslation, len(messageIDs))
	for rows.Next() {
		t := &model.MessageTranslation{}
		if err := rows.Scan(&t.MessageID, &t.Lang, &t.Text, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("translationStore.GetBatch scan: %w", err)
		}
		result[t.MessageID] = t
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("translationStore.GetBatch rows: %w", err)
	}
	return result, nil
}

func (s *translationStore) Upsert(ctx context.Context, t *model.MessageTranslation) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO message_translations (message_id, lang, translated_text, created_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (message_id, lang) DO UPDATE SET
		   translated_text = EXCLUDED.translated_text,
		   created_at      = NOW()`,
		t.MessageID, t.Lang, t.Text,
	)
	if err != nil {
		return fmt.Errorf("translationStore.Upsert: %w", err)
	}
	return nil
}
