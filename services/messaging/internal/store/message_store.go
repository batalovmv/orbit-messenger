package store

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/pkg/crypto"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

var (
	ErrMessageForbidden  = errors.New("forbidden")
	ErrMessageNotOneTime = errors.New("message is not one-time media")
)

const messageSelectColumns = `
	m.id, m.chat_id, m.sender_id, m.type, m.content, m.entities, m.reply_to_id,
	m.is_edited, m.is_deleted, m.is_pinned, m.is_forwarded, m.forwarded_from,
	m.grouped_id, m.sequence_number, m.created_at, m.edited_at,
	m.is_one_time, m.viewed_at, m.viewed_by,
	m.reply_markup, m.via_bot_id,
	COALESCE(u.display_name, '') AS sender_name, u.avatar_url AS sender_avatar_url,
	(SELECT rm.sequence_number FROM messages rm WHERE rm.id = m.reply_to_id) AS reply_to_seq
`

type MessageStore interface {
	Create(ctx context.Context, msg *model.Message) error
	CreateWithMedia(ctx context.Context, msg *model.Message, mediaIDs []uuid.UUID, isSpoiler bool) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.Message, error)
	GetByIDs(ctx context.Context, ids []uuid.UUID) ([]model.Message, error)
	ListByChat(ctx context.Context, chatID uuid.UUID, cursor string, limit int) ([]model.Message, string, bool, error)
	FindByChatAndDate(ctx context.Context, chatID uuid.UUID, date time.Time, limit int) ([]model.Message, string, bool, error)
	Update(ctx context.Context, msg *model.Message) error
	SoftDelete(ctx context.Context, id uuid.UUID) error
	MarkOneTimeViewed(ctx context.Context, msgID, userID uuid.UUID) (*model.Message, error)
	// SoftDeleteAuthorized atomically checks author/admin permission and soft-deletes.
	// Returns (chatID, sequenceNumber, error). Uses apperror for not-found/forbidden.
	SoftDeleteAuthorized(ctx context.Context, msgID, userID uuid.UUID) (uuid.UUID, int, error)
	ListPinned(ctx context.Context, chatID uuid.UUID) ([]model.Message, error)
	Pin(ctx context.Context, chatID, msgID uuid.UUID) error
	Unpin(ctx context.Context, chatID, msgID uuid.UUID) error
	UnpinAll(ctx context.Context, chatID uuid.UUID) error
	UpdateReadPointer(ctx context.Context, chatID, userID, lastReadMsgID uuid.UUID) error
	CreateForwarded(ctx context.Context, msgs []model.Message) ([]model.Message, error)
	// Media
	GetMediaByMessageIDs(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]model.MediaAttachment, error)
	CopyMediaLinks(ctx context.Context, newMessageID uuid.UUID, mediaIDs []string) error
	ListSharedMedia(ctx context.Context, chatID uuid.UUID, mediaType string, cursor string, limit int) ([]model.SharedMediaItem, string, bool, error)
}

type messageStore struct {
	pool    *pgxpool.Pool
	atRest  []byte // master key for server-side AES-256-GCM content encryption
}

type messageScanner interface {
	Scan(dest ...any) error
}

// NewMessageStore creates a MessageStore. `atRestKey` MUST be a 32-byte key
// used for AES-256-GCM encryption of plaintext message content. The server
// wraps `messages.content` on every write and unwraps it on every read, so
// a stolen Postgres dump leaks nothing readable — while admin/compliance
// reads through the service layer stay fully transparent.
func NewMessageStore(pool *pgxpool.Pool, atRestKey []byte) MessageStore {
	return &messageStore{pool: pool, atRest: atRestKey}
}

// encryptContent wraps nullable plaintext with AES-256-GCM. Returns the
// same *nil* pointer when the input is nil so INSERTs/UPDATEs preserve
// NULLs for rows that never had plaintext (media-only, stickers, polls).
func (s *messageStore) encryptContent(p *string) (*string, error) {
	if p == nil {
		return nil, nil
	}
	ct, err := crypto.Encrypt(*p, s.atRest)
	if err != nil {
		return nil, fmt.Errorf("encrypt content: %w", err)
	}
	return &ct, nil
}

// decryptContent reverses encryptContent. If the column is NULL, nothing
// to do. Ciphertext that fails to decrypt is returned as an error so we
// surface corruption loud and early rather than silently losing data.
func (s *messageStore) decryptContent(ct *string) error {
	if ct == nil || *ct == "" {
		return nil
	}
	pt, err := crypto.Decrypt(*ct, s.atRest)
	if err != nil {
		return fmt.Errorf("decrypt content: %w", err)
	}
	*ct = pt
	return nil
}

