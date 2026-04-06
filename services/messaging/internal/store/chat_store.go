package store

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

type ChatStore interface {
	ListByUser(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]model.ChatListItem, string, bool, error)
	GetByID(ctx context.Context, chatID uuid.UUID) (*model.Chat, error)
	Create(ctx context.Context, chat *model.Chat) error
	GetDirectChat(ctx context.Context, user1, user2 uuid.UUID) (*uuid.UUID, error)
	CreateDirectChat(ctx context.Context, user1, user2 uuid.UUID) (*model.Chat, error)
	GetMembers(ctx context.Context, chatID uuid.UUID, cursor string, limit int) ([]model.ChatMember, string, bool, error)
	SearchMembers(ctx context.Context, chatID uuid.UUID, query string, limit int) ([]model.ChatMember, error)
	GetMemberIDs(ctx context.Context, chatID uuid.UUID) ([]string, error)
	AddMember(ctx context.Context, chatID, userID uuid.UUID, role string) error
	IsMember(ctx context.Context, chatID, userID uuid.UUID) (bool, string, error)
	GetContactIDs(ctx context.Context, userID uuid.UUID) ([]string, error)
	// Phase 2
	UpdateChat(ctx context.Context, chatID uuid.UUID, name, description *string, avatarURL *string) error
	DeleteChat(ctx context.Context, chatID uuid.UUID) error
	GetMember(ctx context.Context, chatID, userID uuid.UUID) (*model.ChatMember, error)
	GetAdmins(ctx context.Context, chatID uuid.UUID) ([]model.ChatMember, error)
	AddMembers(ctx context.Context, chatID uuid.UUID, userIDs []uuid.UUID, role string) error
	RemoveMember(ctx context.Context, chatID, userID uuid.UUID) error
	UpdateMemberRole(ctx context.Context, chatID, userID uuid.UUID, role string, permissions int64, customTitle *string) error
	UpdateDefaultPermissions(ctx context.Context, chatID uuid.UUID, perms int64) error
	UpdateMemberPermissions(ctx context.Context, chatID, userID uuid.UUID, perms int64) error
	UpdateMemberPreferences(ctx context.Context, chatID, userID uuid.UUID, prefs model.ChatMemberPreferences) (*model.ChatMember, error)
	SetSlowMode(ctx context.Context, chatID uuid.UUID, seconds int) error
	SetSignatures(ctx context.Context, chatID uuid.UUID, enabled bool) error
	ClearChatPhoto(ctx context.Context, chatID uuid.UUID) error
	ListAll(ctx context.Context, limit int) ([]model.Chat, error)
	GetCommonChats(ctx context.Context, userA, userB uuid.UUID, limit int) ([]model.Chat, error)
	GetOrCreateSavedChat(ctx context.Context, userID uuid.UUID) (*model.Chat, error)
}

type chatStore struct {
	pool *pgxpool.Pool
}

func NewChatStore(pool *pgxpool.Pool) ChatStore {
	return &chatStore{pool: pool}
}

