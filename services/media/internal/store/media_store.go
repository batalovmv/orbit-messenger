package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mst-corp/orbit/services/media/internal/model"
)

// Store defines the media store interface for testability.
type Store interface {
	Create(ctx context.Context, m *model.Media) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.Media, error)
	GetByIDs(ctx context.Context, ids []uuid.UUID) ([]*model.Media, error)
	Delete(ctx context.Context, id uuid.UUID) error
	UpdateProcessingStatus(ctx context.Context, id uuid.UUID, status string) error
	UpdateProcessingResult(ctx context.Context, id uuid.UUID, thumbnailKey, mediumKey *string, width, height *int, duration *float64, waveform []byte) error
	GetByMessageIDs(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]*MessageMediaRow, error)
	LinkToMessage(ctx context.Context, messageID, mediaID uuid.UUID, position int, isSpoiler bool) error
	CleanupOrphaned(ctx context.Context, maxAgeHours int) ([]string, error)
}

// MediaStore handles PostgreSQL operations for media.
type MediaStore struct {
	pool *pgxpool.Pool
}

// NewMediaStore creates a new media store.
func NewMediaStore(pool *pgxpool.Pool) Store {
	return &MediaStore{pool: pool}
}

// Create inserts a new media record.
func (s *MediaStore) Create(ctx context.Context, m *model.Media) error {
	query := `
		INSERT INTO media (id, uploader_id, type, mime_type, original_filename,
			size_bytes, r2_key, thumbnail_r2_key, medium_r2_key,
			width, height, duration_seconds, waveform_data,
			is_one_time, processing_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING created_at`

	return s.pool.QueryRow(ctx, query,
		m.ID, m.UploaderID, m.Type, m.MimeType, m.OriginalFilename,
		m.SizeBytes, m.R2Key, m.ThumbnailR2Key, m.MediumR2Key,
		m.Width, m.Height, m.DurationSeconds, m.WaveformData,
		m.IsOneTime, m.ProcessingStatus,
	).Scan(&m.CreatedAt)
}

// GetByID retrieves a media record by ID.
func (s *MediaStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Media, error) {
	query := `
		SELECT id, uploader_id, type, mime_type, original_filename,
			size_bytes, r2_key, thumbnail_r2_key, medium_r2_key,
			width, height, duration_seconds, waveform_data,
			is_one_time, processing_status, created_at
		FROM media WHERE id = $1`

	m := &model.Media{}
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&m.ID, &m.UploaderID, &m.Type, &m.MimeType, &m.OriginalFilename,
		&m.SizeBytes, &m.R2Key, &m.ThumbnailR2Key, &m.MediumR2Key,
		&m.Width, &m.Height, &m.DurationSeconds, &m.WaveformData,
		&m.IsOneTime, &m.ProcessingStatus, &m.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get media %s: %w", id, err)
	}
	return m, nil
}

// GetByIDs retrieves multiple media records by IDs (preserves order).
func (s *MediaStore) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]*model.Media, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	query := `
		SELECT id, uploader_id, type, mime_type, original_filename,
			size_bytes, r2_key, thumbnail_r2_key, medium_r2_key,
			width, height, duration_seconds, waveform_data,
			is_one_time, processing_status, created_at
		FROM media WHERE id = ANY($1)`

	rows, err := s.pool.Query(ctx, query, ids)
	if err != nil {
		return nil, fmt.Errorf("get media by ids: %w", err)
	}
	defer rows.Close()

	byID := make(map[uuid.UUID]*model.Media, len(ids))
	for rows.Next() {
		m := &model.Media{}
		if err := rows.Scan(
			&m.ID, &m.UploaderID, &m.Type, &m.MimeType, &m.OriginalFilename,
			&m.SizeBytes, &m.R2Key, &m.ThumbnailR2Key, &m.MediumR2Key,
			&m.Width, &m.Height, &m.DurationSeconds, &m.WaveformData,
			&m.IsOneTime, &m.ProcessingStatus, &m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan media: %w", err)
		}
		byID[m.ID] = m
	}

	// Preserve input order
	result := make([]*model.Media, 0, len(ids))
	for _, id := range ids {
		if m, ok := byID[id]; ok {
			result = append(result, m)
		}
	}
	return result, nil
}

// Delete removes a media record.
func (s *MediaStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM media WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete media %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("media %s not found", id)
	}
	return nil
}

