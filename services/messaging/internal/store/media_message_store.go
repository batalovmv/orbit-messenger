package store

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

// CreateWithMedia creates a message and links media via message_media in one transaction.
func (s *messageStore) CreateWithMedia(ctx context.Context, msg *model.Message, mediaIDs []uuid.UUID, isSpoiler bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Atomically increment per-chat sequence counter
	var seq int64
	err = tx.QueryRow(ctx,
		`UPDATE chats SET next_sequence_number = next_sequence_number + 1
		 WHERE id = $1
		 RETURNING next_sequence_number - 1`,
		msg.ChatID,
	).Scan(&seq)
	if err != nil {
		return fmt.Errorf("get sequence: %w", err)
	}

	err = tx.QueryRow(ctx,
		`INSERT INTO messages (chat_id, sender_id, type, content, entities, reply_to_id, sequence_number)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, is_edited, is_deleted, is_pinned, is_forwarded, sequence_number, created_at`,
		msg.ChatID, msg.SenderID, msg.Type, msg.Content, msg.Entities, msg.ReplyToID, seq,
	).Scan(&msg.ID, &msg.IsEdited, &msg.IsDeleted, &msg.IsPinned, &msg.IsForwarded,
		&msg.SequenceNumber, &msg.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}

	// Link media to message
	for i, mediaID := range mediaIDs {
		_, err := tx.Exec(ctx,
			`INSERT INTO message_media (message_id, media_id, position, is_spoiler)
			 VALUES ($1, $2, $3, $4)`,
			msg.ID, mediaID, i, isSpoiler,
		)
		if err != nil {
			return fmt.Errorf("link media %s to message: %w", mediaID, err)
		}
	}

	return tx.Commit(ctx)
}