func (s *chatStore) ListByUser(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]model.ChatListItem, string, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	// Decode cursor (timestamp of last chat's activity)
	var cursorTime time.Time
	if cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil {
			cursorTime, _ = time.Parse(time.RFC3339Nano, string(decoded))
		}
	}

	query := `
		SELECT c.id, c.type, c.name, c.description, c.avatar_url, c.created_by,
		       c.is_encrypted, c.max_members, c.default_permissions, c.slow_mode_seconds,
		       c.is_signatures, c.created_at, c.updated_at,
		       m.id, m.chat_id, m.sender_id, m.type, m.content, m.reply_to_id,
		       m.is_edited, m.is_deleted, m.is_pinned, m.is_forwarded, m.forwarded_from,
		       m.grouped_id, m.is_one_time, m.sequence_number, m.created_at, m.edited_at,
		       m.viewed_at, m.viewed_by,
		       (SELECT COUNT(*) FROM chat_members cm2 WHERE cm2.chat_id = c.id) as member_count,
		       (SELECT COUNT(*) FROM messages msg
		        WHERE msg.chat_id = c.id AND msg.is_deleted = false
		        AND msg.sequence_number > COALESCE(
		            (SELECT m2.sequence_number FROM messages m2 WHERE m2.id = cm.last_read_message_id), 0
		        )) as unread_count,
		       cm.is_pinned, cm.is_muted, cm.is_archived,
		       ou.id, ou.display_name, ou.avatar_url, ou.status, ou.last_seen_at
		FROM chat_members cm
		JOIN chats c ON c.id = cm.chat_id
		LEFT JOIN LATERAL (
		    SELECT * FROM messages
		    WHERE chat_id = c.id AND is_deleted = false
		    ORDER BY sequence_number DESC LIMIT 1
		) m ON true
		LEFT JOIN LATERAL (
		    SELECT u.id, u.display_name, u.avatar_url, u.status, u.last_seen_at
		    FROM chat_members ocm
		    JOIN users u ON u.id = ocm.user_id
		    WHERE ocm.chat_id = c.id AND ocm.user_id != $1 AND c.type = 'direct'
		    LIMIT 1
		) ou ON c.type = 'direct'
		WHERE cm.user_id = $1
		  AND ($2::timestamptz IS NULL OR COALESCE(m.created_at, c.created_at) < $2)
		ORDER BY COALESCE(m.created_at, c.created_at) DESC
		LIMIT $3`

	var cursorParam *time.Time
	if !cursorTime.IsZero() {
		cursorParam = &cursorTime
	}

	rows, err := s.pool.Query(ctx, query, userID, cursorParam, limit+1)
	if err != nil {
		return nil, "", false, fmt.Errorf("list chats: %w", err)
	}
	defer rows.Close()

	var items []model.ChatListItem
	for rows.Next() {
		var item model.ChatListItem
		var msg model.Message
		var msgID, msgChatID, msgSenderID, msgReplyToID, msgForwardedFrom *uuid.UUID
		var msgType, msgContent, msgGroupedID *string
		var msgSeq *int64
		var msgCreatedAt, msgEditedAt, msgViewedAt *time.Time
		var msgViewedBy *uuid.UUID
		var msgIsEdited, msgIsDeleted, msgIsPinned, msgIsForwarded, msgIsOneTime *bool
		var ouID *uuid.UUID
		var ouDisplayName *string
		var ouAvatarURL *string
		var ouStatus *string
		var ouLastSeenAt *time.Time

		err := rows.Scan(
			&item.Chat.ID, &item.Chat.Type, &item.Chat.Name, &item.Chat.Description,
			&item.Chat.AvatarURL, &item.Chat.CreatedBy, &item.Chat.IsEncrypted,
			&item.Chat.MaxMembers, &item.Chat.DefaultPermissions, &item.Chat.SlowModeSeconds,
			&item.Chat.IsSignatures, &item.Chat.CreatedAt, &item.Chat.UpdatedAt,
			&msgID, &msgChatID, &msgSenderID, &msgType, &msgContent, &msgReplyToID,
			&msgIsEdited, &msgIsDeleted, &msgIsPinned, &msgIsForwarded, &msgForwardedFrom,
			&msgGroupedID, &msgIsOneTime, &msgSeq, &msgCreatedAt, &msgEditedAt,
			&msgViewedAt, &msgViewedBy,
			&item.MemberCount, &item.UnreadCount,
			&item.Chat.IsPinned, &item.Chat.IsMuted, &item.Chat.IsArchived,
			&ouID, &ouDisplayName, &ouAvatarURL, &ouStatus, &ouLastSeenAt,
		)
		if err != nil {
			return nil, "", false, fmt.Errorf("scan chat: %w", err)
		}

		if msgID != nil {
			msg.ID = *msgID
			msg.ChatID = *msgChatID
			msg.SenderID = msgSenderID
			if msgType != nil {
				msg.Type = *msgType
			}
			msg.Content = msgContent
			msg.ReplyToID = msgReplyToID
			if msgIsEdited != nil {
				msg.IsEdited = *msgIsEdited
			}
			if msgIsDeleted != nil {
				msg.IsDeleted = *msgIsDeleted
			}
			if msgIsPinned != nil {
				msg.IsPinned = *msgIsPinned
			}
			if msgIsForwarded != nil {
				msg.IsForwarded = *msgIsForwarded
			}
			msg.ForwardedFrom = msgForwardedFrom
			msg.GroupedID = msgGroupedID
			if msgIsOneTime != nil {
				msg.IsOneTime = *msgIsOneTime
			}
			msg.ViewedAt = msgViewedAt
			msg.ViewedBy = msgViewedBy
			if msgSeq != nil {
				msg.SequenceNumber = *msgSeq
			}
			if msgCreatedAt != nil {
				msg.CreatedAt = *msgCreatedAt
			}
			msg.EditedAt = msgEditedAt
			item.LastMessage = &msg
		}

		if ouID != nil {
			item.OtherUser = &model.User{
				ID:          *ouID,
				DisplayName: *ouDisplayName,
				AvatarURL:   ouAvatarURL,
				Status:      *ouStatus,
				LastSeenAt:  ouLastSeenAt,
			}
		}

		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", false, err
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	var nextCursor string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		t := last.Chat.CreatedAt
		if last.LastMessage != nil {
			t = last.LastMessage.CreatedAt
		}
		nextCursor = base64.StdEncoding.EncodeToString([]byte(t.Format(time.RFC3339Nano)))
	}

	return items, nextCursor, hasMore, nil
}