func (s *messageStore) scanMessage(scanner messageScanner, msg *model.Message) error {
	if err := scanner.Scan(
		&msg.ID, &msg.ChatID, &msg.SenderID, &msg.Type, &msg.Content, &msg.Entities, &msg.ReplyToID,
		&msg.IsEdited, &msg.IsDeleted, &msg.IsPinned, &msg.IsForwarded, &msg.ForwardedFrom,
		&msg.GroupedID, &msg.SequenceNumber, &msg.CreatedAt, &msg.EditedAt,
		&msg.IsOneTime, &msg.ViewedAt, &msg.ViewedBy,
		&msg.ReplyMarkup, &msg.ViaBotID,
		&msg.SenderName, &msg.SenderAvatarURL, &msg.ReplyToSeqNum,
	); err != nil {
		return err
	}
	return s.decryptContent(msg.Content)
}

func (s *messageStore) Create(ctx context.Context, msg *model.Message) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

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

	ctContent, err := s.encryptContent(msg.Content)
	if err != nil {
		return err
	}

	err = tx.QueryRow(ctx,
		`INSERT INTO messages (chat_id, sender_id, type, content, entities, reply_to_id, sequence_number, reply_markup, via_bot_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, is_edited, is_deleted, is_pinned, is_forwarded, is_one_time,
		           sequence_number, created_at, viewed_at, viewed_by`,
		msg.ChatID, msg.SenderID, msg.Type, ctContent, msg.Entities, msg.ReplyToID, seq, msg.ReplyMarkup, msg.ViaBotID,
	).Scan(&msg.ID, &msg.IsEdited, &msg.IsDeleted, &msg.IsPinned, &msg.IsForwarded, &msg.IsOneTime,
		&msg.SequenceNumber, &msg.CreatedAt, &msg.ViewedAt, &msg.ViewedBy)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *messageStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Message, error) {
	msg := &model.Message{}
	query := "SELECT " + messageSelectColumns + `
		FROM messages m
		LEFT JOIN users u ON u.id = m.sender_id
		WHERE m.id = $1`
	err := s.scanMessage(s.pool.QueryRow(ctx, query, id), msg)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return msg, nil
}

func (s *messageStore) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]model.Message, error) {
	if len(ids) > 200 {
		ids = ids[:200]
	}
	rows, err := s.pool.Query(ctx,
		"SELECT "+messageSelectColumns+`
		FROM messages m
		LEFT JOIN users u ON u.id = m.sender_id
		WHERE m.id = ANY($1)`,
		ids,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []model.Message
	for rows.Next() {
		var msg model.Message
		if err := s.scanMessage(rows, &msg); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}

	return messages, rows.Err()
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

	query := "SELECT " + messageSelectColumns + `
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
		var msg model.Message
		if err := s.scanMessage(rows, &msg); err != nil {
			return nil, "", false, err
		}
		messages = append(messages, msg)
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

	query := "SELECT " + messageSelectColumns + `
		FROM messages m
		LEFT JOIN users u ON u.id = m.sender_id
		WHERE m.chat_id = $1 AND m.is_deleted = false AND m.created_at >= $2
		ORDER BY m.sequence_number ASC
		LIMIT $3`

	rows, err := s.pool.Query(ctx, query, chatID, date, limit+1)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()

	var messages []model.Message
	for rows.Next() {
		var msg model.Message
		if err := s.scanMessage(rows, &msg); err != nil {
			return nil, "", false, err
		}
		messages = append(messages, msg)
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
	ctContent, err := s.encryptContent(msg.Content)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE messages SET content = $1, entities = $2, reply_markup = $3, is_edited = true, edited_at = now()
		 WHERE id = $4`,
		ctContent, msg.Entities, msg.ReplyMarkup, msg.ID,
	)
	return err
}

