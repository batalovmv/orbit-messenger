package store

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

type MessageStore interface {
	Create(ctx context.Context, msg *model.Message) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.Message, error)
	ListByChat(ctx context.Context, chatID uuid.UUID, cursor string, limit int) ([]model.Message, string, bool, error)
	FindByChatAndDate(ctx context.Context, chatID uuid.UUID, date time.Time, limit int) ([]model.Message, string, bool, error)
	Update(ctx context.Context, msg *model.Message) error
	SoftDelete(ctx context.Context, id uuid.UUID) error
	ListPinned(ctx context.Context, chatID uuid.UUID) ([]model.Message, error)
	Pin(ctx context.Context, chatID, msgID uuid.UUID) error
	Unpin(ctx context.Context, chatID, msgID uuid.UUID) error
	UnpinAll(ctx context.Context, chatID uuid.UUID) error
	UpdateReadPointer(ctx context.Context, chatID, userID, lastReadMsgID uuid.UUID) error
	CreateForwarded(ctx context.Context, msgs []model.Message) ([]model.Message, error)
}

type messageStore struct {
	pool *pgxpool.Pool
}

func NewMessageStore(pool *pgxpool.Pool) MessageStore {
	return &messageStore{pool: pool}
}

func (s *messageStore) Create(ctx context.Context, msg *model.Message) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO messages (chat_id, sender_id, type, content, reply_to_id)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, is_edited, is_deleted, is_pinned, is_forwarded, sequence_number, created_at`,
		msg.ChatID, msg.SenderID, msg.Type, msg.Content, msg.ReplyToID,
	).Scan(&msg.ID, &msg.IsEdited, &msg.IsDeleted, &msg.IsPinned, &msg.IsForwarded,
		&msg.SequenceNumber, &msg.CreatedAt)
}

func (s *messageStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Message, error) {
	msg := &model.Message{}
	err := s.pool.QueryRow(ctx,
		`SELECT m.id, m.chat_id, m.sender_id, m.type, m.content, m.reply_to_id,
		        m.is_edited, m.is_deleted, m.is_pinned, m.is_forwarded, m.forwarded_from,
		        m.sequence_number, m.created_at, m.edited_at,
		        COALESCE(u.display_name, '') as sender_name, u.avatar_url as sender_avatar_url
		 FROM messages m
		 LEFT JOIN users u ON u.id = m.sender_id
		 WHERE m.id = $1`, id,
	).Scan(&msg.ID, &msg.ChatID, &msg.SenderID, &msg.Type, &msg.Content, &msg.ReplyToID,
		&msg.IsEdited, &msg.IsDeleted, &msg.IsPinned, &msg.IsForwarded, &msg.ForwardedFrom,
		&msg.SequenceNumber, &msg.CreatedAt, &msg.EditedAt,
		&msg.SenderName, &msg.SenderAvatarURL)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return msg, err
}

func (s *messageStore) ListByChat(ctx context.Context, chatID uuid.UUID, cursor string, limit int) ([]model.Message, string, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var cursorSeq *int64
	if cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil {
			seq, err := strconv.ParseInt(string(decoded), 10, 64)
			if err == nil {
				cursorSeq = &seq
			}
		}
	}

	query := `
		SELECT m.id, m.chat_id, m.sender_id, m.type, m.content, m.reply_to_id,
		       m.is_edited, m.is_deleted, m.is_pinned, m.is_forwarded, m.forwarded_from,
		       m.sequence_number, m.created_at, m.edited_at,
		       COALESCE(u.display_name, '') as sender_name, u.avatar_url as sender_avatar_url
		FROM messages m
		LEFT JOIN users u ON u.id = m.sender_id
		WHERE m.chat_id = $1 AND m.is_deleted = false
		  AND ($2::bigint IS NULL OR m.sequence_number < $2)
		ORDER BY m.sequence_number DESC
		LIMIT $3`

	rows, err := s.pool.Query(ctx, query, chatID, cursorSeq, limit+1)
	if err != nil {
		return nil, "", false, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var messages []model.Message
	for rows.Next() {
		var m model.Message
		if err := rows.Scan(&m.ID, &m.ChatID, &m.SenderID, &m.Type, &m.Content, &m.ReplyToID,
			&m.IsEdited, &m.IsDeleted, &m.IsPinned, &m.IsForwarded, &m.ForwardedFrom,
			&m.SequenceNumber, &m.CreatedAt, &m.EditedAt,
			&m.SenderName, &m.SenderAvatarURL); err != nil {
			return nil, "", false, err
		}
		messages = append(messages, m)
	}

	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}

	var nextCursor string
	if hasMore && len(messages) > 0 {
		last := messages[len(messages)-1]
		nextCursor = base64.StdEncoding.EncodeToString(
			[]byte(strconv.FormatInt(last.SequenceNumber, 10)),
		)
	}

	return messages, nextCursor, hasMore, rows.Err()
}

func (s *messageStore) FindByChatAndDate(ctx context.Context, chatID uuid.UUID, date time.Time, limit int) ([]model.Message, string, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := `
		SELECT m.id, m.chat_id, m.sender_id, m.type, m.content, m.reply_to_id,
		       m.is_edited, m.is_deleted, m.is_pinned, m.is_forwarded, m.forwarded_from,
		       m.sequence_number, m.created_at, m.edited_at,
		       COALESCE(u.display_name, '') as sender_name, u.avatar_url as sender_avatar_url
		FROM messages m
		LEFT JOIN users u ON u.id = m.sender_id
		WHERE m.chat_id = $1 AND m.is_deleted = false AND m.created_at >= $2
		ORDER BY m.created_at ASC
		LIMIT $3`

	rows, err := s.pool.Query(ctx, query, chatID, date, limit+1)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()

	var messages []model.Message
	for rows.Next() {
		var m model.Message
		if err := rows.Scan(&m.ID, &m.ChatID, &m.SenderID, &m.Type, &m.Content, &m.ReplyToID,
			&m.IsEdited, &m.IsDeleted, &m.IsPinned, &m.IsForwarded, &m.ForwardedFrom,
			&m.SequenceNumber, &m.CreatedAt, &m.EditedAt,
			&m.SenderName, &m.SenderAvatarURL); err != nil {
			return nil, "", false, err
		}
		messages = append(messages, m)
	}

	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}

	var nextCursor string
	if hasMore && len(messages) > 0 {
		last := messages[len(messages)-1]
		nextCursor = base64.StdEncoding.EncodeToString(
			[]byte(strconv.FormatInt(last.SequenceNumber, 10)),
		)
	}

	return messages, nextCursor, hasMore, rows.Err()
}

func (s *messageStore) Update(ctx context.Context, msg *model.Message) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE messages SET content = $1, is_edited = true, edited_at = now()
		 WHERE id = $2`,
		msg.Content, msg.ID,
	)
	return err
}

