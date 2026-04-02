package store

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

// ReactionStore manages emoji reactions on messages.
type ReactionStore interface {
	// Add adds an emoji reaction to a message. Returns conflict error if already exists.
	Add(ctx context.Context, messageID, userID uuid.UUID, emoji string) error
	// Remove removes an emoji reaction from a message.
	Remove(ctx context.Context, messageID, userID uuid.UUID, emoji string) error
	// ListByMessage returns all reactions for a message grouped by emoji.
	ListByMessage(ctx context.Context, messageID uuid.UUID) ([]model.ReactionSummary, error)
	// ListByMessageIDs returns grouped reactions for many messages in one query.
	ListByMessageIDs(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]model.ReactionSummary, error)
	// ListUsersByEmoji returns users who reacted with a specific emoji, with pagination.
	ListUsersByEmoji(ctx context.Context, messageID uuid.UUID, emoji string, cursor string, limit int) ([]model.Reaction, string, bool, error)
	// CountByMessage returns total reaction count for a message.
	CountByMessage(ctx context.Context, messageID uuid.UUID) (int, error)
	// GetAvailableReactions returns the reaction configuration for a chat.
	GetAvailableReactions(ctx context.Context, chatID uuid.UUID) (*model.ChatAvailableReactions, error)
	// SetAvailableReactions sets which reactions are allowed in a chat.
	SetAvailableReactions(ctx context.Context, chatID uuid.UUID, mode string, emojis []string) error
}

type reactionStore struct {
	pool *pgxpool.Pool
}

func NewReactionStore(pool *pgxpool.Pool) ReactionStore {
	return &reactionStore{pool: pool}
}

func (s *reactionStore) Add(ctx context.Context, messageID, userID uuid.UUID, emoji string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO message_reactions (message_id, user_id, emoji)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (message_id, user_id, emoji) DO NOTHING`,
		messageID, userID, emoji,
	)
	if err != nil {
		return fmt.Errorf("add reaction: %w", err)
	}
	return nil
}

func (s *reactionStore) Remove(ctx context.Context, messageID, userID uuid.UUID, emoji string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM message_reactions
		 WHERE message_id = $1 AND user_id = $2 AND emoji = $3`,
		messageID, userID, emoji,
	)
	if err != nil {
		return fmt.Errorf("remove reaction: %w", err)
	}
	return nil
}