func (s *chatStore) GetByID(ctx context.Context, chatID uuid.UUID) (*model.Chat, error) {
	c := &model.Chat{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, type, name, description, avatar_url, created_by,
		        is_encrypted, max_members, default_permissions, slow_mode_seconds,
		        is_signatures, created_at, updated_at
		 FROM chats WHERE id = $1`, chatID,
	).Scan(&c.ID, &c.Type, &c.Name, &c.Description, &c.AvatarURL, &c.CreatedBy,
		&c.IsEncrypted, &c.MaxMembers, &c.DefaultPermissions, &c.SlowModeSeconds,
		&c.IsSignatures, &c.CreatedAt, &c.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func (s *chatStore) Create(ctx context.Context, chat *model.Chat) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO chats (type, name, description, created_by, default_permissions)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, is_encrypted, max_members, default_permissions, slow_mode_seconds,
		           is_signatures, created_at, updated_at`,
		chat.Type, chat.Name, chat.Description, chat.CreatedBy, chat.DefaultPermissions,
	).Scan(&chat.ID, &chat.IsEncrypted, &chat.MaxMembers, &chat.DefaultPermissions,
		&chat.SlowModeSeconds, &chat.IsSignatures, &chat.CreatedAt, &chat.UpdatedAt)
}

func (s *chatStore) GetDirectChat(ctx context.Context, user1, user2 uuid.UUID) (*uuid.UUID, error) {
	u1, u2 := canonicalOrder(user1, user2)
	var chatID uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT chat_id FROM direct_chat_lookup WHERE user1_id = $1 AND user2_id = $2`,
		u1, u2,
	).Scan(&chatID)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &chatID, nil
}

func (s *chatStore) CreateDirectChat(ctx context.Context, user1, user2 uuid.UUID) (*model.Chat, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	chat := &model.Chat{
		Type:               "direct",
		DefaultPermissions: permissions.DefaultDirectPermissions,
	}
	err = tx.QueryRow(ctx,
		`INSERT INTO chats (type, default_permissions) VALUES ('direct', $1)
		 RETURNING id, type, is_encrypted, max_members, default_permissions, created_at, updated_at`,
		permissions.DefaultDirectPermissions,
	).Scan(&chat.ID, &chat.Type, &chat.IsEncrypted, &chat.MaxMembers, &chat.DefaultPermissions, &chat.CreatedAt, &chat.UpdatedAt)
	if err != nil {
		return nil, err
	}

	// Add both users as members
	_, err = tx.Exec(ctx,
		`INSERT INTO chat_members (chat_id, user_id, role, permissions)
		 VALUES ($1, $2, 'member', $4), ($1, $3, 'member', $4)`,
		chat.ID, user1, user2, permissions.PermissionsUnset,
	)
	if err != nil {
		return nil, err
	}

	// Create lookup entry
	u1, u2 := canonicalOrder(user1, user2)
	_, err = tx.Exec(ctx,
		`INSERT INTO direct_chat_lookup (user1_id, user2_id, chat_id) VALUES ($1, $2, $3)`,
		u1, u2, chat.ID,
	)
	if err != nil {
		return nil, err
	}

	return chat, tx.Commit(ctx)
}

func (s *chatStore) GetMembers(ctx context.Context, chatID uuid.UUID, cursor string, limit int) ([]model.ChatMember, string, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := `
		SELECT cm.chat_id, cm.user_id, cm.role, cm.permissions, cm.custom_title,
		       cm.last_read_message_id, cm.joined_at, cm.muted_until, cm.notification_level,
		       cm.is_pinned, cm.is_muted, cm.is_archived,
		       u.display_name, u.avatar_url
		FROM chat_members cm
		JOIN users u ON u.id = cm.user_id
		WHERE cm.chat_id = $1
		  AND ($2::uuid IS NULL OR cm.user_id > $2)
		ORDER BY cm.user_id
		LIMIT $3`

	var cursorID *uuid.UUID
	if cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil {
			id, err := uuid.Parse(string(decoded))
			if err == nil {
				cursorID = &id
			}
		}
	}

	rows, err := s.pool.Query(ctx, query, chatID, cursorID, limit+1)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()

	var members []model.ChatMember
	for rows.Next() {
		var m model.ChatMember
		if err := rows.Scan(&m.ChatID, &m.UserID, &m.Role, &m.Permissions, &m.CustomTitle,
			&m.LastReadMessageID, &m.JoinedAt, &m.MutedUntil, &m.NotificationLevel,
			&m.IsPinned, &m.IsMuted, &m.IsArchived,
			&m.DisplayName, &m.AvatarURL); err != nil {
			return nil, "", false, err
		}
		members = append(members, m)
	}

	hasMore := len(members) > limit
	if hasMore {
		members = members[:limit]
	}

	var nextCursor string
	if hasMore && len(members) > 0 {
		last := members[len(members)-1]
		nextCursor = base64.StdEncoding.EncodeToString([]byte(last.UserID.String()))
	}

	return members, nextCursor, hasMore, rows.Err()
}

func (s *chatStore) GetMemberIDs(ctx context.Context, chatID uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_id FROM chat_members WHERE chat_id = $1 LIMIT 10000`, chatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id.String())
	}
	return ids, rows.Err()
}