func (s *messageStore) SoftDelete(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE messages SET is_deleted = true, content = NULL WHERE id = $1`, id,
	)
	return err
}

func (s *messageStore) ListPinned(ctx context.Context, chatID uuid.UUID) ([]model.Message, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT m.id, m.chat_id, m.sender_id, m.type, m.content, m.reply_to_id,
		        m.is_edited, m.is_deleted, m.is_pinned, m.is_forwarded, m.forwarded_from,
		        m.sequence_number, m.created_at, m.edited_at,
		        COALESCE(u.display_name, '') as sender_name, u.avatar_url as sender_avatar_url
		 FROM messages m
		 LEFT JOIN users u ON u.id = m.sender_id
		 WHERE m.chat_id = $1 AND m.is_pinned = true AND m.is_deleted = false
		 ORDER BY m.created_at DESC`, chatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []model.Message
	for rows.Next() {
		var m model.Message
		if err := rows.Scan(&m.ID, &m.ChatID, &m.SenderID, &m.Type, &m.Content, &m.ReplyToID,
			&m.IsEdited, &m.IsDeleted, &m.IsPinned, &m.IsForwarded, &m.ForwardedFrom,
			&m.SequenceNumber, &m.CreatedAt, &m.EditedAt,
			&m.SenderName, &m.SenderAvatarURL); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

func (s *messageStore) Pin(ctx context.Context, chatID, msgID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE messages SET is_pinned = true WHERE id = $1 AND chat_id = $2`, msgID, chatID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *messageStore) Unpin(ctx context.Context, chatID, msgID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE messages SET is_pinned = false WHERE id = $1 AND chat_id = $2`, msgID, chatID,
	)
	return err
}

func (s *messageStore) UnpinAll(ctx context.Context, chatID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE messages SET is_pinned = false WHERE chat_id = $1 AND is_pinned = true`, chatID,
	)
	return err
}

func (s *messageStore) UpdateReadPointer(ctx context.Context, chatID, userID, lastReadMsgID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chat_members SET last_read_message_id = $1
		 WHERE chat_id = $2 AND user_id = $3`,
		lastReadMsgID, chatID, userID,
	)
	return err
}

func (s *messageStore) CreateForwarded(ctx context.Context, msgs []model.Message) ([]model.Message, error) {
	var result []model.Message
	for i := range msgs {
		m := &msgs[i]
		err := s.pool.QueryRow(ctx,
			`INSERT INTO messages (chat_id, sender_id, type, content, is_forwarded, forwarded_from)
			 VALUES ($1, $2, $3, $4, true, $5)
			 RETURNING id, is_edited, is_deleted, is_pinned, sequence_number, created_at`,
			m.ChatID, m.SenderID, m.Type, m.Content, m.ForwardedFrom,
		).Scan(&m.ID, &m.IsEdited, &m.IsDeleted, &m.IsPinned, &m.SequenceNumber, &m.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("create forwarded message: %w", err)
		}
		m.IsForwarded = true
		result = append(result, *m)
	}
	return result, nil
}