func (s *reactionStore) ListByMessage(ctx context.Context, messageID uuid.UUID) ([]model.ReactionSummary, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT emoji,
		        COUNT(*)::int AS count,
		        COALESCE(array_agg(user_id::text ORDER BY created_at DESC), ARRAY[]::text[]) AS user_ids
		 FROM message_reactions
		 WHERE message_id = $1
		 GROUP BY emoji
		 ORDER BY COUNT(*) DESC, emoji ASC`,
		messageID,
	)
	if err != nil {
		return nil, fmt.Errorf("list reactions by message: %w", err)
	}
	defer rows.Close()

	var reactions []model.ReactionSummary
	for rows.Next() {
		var summary model.ReactionSummary
		if err := rows.Scan(&summary.Emoji, &summary.Count, &summary.UserIDs); err != nil {
			return nil, fmt.Errorf("scan reaction summary: %w", err)
		}
		reactions = append(reactions, summary)
	}

	return reactions, rows.Err()
}

func (s *reactionStore) ListByMessageIDs(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]model.ReactionSummary, error) {
	if len(messageIDs) == 0 {
		return map[uuid.UUID][]model.ReactionSummary{}, nil
	}

	rows, err := s.pool.Query(ctx,
		`SELECT message_id,
		        emoji,
		        COUNT(*)::int AS count,
		        COALESCE(array_agg(user_id::text ORDER BY created_at DESC), ARRAY[]::text[]) AS user_ids
		 FROM message_reactions
		 WHERE message_id = ANY($1)
		 GROUP BY message_id, emoji
		 ORDER BY message_id, COUNT(*) DESC, emoji ASC`,
		messageIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("list reactions by message IDs: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID][]model.ReactionSummary)
	for rows.Next() {
		var messageID uuid.UUID
		var summary model.ReactionSummary
		if err := rows.Scan(&messageID, &summary.Emoji, &summary.Count, &summary.UserIDs); err != nil {
			return nil, fmt.Errorf("scan reaction summary by message IDs: %w", err)
		}
		result[messageID] = append(result[messageID], summary)
	}

	return result, rows.Err()
}

func (s *reactionStore) ListUsersByEmoji(ctx context.Context, messageID uuid.UUID, emoji string, cursor string, limit int) ([]model.Reaction, string, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	cursorCreatedAt, cursorUserID, err := decodeReactionCursor(cursor)
	if err != nil {
		return nil, "", false, fmt.Errorf("decode reaction cursor: %w", err)
	}

	rows, err := s.pool.Query(ctx,
		`SELECT mr.message_id, mr.user_id, mr.emoji, mr.created_at,
		        COALESCE(u.display_name, '') AS display_name, u.avatar_url
		 FROM message_reactions mr
		 JOIN users u ON u.id = mr.user_id
		 WHERE mr.message_id = $1
		   AND mr.emoji = $2
		   AND (
		     $3::timestamptz IS NULL
		     OR mr.created_at < $3
		     OR (mr.created_at = $3 AND mr.user_id < $4::uuid)
		   )
		 ORDER BY mr.created_at DESC, mr.user_id DESC
		 LIMIT $5`,
		messageID, emoji, cursorCreatedAt, cursorUserID, limit+1,
	)
	if err != nil {
		return nil, "", false, fmt.Errorf("list reaction users by emoji: %w", err)
	}
	defer rows.Close()

	var reactions []model.Reaction
	for rows.Next() {
		var reaction model.Reaction
		if err := rows.Scan(
			&reaction.MessageID,
			&reaction.UserID,
			&reaction.Emoji,
			&reaction.CreatedAt,
			&reaction.DisplayName,
			&reaction.AvatarURL,
		); err != nil {
			return nil, "", false, fmt.Errorf("scan reaction user: %w", err)
		}
		reactions = append(reactions, reaction)
	}
	if err := rows.Err(); err != nil {
		return nil, "", false, err
	}

	hasMore := len(reactions) > limit
	if hasMore {
		reactions = reactions[:limit]
	}

	var nextCursor string
	if hasMore && len(reactions) > 0 {
		last := reactions[len(reactions)-1]
		nextCursor = encodeReactionCursor(last.CreatedAt, last.UserID)
	}

	return reactions, nextCursor, hasMore, nil
}

func (s *reactionStore) CountByMessage(ctx context.Context, messageID uuid.UUID) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*)::int FROM message_reactions WHERE message_id = $1`,
		messageID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count reactions by message: %w", err)
	}
	return count, nil
}

func (s *reactionStore) GetAvailableReactions(ctx context.Context, chatID uuid.UUID) (*model.ChatAvailableReactions, error) {
	cfg := &model.ChatAvailableReactions{}
	err := s.pool.QueryRow(ctx,
		`SELECT chat_id, mode, allowed_emojis, updated_at
		 FROM chat_available_reactions
		 WHERE chat_id = $1`,
		chatID,
	).Scan(&cfg.ChatID, &cfg.Mode, &cfg.AllowedEmojis, &cfg.UpdatedAt)
	if err == pgx.ErrNoRows {
		return &model.ChatAvailableReactions{
			ChatID: chatID,
			Mode:   "all",
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get available reactions: %w", err)
	}
	return cfg, nil
}

func (s *reactionStore) SetAvailableReactions(ctx context.Context, chatID uuid.UUID, mode string, emojis []string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO chat_available_reactions (chat_id, mode, allowed_emojis)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (chat_id) DO UPDATE SET
		   mode = EXCLUDED.mode,
		   allowed_emojis = EXCLUDED.allowed_emojis,
		   updated_at = NOW()`,
		chatID, mode, emojis,
	)
	if err != nil {
		return fmt.Errorf("set available reactions: %w", err)
	}
	return nil
}

func encodeReactionCursor(createdAt time.Time, userID uuid.UUID) string {
	raw := createdAt.UTC().Format(time.RFC3339Nano) + "|" + userID.String()
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

func decodeReactionCursor(cursor string) (*time.Time, *uuid.UUID, error) {
	if cursor == "" {
		return nil, nil, nil
	}

	decoded, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return nil, nil, err
	}

	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("invalid cursor format")
	}

	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, nil, err
	}

	userID, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, nil, err
	}

	return &createdAt, &userID, nil
}