func (s *chatStore) AddMember(ctx context.Context, chatID, userID uuid.UUID, role string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO chat_members (chat_id, user_id, role, permissions) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (chat_id, user_id) DO NOTHING`,
		chatID, userID, role, permissions.PermissionsUnset,
	)
	return err
}

func (s *chatStore) IsMember(ctx context.Context, chatID, userID uuid.UUID) (bool, string, error) {
	var role string
	err := s.pool.QueryRow(ctx,
		`SELECT role FROM chat_members WHERE chat_id = $1 AND user_id = $2`,
		chatID, userID,
	).Scan(&role)
	if err == pgx.ErrNoRows {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return true, role, nil
}

// GetContactIDs returns distinct user IDs that share at least one chat with the given user.
func (s *chatStore) GetContactIDs(ctx context.Context, userID uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT cm2.user_id
		 FROM chat_members cm1
		 JOIN chat_members cm2 ON cm1.chat_id = cm2.chat_id
		 WHERE cm1.user_id = $1 AND cm2.user_id != $1
		 LIMIT 5000`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id.String())
	}
	return ids, rows.Err()
}

// SearchMembers searches chat members by display name (for @mention autocomplete).
func (s *chatStore) SearchMembers(ctx context.Context, chatID uuid.UUID, query string, limit int) ([]model.ChatMember, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx,
		`SELECT cm.chat_id, cm.user_id, cm.role, cm.permissions, cm.custom_title,
		        cm.last_read_message_id, cm.joined_at, cm.muted_until, cm.notification_level,
		        cm.is_pinned, cm.is_muted, cm.is_archived,
		        u.display_name, u.avatar_url
		 FROM chat_members cm
		 JOIN users u ON u.id = cm.user_id
		 WHERE cm.chat_id = $1 AND u.display_name ILIKE '%' || $2 || '%' ESCAPE '\'
		 ORDER BY u.display_name
		 LIMIT $3`, chatID, escapeILIKE(query), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search members: %w", err)
	}
	defer rows.Close()

	var members []model.ChatMember
	for rows.Next() {
		var m model.ChatMember
		if err := rows.Scan(&m.ChatID, &m.UserID, &m.Role, &m.Permissions, &m.CustomTitle,
			&m.LastReadMessageID, &m.JoinedAt, &m.MutedUntil, &m.NotificationLevel,
			&m.IsPinned, &m.IsMuted, &m.IsArchived,
			&m.DisplayName, &m.AvatarURL); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

func (s *chatStore) UpdateChat(ctx context.Context, chatID uuid.UUID, name, description *string, avatarURL *string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chats SET
		   name = COALESCE($2, name),
		   description = COALESCE($3, description),
		   avatar_url = COALESCE($4, avatar_url),
		   updated_at = NOW()
		 WHERE id = $1`,
		chatID, name, description, avatarURL,
	)
	return err
}

func (s *chatStore) ClearChatPhoto(ctx context.Context, chatID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chats SET avatar_url = NULL, updated_at = NOW() WHERE id = $1`,
		chatID)
	return err
}

