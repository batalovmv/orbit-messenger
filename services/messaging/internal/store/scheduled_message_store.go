package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/pkg/crypto"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

// ScheduledMessageStore manages scheduled messages.
type ScheduledMessageStore interface {
	// Create creates a new scheduled message.
	Create(ctx context.Context, msg *model.ScheduledMessage) error
	// GetByID returns a scheduled message by ID.
	GetByID(ctx context.Context, id uuid.UUID) (*model.ScheduledMessage, error)
	// ListByChat returns pending scheduled messages for a chat by a specific user.
	ListByChat(ctx context.Context, chatID, senderID uuid.UUID) ([]model.ScheduledMessage, error)
	// Update updates a scheduled message's content, entities, and/or scheduled_at.
	Update(ctx context.Context, id uuid.UUID, content *string, entities []byte, scheduledAt *time.Time) error
	// Delete deletes a scheduled message. Only the sender can delete.
	Delete(ctx context.Context, id, senderID uuid.UUID) error
	// ClaimScheduled atomically marks a specific scheduled message as sent.
	// Returns true when the claim succeeded, false when the message was already sent.
	ClaimScheduled(ctx context.Context, id uuid.UUID) (bool, error)
	// MarkSent marks a scheduled message as sent.
	MarkSent(ctx context.Context, id uuid.UUID) error
	// ClaimAndMarkPending atomically claims and marks as sent all scheduled messages
	// due for delivery (scheduled_at <= now, is_sent = false). Uses a CTE with
	// FOR UPDATE SKIP LOCKED inside a single UPDATE statement so concurrent workers
	// cannot double-deliver the same message. Returns the claimed messages.
	ClaimAndMarkPending(ctx context.Context, limit int) ([]model.ScheduledMessage, error)
}

type scheduledMessageStore struct {
	pool   *pgxpool.Pool
	atRest []byte // master key for AES-256-GCM content encryption, shared with messageStore
}

// NewScheduledMessageStore creates a ScheduledMessageStore. `atRestKey` MUST be
// the same 32-byte master key used by MessageStore so plaintext never lands on
// disk, and delivery into `messages.content` round-trips through plaintext
// correctly instead of double-encrypting.
func NewScheduledMessageStore(pool *pgxpool.Pool, atRestKey []byte) ScheduledMessageStore {
	return &scheduledMessageStore{pool: pool, atRest: atRestKey}
}

func (s *scheduledMessageStore) Create(ctx context.Context, msg *model.ScheduledMessage) error {
	if msg.Type == "" {
		msg.Type = "text"
	}

	pollPayload, err := marshalScheduledPollPayload(msg.PollPayload)
	if err != nil {
		return fmt.Errorf("marshal scheduled poll payload: %w", err)
	}

	ctContent, err := crypto.EncryptContentField(msg.Content, s.atRest)
	if err != nil {
		return err
	}

	return s.pool.QueryRow(ctx,
		`INSERT INTO scheduled_messages (
		    chat_id, sender_id, content, entities, reply_to_id, type,
		    media_ids, is_spoiler, poll_payload, scheduled_at
		)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10)
		 RETURNING id, type, is_sent, sent_at, created_at, updated_at`,
		msg.ChatID,
		msg.SenderID,
		ctContent,
		msg.Entities,
		msg.ReplyToID,
		msg.Type,
		msg.MediaIDs,
		msg.IsSpoiler,
		pollPayload,
		msg.ScheduledAt,
	).Scan(&msg.ID, &msg.Type, &msg.IsSent, &msg.SentAt, &msg.CreatedAt, &msg.UpdatedAt)
}

func (s *scheduledMessageStore) GetByID(ctx context.Context, id uuid.UUID) (*model.ScheduledMessage, error) {
	msg := &model.ScheduledMessage{}
	err := s.scanScheduledMessageRow(
		s.pool.QueryRow(ctx, scheduledMessageSelectQuery(`WHERE sm.id = $1`), id),
		msg,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get scheduled message: %w", err)
	}
	messages := []model.ScheduledMessage{*msg}
	if err := s.hydrateScheduledMessagesMedia(ctx, messages); err != nil {
		return nil, fmt.Errorf("hydrate scheduled message media: %w", err)
	}
	*msg = messages[0]
	return msg, nil
}