// UpdateProcessingStatus updates the processing_status field.
func (s *MediaStore) UpdateProcessingStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE media SET processing_status = $1 WHERE id = $2`,
		status, id)
	return err
}

// UpdateProcessingResult updates media metadata after processing completes.
func (s *MediaStore) UpdateProcessingResult(ctx context.Context, id uuid.UUID,
	thumbnailKey, mediumKey *string, width, height *int, duration *float64, waveform []byte) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE media SET
			thumbnail_r2_key = COALESCE($1, thumbnail_r2_key),
			medium_r2_key = COALESCE($2, medium_r2_key),
			width = COALESCE($3, width),
			height = COALESCE($4, height),
			duration_seconds = COALESCE($5, duration_seconds),
			waveform_data = COALESCE($6, waveform_data),
			processing_status = 'ready'
		WHERE id = $7`,
		thumbnailKey, mediumKey, width, height, duration, waveform, id)
	return err
}

// GetByMessageIDs retrieves media for a batch of messages (avoids N+1).
// Returns map[messageID] -> []MediaAttachment.
func (s *MediaStore) GetByMessageIDs(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]*MessageMediaRow, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}

	query := `
		SELECT mm.message_id, mm.position, mm.is_spoiler,
			m.id, m.type, m.mime_type, m.original_filename,
			m.size_bytes, m.r2_key, m.thumbnail_r2_key, m.medium_r2_key,
			m.width, m.height, m.duration_seconds, m.waveform_data,
			m.is_one_time, m.processing_status
		FROM message_media mm
		JOIN media m ON m.id = mm.media_id
		WHERE mm.message_id = ANY($1)
		ORDER BY mm.message_id, mm.position`

	rows, err := s.pool.Query(ctx, query, messageIDs)
	if err != nil {
		return nil, fmt.Errorf("get message media: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID][]*MessageMediaRow)
	for rows.Next() {
		r := &MessageMediaRow{}
		if err := rows.Scan(
			&r.MessageID, &r.Position, &r.IsSpoiler,
			&r.MediaID, &r.Type, &r.MimeType, &r.OriginalFilename,
			&r.SizeBytes, &r.R2Key, &r.ThumbnailR2Key, &r.MediumR2Key,
			&r.Width, &r.Height, &r.DurationSeconds, &r.WaveformData,
			&r.IsOneTime, &r.ProcessingStatus,
		); err != nil {
			return nil, fmt.Errorf("scan message media: %w", err)
		}
		result[r.MessageID] = append(result[r.MessageID], r)
	}
	return result, nil
}

// LinkToMessage creates a message_media junction record.
func (s *MediaStore) LinkToMessage(ctx context.Context, messageID, mediaID uuid.UUID, position int, isSpoiler bool) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO message_media (message_id, media_id, position, is_spoiler) VALUES ($1, $2, $3, $4)
		 ON CONFLICT DO NOTHING`,
		messageID, mediaID, position, isSpoiler)
	return err
}

// CleanupOrphaned deletes media not linked to any message and older than maxAge.
func (s *MediaStore) CleanupOrphaned(ctx context.Context, maxAgeHours int) ([]string, error) {
	query := `
		DELETE FROM media
		WHERE id IN (
			SELECT m.id FROM media m
			LEFT JOIN message_media mm ON mm.media_id = m.id
			WHERE mm.media_id IS NULL
			  AND m.created_at < now() - make_interval(hours => $1)
			LIMIT 500
		)
		RETURNING r2_key, thumbnail_r2_key, medium_r2_key`

	rows, err := s.pool.Query(ctx, query, maxAgeHours)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var r2Key string
		var thumbKey, medKey *string
		if err := rows.Scan(&r2Key, &thumbKey, &medKey); err != nil {
			return nil, err
		}
		keys = append(keys, r2Key)
		if thumbKey != nil {
			keys = append(keys, *thumbKey)
		}
		if medKey != nil {
			keys = append(keys, *medKey)
		}
	}
	return keys, nil
}

// MessageMediaRow represents a joined media row for a message.
type MessageMediaRow struct {
	MessageID        uuid.UUID
	Position         int
	IsSpoiler        bool
	MediaID          uuid.UUID
	Type             string
	MimeType         string
	OriginalFilename *string
	SizeBytes        int64
	R2Key            string
	ThumbnailR2Key   *string
	MediumR2Key      *string
	Width            *int
	Height           *int
	DurationSeconds  *float64
	WaveformData     []byte
	IsOneTime        bool
	ProcessingStatus string
}