func (s *chatStore) DeleteChat(ctx context.Context, chatID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM chats WHERE id = $1`, chatID)
	return err
}

func (s *chatStore) GetMember(ctx context.Context, chatID, userID uuid.UUID) (*model.ChatMember, error) {
	m := &model.ChatMember{}
	err := s.pool.QueryRow(ctx,
		`SELECT cm.chat_id, cm.user_id, cm.role, cm.permissions, cm.custom_title,
		        cm.last_read_message_id, cm.joined_at, cm.muted_until, cm.notification_level,
		        cm.is_pinned, cm.is_muted, cm.is_archived,
		        u.display_name, u.avatar_url
		 FROM chat_members cm
		 JOIN users u ON u.id = cm.user_id
		 WHERE cm.chat_id = $1 AND cm.user_id = $2`, chatID, userID,
	).Scan(&m.ChatID, &m.UserID, &m.Role, &m.Permissions, &m.CustomTitle,
		&m.LastReadMessageID, &m.JoinedAt, &m.MutedUntil, &m.NotificationLevel,
		&m.IsPinned, &m.IsMuted, &m.IsArchived,
		&m.DisplayName, &m.AvatarURL)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func (s *chatStore) GetAdmins(ctx context.Context, chatID uuid.UUID) ([]model.ChatMember, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT cm.chat_id, cm.user_id, cm.role, cm.permissions, cm.custom_title,
		        cm.last_read_message_id, cm.joined_at, cm.muted_until, cm.notification_level,
		        cm.is_pinned, cm.is_muted, cm.is_archived,
		        u.display_name, u.avatar_url
		 FROM chat_members cm
		 JOIN users u ON u.id = cm.user_id
		 WHERE cm.chat_id = $1 AND cm.role IN ('owner', 'admin')
		 ORDER BY cm.role, u.display_name
		 LIMIT 200`, chatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []model.ChatMember
	for rows.Next() {
		var m model.ChatMember
		if err := rows.Scan(&m.ChatID, &m.UserID, &m.Role, &m.Permissions, &m.CustomTitle,
			&m.LastReadMessageID, &m.JoinedAt, &m.MutedUntil, &m.NotificationLevel,
			&m.IsPinned, &m.IsMuted, &m.IsArchived,
			&m.DisplayName, &m.AvatarURL); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

func (s *chatStore) AddMembers(ctx context.Context, chatID uuid.UUID, userIDs []uuid.UUID, role string) error {
	if len(userIDs) == 0 {
		return nil
	}

	// Batch insert in chunks of 100 to avoid oversized queries
	const batchSize = 100
	for i := 0; i < len(userIDs); i += batchSize {
		end := i + batchSize
		if end > len(userIDs) {
			end = len(userIDs)
		}
		chunk := userIDs[i:end]

		// Build batch VALUES clause: ($1, $3, $2, $4), ($1, $5, $2, $4), ...
		args := []interface{}{chatID, role, permissions.PermissionsUnset}
		values := ""
		for j, uid := range chunk {
			if j > 0 {
				values += ", "
			}
			paramIdx := len(args) + 1
			values += fmt.Sprintf("($1, $%d, $2, $3)", paramIdx)
			args = append(args, uid)
		}

		query := fmt.Sprintf(
			`INSERT INTO chat_members (chat_id, user_id, role, permissions) VALUES %s
			 ON CONFLICT (chat_id, user_id) DO NOTHING`,
			values,
		)
		if _, err := s.pool.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("add members batch: %w", err)
		}
	}
	return nil
}

func (s *chatStore) RemoveMember(ctx context.Context, chatID, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM chat_members WHERE chat_id = $1 AND user_id = $2`,
		chatID, userID,
	)
	return err
}

func (s *chatStore) UpdateMemberRole(ctx context.Context, chatID, userID uuid.UUID, role string, permissions int64, customTitle *string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chat_members SET role = $3, permissions = $4, custom_title = $5
		 WHERE chat_id = $1 AND user_id = $2`,
		chatID, userID, role, permissions, customTitle,
	)
	return err
}