func (s *scheduledMessageStore) ListByChat(ctx context.Context, chatID, senderID uuid.UUID) ([]model.ScheduledMessage, error) {
	rows, err := s.pool.Query(ctx,
		scheduledMessageSelectQuery(`
			WHERE sm.chat_id = $1 AND sm.sender_id = $2 AND sm.is_sent = false
			ORDER BY sm.scheduled_at ASC, sm.created_at ASC`,
		),
		chatID, senderID,
	)
	if err != nil {
		return nil, fmt.Errorf("list scheduled messages: %w", err)
	}
	defer rows.Close()

	var messages []model.ScheduledMessage
	for rows.Next() {
		var msg model.ScheduledMessage
		if err := s.scanScheduledMessageRow(rows, &msg); err != nil {
			return nil, fmt.Errorf("scan scheduled message: %w", err)
		}
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.hydrateScheduledMessagesMedia(ctx, messages); err != nil {
		return nil, fmt.Errorf("hydrate scheduled messages media: %w", err)
	}

	return messages, nil
}

func (s *scheduledMessageStore) Update(ctx context.Context, id uuid.UUID, content *string, entities []byte, scheduledAt *time.Time) error {
	ctContent, err := crypto.EncryptContentField(content, s.atRest)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE scheduled_messages
		 SET content = COALESCE($2, content),
		     entities = COALESCE($3::jsonb, entities),
		     scheduled_at = COALESCE($4, scheduled_at),
		     updated_at = NOW()
		 WHERE id = $1`,
		id, ctContent, entities, scheduledAt,
	)
	if err != nil {
		return fmt.Errorf("update scheduled message: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *scheduledMessageStore) Delete(ctx context.Context, id, senderID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM scheduled_messages
		 WHERE id = $1 AND sender_id = $2`,
		id, senderID,
	)
	if err != nil {
		return fmt.Errorf("delete scheduled message: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *scheduledMessageStore) ClaimScheduled(ctx context.Context, id uuid.UUID) (bool, error) {
	var claimedID uuid.UUID
	err := s.pool.QueryRow(ctx,
		`UPDATE scheduled_messages
		 SET is_sent = true, sent_at = NOW(), updated_at = NOW()
		 WHERE id = $1 AND is_sent = false
		 RETURNING id`,
		id,
	).Scan(&claimedID)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim scheduled message: %w", err)
	}
	return true, nil
}

func (s *scheduledMessageStore) MarkSent(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE scheduled_messages
		 SET is_sent = true, sent_at = NOW(), updated_at = NOW()
		 WHERE id = $1 AND is_sent = false`,
		id,
	)
	if err != nil {
		return fmt.Errorf("mark scheduled message sent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *scheduledMessageStore) ClaimAndMarkPending(ctx context.Context, limit int) ([]model.ScheduledMessage, error) {
	// Atomically select and mark as sent in one statement.
	// The CTE picks candidate rows with FOR UPDATE SKIP LOCKED so concurrent
	// workers each get a disjoint set — no double-delivery is possible.
	rows, err := s.pool.Query(ctx, `
		WITH pending AS (
			SELECT id FROM scheduled_messages
			WHERE is_sent = FALSE
			  AND scheduled_at <= NOW()
			ORDER BY scheduled_at ASC, created_at ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE scheduled_messages sm
		SET is_sent = TRUE, sent_at = NOW(), updated_at = NOW()
		FROM pending p
		WHERE sm.id = p.id
		RETURNING sm.id, sm.chat_id, sm.sender_id, sm.content, sm.entities,
		          sm.reply_to_id,
		          (SELECT rm.sequence_number FROM messages rm WHERE rm.id = sm.reply_to_id) AS reply_to_seq,
		          sm.type, sm.media_ids, sm.is_spoiler, sm.poll_payload, sm.scheduled_at,
		          sm.is_sent, sm.sent_at, sm.created_at, sm.updated_at
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("claim pending scheduled messages: %w", err)
	}
	defer rows.Close()

	var messages []model.ScheduledMessage
	for rows.Next() {
		var msg model.ScheduledMessage
		if err := s.scanScheduledMessageRow(rows, &msg); err != nil {
			return nil, fmt.Errorf("scan claimed scheduled message: %w", err)
		}
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return messages, nil
}

func scheduledMessageSelectQuery(suffix string) string {
	return `
		SELECT sm.id, sm.chat_id, sm.sender_id, sm.content, sm.entities, sm.reply_to_id,
		       (SELECT rm.sequence_number FROM messages rm WHERE rm.id = sm.reply_to_id) AS reply_to_seq,
		       sm.type, sm.media_ids, sm.is_spoiler, sm.poll_payload, sm.scheduled_at,
		       sm.is_sent, sm.sent_at, sm.created_at, sm.updated_at
		FROM scheduled_messages sm
	` + suffix
}

type scheduledMessageScanner interface {
	Scan(dest ...any) error
}

func (s *scheduledMessageStore) scanScheduledMessageRow(scanner scheduledMessageScanner, msg *model.ScheduledMessage) error {
	var pollPayload []byte

	err := scanner.Scan(
		&msg.ID,
		&msg.ChatID,
		&msg.SenderID,
		&msg.Content,
		&msg.Entities,
		&msg.ReplyToID,
		&msg.ReplyToSeqNum,
		&msg.Type,
		&msg.MediaIDs,
		&msg.IsSpoiler,
		&pollPayload,
		&msg.ScheduledAt,
		&msg.IsSent,
		&msg.SentAt,
		&msg.CreatedAt,
		&msg.UpdatedAt,
	)
	if err != nil {
		return err
	}

	crypto.DecryptContentField(msg.Content, s.atRest)

	if len(pollPayload) > 0 {
		var payload model.ScheduledPollPayload
		if err := json.Unmarshal(pollPayload, &payload); err != nil {
			return fmt.Errorf("unmarshal scheduled poll payload: %w", err)
		}
		msg.PollPayload = &payload
	}

	return nil
}

func marshalScheduledPollPayload(payload *model.ScheduledPollPayload) ([]byte, error) {
	if payload == nil {
		return nil, nil
	}

	return json.Marshal(payload)
}

func (s *scheduledMessageStore) hydrateScheduledMessagesMedia(
	ctx context.Context,
	messages []model.ScheduledMessage,
) error {
	var allMediaIDs []uuid.UUID
	seenMediaIDs := make(map[uuid.UUID]struct{})

	for _, message := range messages {
		for _, mediaID := range message.MediaIDs {
			if _, ok := seenMediaIDs[mediaID]; ok {
				continue
			}
			seenMediaIDs[mediaID] = struct{}{}
			allMediaIDs = append(allMediaIDs, mediaID)
		}
	}

	if len(allMediaIDs) == 0 {
		return nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, type, mime_type, original_filename,
		       size_bytes, thumbnail_r2_key, medium_r2_key,
		       width, height, duration_seconds, waveform_data,
		       is_one_time, processing_status
		FROM media
		WHERE id = ANY($1)
	`, allMediaIDs)
	if err != nil {
		return fmt.Errorf("query scheduled message media: %w", err)
	}
	defer rows.Close()

	attachmentsByMediaID := make(map[uuid.UUID]model.MediaAttachment, len(allMediaIDs))

	for rows.Next() {
		var (
			mediaID    uuid.UUID
			mediaType  string
			mimeType   string
			filename   *string
			sizeBytes  int64
			thumbKey   *string
			mediumKey  *string
			width      *int
			height     *int
			duration   *float64
			waveform   []byte
			isOneTime  bool
			procStatus string
		)
		if err := rows.Scan(
			&mediaID, &mediaType, &mimeType, &filename,
			&sizeBytes, &thumbKey, &mediumKey,
			&width, &height, &duration, &waveform,
			&isOneTime, &procStatus,
		); err != nil {
			return fmt.Errorf("scan scheduled media attachment: %w", err)
		}

		attachment := model.MediaAttachment{
			MediaID:          mediaID.String(),
			Type:             mediaType,
			MimeType:         mimeType,
			SizeBytes:        sizeBytes,
			Width:            width,
			Height:           height,
			DurationSeconds:  duration,
			WaveformData:     bytesToInts(waveform),
			IsOneTime:        isOneTime,
			ProcessingStatus: procStatus,
			URL:              "/media/" + mediaID.String(),
		}
		if thumbKey != nil {
			attachment.ThumbnailURL = "/media/" + mediaID.String() + "/thumbnail"
		}
		if mediumKey != nil {
			attachment.MediumURL = "/media/" + mediaID.String() + "/medium"
		}
		if filename != nil {
			attachment.OriginalFilename = *filename
		}

		attachmentsByMediaID[mediaID] = attachment
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate scheduled media attachments: %w", err)
	}

	for i := range messages {
		if len(messages[i].MediaIDs) == 0 {
			continue
		}

		messageAttachments := make([]model.MediaAttachment, 0, len(messages[i].MediaIDs))
		for position, mediaID := range messages[i].MediaIDs {
			attachment, ok := attachmentsByMediaID[mediaID]
			if !ok {
				continue
			}
			attachment.Position = position
			attachment.IsSpoiler = messages[i].IsSpoiler
			messageAttachments = append(messageAttachments, attachment)
		}
		messages[i].MediaAttachments = messageAttachments
	}

	return nil
}