// GetMediaByMessageIDs batch-loads media attachments for a list of messages.
// Returns map[messageID] -> []MediaAttachment. Avoids N+1 queries.
func (s *messageStore) GetMediaByMessageIDs(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]model.MediaAttachment, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	if len(messageIDs) > 200 {
		messageIDs = messageIDs[:200]
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
		return nil, fmt.Errorf("get media by message IDs: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID][]model.MediaAttachment)
	for rows.Next() {
		var (
			messageID   uuid.UUID
			position    int
			isSpoiler   bool
			mediaID     uuid.UUID
			mediaType   string
			mimeType    string
			filename    *string
			sizeBytes   int64
			r2Key       string
			thumbKey    *string
			mediumKey   *string
			width       *int
			height      *int
			duration    *float64
			waveform    []byte
			isOneTime   bool
			procStatus  string
		)
		if err := rows.Scan(
			&messageID, &position, &isSpoiler,
			&mediaID, &mediaType, &mimeType, &filename,
			&sizeBytes, &r2Key, &thumbKey, &mediumKey,
			&width, &height, &duration, &waveform,
			&isOneTime, &procStatus,
		); err != nil {
			return nil, fmt.Errorf("scan media attachment: %w", err)
		}

		mediaIDStr := mediaID.String()
		att := model.MediaAttachment{
			MediaID:          mediaIDStr,
			Type:             mediaType,
			MimeType:         mimeType,
			SizeBytes:        sizeBytes,
			Width:            width,
			Height:           height,
			DurationSeconds:  duration,
			WaveformData:     bytesToInts(waveform),
			Position:         position,
			IsSpoiler:        isSpoiler,
			IsOneTime:        isOneTime,
			ProcessingStatus: procStatus,
			URL:              "/media/" + mediaIDStr,
		}
		if thumbKey != nil {
			att.ThumbnailURL = "/media/" + mediaIDStr + "/thumbnail"
		}
		if mediumKey != nil {
			att.MediumURL = "/media/" + mediaIDStr + "/medium"
		}
		if filename != nil {
			att.OriginalFilename = *filename
		}
		result[messageID] = append(result[messageID], att)
	}
	return result, rows.Err()
}

// ListSharedMedia returns media from a specific chat, optionally filtered by type.
// Each item includes the parent message context so the frontend can build full ApiMessage objects.
func (s *messageStore) ListSharedMedia(ctx context.Context, chatID uuid.UUID, mediaType string, cursor string, limit int) ([]model.SharedMediaItem, string, bool, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	var cursorTime *time.Time
	if cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil {
			ts, err := strconv.ParseInt(string(decoded), 10, 64)
			if err == nil {
				t := time.Unix(0, ts)
				cursorTime = &t
			}
		}
	}

	query := `
		SELECT m.id, m.type, m.mime_type, m.original_filename,
			m.size_bytes, m.r2_key, m.thumbnail_r2_key, m.medium_r2_key,
			m.width, m.height, m.duration_seconds, m.waveform_data,
			m.processing_status, m.created_at,
			msg.id, msg.sequence_number, msg.sender_id, msg.chat_id, msg.content
		FROM media m
		JOIN message_media mm ON mm.media_id = m.id
		JOIN messages msg ON msg.id = mm.message_id
		WHERE msg.chat_id = $1 AND msg.is_deleted = false
		  AND ($2::text IS NULL OR $2 = '' OR m.type = $2
		       OR ($2 = 'media' AND m.type IN ('photo', 'video')))
		  AND ($3::timestamptz IS NULL OR m.created_at < $3)
		ORDER BY m.created_at DESC
		LIMIT $4`

	rows, err := s.pool.Query(ctx, query, chatID, mediaType, cursorTime, limit+1)
	if err != nil {
		return nil, "", false, fmt.Errorf("list shared media: %w", err)
	}
	defer rows.Close()

	var items []model.SharedMediaItem
	var timestamps []time.Time
	for rows.Next() {
		var (
			mediaID     uuid.UUID
			mType       string
			mimeType    string
			filename    *string
			sizeBytes   int64
			r2Key       string
			thumbKey    *string
			mediumKey   *string
			width       *int
			height      *int
			duration    *float64
			waveform    []byte
			procStatus  string
			createdAt   time.Time
			msgID       uuid.UUID
			seqNum      int64
			senderID    uuid.UUID
			msgChatID   uuid.UUID
			msgContent  *string
		)
		if err := rows.Scan(
			&mediaID, &mType, &mimeType, &filename,
			&sizeBytes, &r2Key, &thumbKey, &mediumKey,
			&width, &height, &duration, &waveform,
			&procStatus, &createdAt,
			&msgID, &seqNum, &senderID, &msgChatID, &msgContent,
		); err != nil {
			return nil, "", false, fmt.Errorf("scan shared media: %w", err)
		}

		att := model.MediaAttachment{
			MediaID:          mediaID.String(),
			Type:             mType,
			MimeType:         mimeType,
			SizeBytes:        sizeBytes,
			Width:            width,
			Height:           height,
			DurationSeconds:  duration,
			WaveformData:     bytesToInts(waveform),
			ProcessingStatus: procStatus,
		}
		if filename != nil {
			att.OriginalFilename = *filename
		}
		content := ""
		if msgContent != nil {
			content = *msgContent
		}
		items = append(items, model.SharedMediaItem{
			MessageID:      msgID.String(),
			SequenceNumber: seqNum,
			ChatID:         msgChatID.String(),
			SenderID:       senderID.String(),
			Content:        content,
			CreatedAt:      createdAt,
			Attachment:     att,
		})
		timestamps = append(timestamps, createdAt)
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
		timestamps = timestamps[:limit]
	}

	var nextCursor string
	if hasMore && len(timestamps) > 0 {
		last := timestamps[len(timestamps)-1]
		nextCursor = base64.StdEncoding.EncodeToString(
			[]byte(strconv.FormatInt(last.UnixNano(), 10)),
		)
	}

	return items, nextCursor, hasMore, rows.Err()
}

func bytesToInts(b []byte) []int {
	if len(b) == 0 {
		return nil
	}
	out := make([]int, len(b))
	for i, v := range b {
		out[i] = int(v)
	}
	return out
}