func (s *chatStore) UpdateDefaultPermissions(ctx context.Context, chatID uuid.UUID, perms int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chats SET default_permissions = $2, updated_at = NOW() WHERE id = $1`,
		chatID, perms,
	)
	return err
}

func (s *chatStore) UpdateMemberPermissions(ctx context.Context, chatID, userID uuid.UUID, perms int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chat_members SET permissions = $3 WHERE chat_id = $1 AND user_id = $2`,
		chatID, userID, perms,
	)
	return err
}

func (s *chatStore) UpdateMemberPreferences(ctx context.Context, chatID, userID uuid.UUID, prefs model.ChatMemberPreferences) (*model.ChatMember, error) {
	member := &model.ChatMember{}
	err := s.pool.QueryRow(ctx,
		`WITH updated AS (
		    UPDATE chat_members
		    SET is_pinned = COALESCE($3, is_pinned),
		        is_muted = COALESCE($4, is_muted),
		        is_archived = COALESCE($5, is_archived)
		    WHERE chat_id = $1 AND user_id = $2
		    RETURNING chat_id, user_id, role, permissions, custom_title,
		              last_read_message_id, joined_at, muted_until, notification_level,
		              is_pinned, is_muted, is_archived
		)
		SELECT u.chat_id, u.user_id, u.role, u.permissions, u.custom_title,
		       u.last_read_message_id, u.joined_at, u.muted_until, u.notification_level,
		       u.is_pinned, u.is_muted, u.is_archived,
		       usr.display_name, usr.avatar_url
		FROM updated u
		JOIN users usr ON usr.id = u.user_id`,
		chatID, userID, prefs.IsPinned, prefs.IsMuted, prefs.IsArchived,
	).Scan(
		&member.ChatID, &member.UserID, &member.Role, &member.Permissions, &member.CustomTitle,
		&member.LastReadMessageID, &member.JoinedAt, &member.MutedUntil, &member.NotificationLevel,
		&member.IsPinned, &member.IsMuted, &member.IsArchived,
		&member.DisplayName, &member.AvatarURL,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("update member preferences: %w", err)
	}

	return member, nil
}

func (s *chatStore) SetSlowMode(ctx context.Context, chatID uuid.UUID, seconds int) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chats SET slow_mode_seconds = $2, updated_at = NOW() WHERE id = $1`,
		chatID, seconds,
	)
	return err
}

func (s *chatStore) SetSignatures(ctx context.Context, chatID uuid.UUID, enabled bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chats SET is_signatures = $2, updated_at = NOW() WHERE id = $1`,
		chatID, enabled,
	)
	return err
}