func (s *messageStore) SoftDelete(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE messages SET is_deleted = true, content = NULL, entities = NULL WHERE id = $1`, id,
	)
	return err
}

func (s *messageStore) MarkOneTimeViewed(ctx context.Context, msgID, userID uuid.UUID) (*model.Message, error) {
	var (
		isMember  bool
		isOneTime bool
	)

	err := s.pool.QueryRow(ctx,
		`WITH candidate AS (
		   SELECT m.id, m.is_one_time,
		          EXISTS (
		            SELECT 1
		            FROM chat_members cm
		            WHERE cm.chat_id = m.chat_id
		              AND cm.user_id = $2
		          ) AS is_member
		   FROM messages m
		   WHERE m.id = $1 AND m.is_deleted = false
		 ),
		 updated AS (
		   UPDATE messages m
		   SET viewed_at = CASE WHEN m.viewed_at IS NULL THEN now() ELSE m.viewed_at END,
		       viewed_by = CASE WHEN m.viewed_at IS NULL THEN $2 ELSE m.viewed_by END
		   FROM candidate c
		   WHERE m.id = c.id
		     AND c.is_member = true
		     AND c.is_one_time = true
		   RETURNING m.id
		 )
		 SELECT c.is_member, c.is_one_time
		 FROM candidate c`,
		msgID, userID,
	).Scan(&isMember, &isOneTime)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pgx.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("mark one-time viewed: %w", err)
	}
	if !isMember {
		return nil, ErrMessageForbidden
	}
	if !isOneTime {
		return nil, ErrMessageNotOneTime
	}

	msg, err := s.GetByID(ctx, msgID)
	if err != nil {
		return nil, fmt.Errorf("get viewed message: %w", err)
	}
	if msg == nil {
		return nil, pgx.ErrNoRows
	}

	return msg, nil
}

// SoftDeleteAuthorized atomically verifies the user is either the message author
// or an admin/owner of the chat, then soft-deletes the message in a single query.
func (s *messageStore) SoftDeleteAuthorized(ctx context.Context, msgID, userID uuid.UUID) (uuid.UUID, int, error) {
	var chatID uuid.UUID
	var seqNum int
	err := s.pool.QueryRow(ctx,
		`UPDATE messages m SET is_deleted = true, content = NULL, entities = NULL
		 WHERE m.id = $1 AND m.is_deleted = false
		 AND (
		     m.sender_id = $2
		     OR EXISTS (
		         SELECT 1 FROM chat_members cm
		         WHERE cm.chat_id = m.chat_id AND cm.user_id = $2
		         AND cm.role IN ('owner', 'admin')
		     )
		 )
		 RETURNING m.chat_id, m.sequence_number`,
		msgID, userID,
	).Scan(&chatID, &seqNum)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if scanErr := s.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM messages WHERE id = $1 AND is_deleted = false)`, msgID,
		).Scan(&exists); scanErr != nil {
			return uuid.Nil, 0, fmt.Errorf("check message existence: %w", scanErr)
		}
		if !exists {
			return uuid.Nil, 0, pgx.ErrNoRows
		}
		return uuid.Nil, 0, ErrMessageForbidden
	}
	if err != nil {
		return uuid.Nil, 0, fmt.Errorf("soft delete authorized: %w", err)
	}
	return chatID, seqNum, nil
}

func (s *messageStore) ListPinned(ctx context.Context, chatID uuid.UUID) ([]model.Message, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+messageSelectColumns+`
		FROM messages m
		LEFT JOIN users u ON u.id = m.sender_id
		WHERE m.chat_id = $1 AND m.is_pinned = true AND m.is_deleted = false
		ORDER BY m.created_at DESC
		LIMIT 200`,
		chatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []model.Message
	for rows.Next() {
		var msg model.Message
		if err := s.scanMessage(rows, &msg); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
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
		 WHERE chat_id = $2 AND user_id = $3
		   AND (
		     last_read_message_id IS NULL
		     OR (SELECT sequence_number FROM messages WHERE id = $1 AND chat_id = $2) >
		        (SELECT sequence_number FROM messages WHERE id = last_read_message_id AND chat_id = $2)
		   )`,
		lastReadMsgID, chatID, userID,
	)
	return err
}

func (s *messageStore) CreateForwarded(ctx context.Context, msgs []model.Message) ([]model.Message, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var result []model.Message
	for i := range msgs {
		msg := &msgs[i]
		var seq int64
		err := tx.QueryRow(ctx,
			`UPDATE chats SET next_sequence_number = next_sequence_number + 1
			 WHERE id = $1
			 RETURNING next_sequence_number - 1`,
			msg.ChatID,
		).Scan(&seq)
		if err != nil {
			return nil, fmt.Errorf("get sequence for forwarded: %w", err)
		}

		ctContent, err := s.encryptContent(msg.Content)
		if err != nil {
			return nil, err
		}
		err = tx.QueryRow(ctx,
			`INSERT INTO messages (chat_id, sender_id, type, content, entities, is_forwarded, forwarded_from, sequence_number)
			 VALUES ($1, $2, $3, $4, $5, true, $6, $7)
			 RETURNING id, is_edited, is_deleted, is_pinned, is_one_time,
			           sequence_number, created_at, viewed_at, viewed_by`,
			msg.ChatID, msg.SenderID, msg.Type, ctContent, msg.Entities, msg.ForwardedFrom, seq,
		).Scan(&msg.ID, &msg.IsEdited, &msg.IsDeleted, &msg.IsPinned, &msg.IsOneTime,
			&msg.SequenceNumber, &msg.CreatedAt, &msg.ViewedAt, &msg.ViewedBy)
		if err != nil {
			return nil, fmt.Errorf("create forwarded message: %w", err)
		}

		msg.IsForwarded = true
		result = append(result, *msg)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return result, nil
}