func (s *chatStore) ListAll(ctx context.Context, limit int) ([]model.Chat, error) {
	if limit <= 0 {
		limit = 10000
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, type, name, description, avatar_url, created_by,
		        is_encrypted, max_members, default_permissions, slow_mode_seconds,
		        is_signatures, created_at, updated_at
		 FROM chats
		 ORDER BY created_at
		 LIMIT $1`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chats []model.Chat
	for rows.Next() {
		var c model.Chat
		if err := rows.Scan(
			&c.ID, &c.Type, &c.Name, &c.Description, &c.AvatarURL, &c.CreatedBy,
			&c.IsEncrypted, &c.MaxMembers, &c.DefaultPermissions, &c.SlowModeSeconds,
			&c.IsSignatures, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		chats = append(chats, c)
	}
	return chats, rows.Err()
}

func (s *chatStore) GetCommonChats(ctx context.Context, userA, userB uuid.UUID, limit int) ([]model.Chat, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.type, c.name, c.description, c.avatar_url, c.created_by,
		        c.is_encrypted, c.max_members, c.default_permissions, c.slow_mode_seconds,
		        c.is_signatures, c.created_at, c.updated_at
		 FROM chats c
		 JOIN chat_members cm1 ON cm1.chat_id = c.id AND cm1.user_id = $1
		 JOIN chat_members cm2 ON cm2.chat_id = c.id AND cm2.user_id = $2
		 WHERE c.type IN ('group', 'channel')
		   AND cm1.role != 'banned' AND cm2.role != 'banned'
		 ORDER BY c.name
		 LIMIT $3`, userA, userB, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("get common chats: %w", err)
	}
	defer rows.Close()

	var chats []model.Chat
	for rows.Next() {
		var c model.Chat
		if err := rows.Scan(
			&c.ID, &c.Type, &c.Name, &c.Description, &c.AvatarURL, &c.CreatedBy,
			&c.IsEncrypted, &c.MaxMembers, &c.DefaultPermissions, &c.SlowModeSeconds,
			&c.IsSignatures, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		chats = append(chats, c)
	}
	return chats, rows.Err()
}

func (s *chatStore) GetOrCreateSavedChat(ctx context.Context, userID uuid.UUID) (*model.Chat, error) {
	// Check saved_messages_lookup first
	var chatID uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT chat_id FROM saved_messages_lookup WHERE user_id = $1`, userID,
	).Scan(&chatID)
	if err == nil {
		return s.GetByID(ctx, chatID)
	}
	if err != pgx.ErrNoRows {
		return nil, fmt.Errorf("get saved chat: %w", err)
	}

	// Create new saved chat
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	chat := &model.Chat{}
	name := "Saved Messages"
	err = tx.QueryRow(ctx,
		`INSERT INTO chats (type, name, created_by)
		 VALUES ('direct', $1, $2)
		 RETURNING id, type, name, description, avatar_url, created_by,
		           is_encrypted, max_members, default_permissions, slow_mode_seconds,
		           is_signatures, created_at, updated_at`,
		name, userID,
	).Scan(&chat.ID, &chat.Type, &chat.Name, &chat.Description, &chat.AvatarURL, &chat.CreatedBy,
		&chat.IsEncrypted, &chat.MaxMembers, &chat.DefaultPermissions, &chat.SlowModeSeconds,
		&chat.IsSignatures, &chat.CreatedAt, &chat.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create saved chat: %w", err)
	}

	// Add user as sole member
	_, err = tx.Exec(ctx,
		`INSERT INTO chat_members (chat_id, user_id, role) VALUES ($1, $2, 'owner')`,
		chat.ID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("add saved chat member: %w", err)
	}

	// Register in lookup table
	_, err = tx.Exec(ctx,
		`INSERT INTO saved_messages_lookup (user_id, chat_id) VALUES ($1, $2)
		 ON CONFLICT (user_id) DO NOTHING`,
		userID, chat.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("save lookup: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit saved chat: %w", err)
	}

	return chat, nil
}

func canonicalOrder(a, b uuid.UUID) (uuid.UUID, uuid.UUID) {
	if a.String() < b.String() {
		return a, b
	}
	return b, a
}
