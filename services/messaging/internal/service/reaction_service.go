package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

// ReactionService handles business logic for message reactions.
type ReactionService struct {
	reactions store.ReactionStore
	messages  store.MessageStore
	chats     store.ChatStore
	nats      Publisher
	logger    *slog.Logger
}

// NewReactionService creates a new ReactionService.
func NewReactionService(
	reactions store.ReactionStore,
	messages store.MessageStore,
	chats store.ChatStore,
	nats Publisher,
	logger *slog.Logger,
) *ReactionService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ReactionService{
		reactions: reactions,
		messages:  messages,
		chats:     chats,
		nats:      nats,
		logger:    logger,
	}
}

// AddReaction adds an emoji reaction to a message.
func (s *ReactionService) AddReaction(ctx context.Context, messageID, userID uuid.UUID, emoji string) error {
	emoji = strings.TrimSpace(emoji)
	if emoji == "" {
		return apperror.BadRequest("Emoji is required")
	}

	msg, err := s.getAccessibleMessage(ctx, messageID, userID)
	if err != nil {
		return err
	}

	allowed, err := s.reactions.GetAvailableReactions(ctx, msg.ChatID)
	if err != nil {
		return fmt.Errorf("get available reactions: %w", err)
	}
	if allowed == nil {
		allowed = &model.ChatAvailableReactions{ChatID: msg.ChatID, Mode: "all"}
	}

	switch allowed.Mode {
	case "none":
		return apperror.Forbidden("Reactions are disabled in this chat")
	case "selected":
		if !containsEmoji(allowed.AllowedEmojis, emoji) {
			return apperror.BadRequest("Emoji is not allowed in this chat")
		}
	case "all", "":
	default:
		return apperror.BadRequest("Invalid reactions mode")
	}

	// Enforce one reaction per user per message: remove any existing reactions first.
	if err := s.reactions.RemoveAllByUser(ctx, messageID, userID); err != nil {
		return fmt.Errorf("remove existing reactions: %w", err)
	}

	if err := s.reactions.Add(ctx, messageID, userID, emoji); err != nil {
		return fmt.Errorf("add reaction: %w", err)
	}

	s.publishReactionEvent(ctx, msg, "reaction_added", map[string]any{
		"chat_id":         msg.ChatID.String(),
		"message_id":      messageID.String(),
		"sequence_number": msg.SequenceNumber,
		"user_id":         userID.String(),
		"emoji":           emoji,
	}, userID)

	return nil
}

// RemoveReaction removes an emoji reaction from a message.
func (s *ReactionService) RemoveReaction(ctx context.Context, messageID, userID uuid.UUID, emoji string) error {
	emoji = strings.TrimSpace(emoji)
	if emoji == "" {
		return apperror.BadRequest("Emoji is required")
	}

	msg, err := s.getAccessibleMessage(ctx, messageID, userID)
	if err != nil {
		return err
	}

	if err := s.reactions.Remove(ctx, messageID, userID, emoji); err != nil {
		return fmt.Errorf("remove reaction: %w", err)
	}

	s.publishReactionEvent(ctx, msg, "reaction_removed", map[string]any{
		"chat_id":         msg.ChatID.String(),
		"message_id":      messageID.String(),
		"sequence_number": msg.SequenceNumber,
		"user_id":         userID.String(),
		"emoji":           emoji,
	}, userID)

	return nil
}

// ListReactions returns reaction summaries for a message.
func (s *ReactionService) ListReactions(ctx context.Context, messageID, userID uuid.UUID) ([]model.ReactionSummary, error) {
	if _, err := s.getAccessibleMessage(ctx, messageID, userID); err != nil {
		return nil, err
	}

	reactions, err := s.reactions.ListByMessage(ctx, messageID)
	if err != nil {
		return nil, fmt.Errorf("list reactions: %w", err)
	}

	return reactions, nil
}

// ListReactionUsers returns users who reacted with a specific emoji.
func (s *ReactionService) ListReactionUsers(ctx context.Context, messageID, userID uuid.UUID, emoji string, cursor string, limit int) ([]model.Reaction, string, bool, error) {
	emoji = strings.TrimSpace(emoji)
	if emoji == "" {
		return nil, "", false, apperror.BadRequest("Emoji is required")
	}

	if _, err := s.getAccessibleMessage(ctx, messageID, userID); err != nil {
		return nil, "", false, err
	}

	reactions, nextCursor, hasMore, err := s.reactions.ListUsersByEmoji(ctx, messageID, emoji, cursor, limit)
	if err != nil {
		return nil, "", false, fmt.Errorf("list reaction users: %w", err)
	}

	return reactions, nextCursor, hasMore, nil
}

// SetAvailableReactions sets which reactions are allowed in a chat (admin only).
func (s *ReactionService) SetAvailableReactions(ctx context.Context, chatID, userID uuid.UUID, mode string, emojis []string) error {
	isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return apperror.Forbidden("Not a member of this chat")
	}

	member, err := s.chats.GetMember(ctx, chatID, userID)
	if err != nil {
		return fmt.Errorf("get member: %w", err)
	}
	if member == nil {
		return apperror.Forbidden("Not a member of this chat")
	}
	if !permissions.IsAdminOrOwner(member.Role) {
		return apperror.Forbidden("Only admins can configure available reactions")
	}

	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "all":
		emojis = nil
	case "selected":
		emojis = normalizeEmojiList(emojis)
		if len(emojis) == 0 {
			return apperror.BadRequest("At least one emoji is required for selected mode")
		}
	case "none":
		emojis = nil
	default:
		return apperror.BadRequest("Invalid reactions mode")
	}

	if err := s.reactions.SetAvailableReactions(ctx, chatID, mode, emojis); err != nil {
		return fmt.Errorf("set available reactions: %w", err)
	}

	return nil
}

func (s *ReactionService) GetAvailableReactions(ctx context.Context, chatID, userID uuid.UUID) (*model.ChatAvailableReactions, error) {
	isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	reactions, err := s.reactions.GetAvailableReactions(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("get available reactions: %w", err)
	}
	if reactions == nil {
		return &model.ChatAvailableReactions{ChatID: chatID, Mode: "all"}, nil
	}

	return reactions, nil
}

func (s *ReactionService) HydrateMessageReactions(ctx context.Context, msgs []model.Message) error {
	if len(msgs) == 0 {
		return nil
	}

	messageIDs := make([]uuid.UUID, 0, len(msgs))
	for i := range msgs {
		messageIDs = append(messageIDs, msgs[i].ID)
	}

	reactionsByMessageID, err := s.reactions.ListByMessageIDs(ctx, messageIDs)
	if err != nil {
		return fmt.Errorf("list reactions by message IDs: %w", err)
	}

	for i := range msgs {
		if reactions, ok := reactionsByMessageID[msgs[i].ID]; ok {
			msgs[i].Reactions = reactions
		}
	}

	return nil
}

func (s *ReactionService) getAccessibleMessage(ctx context.Context, messageID, userID uuid.UUID) (*model.Message, error) {
	msg, err := s.messages.GetByID(ctx, messageID)
	if err != nil {
		return nil, fmt.Errorf("get message: %w", err)
	}
	if msg == nil {
		return nil, apperror.NotFound("Message not found")
	}

	isMember, _, err := s.chats.IsMember(ctx, msg.ChatID, userID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	return msg, nil
}

func (s *ReactionService) publishReactionEvent(ctx context.Context, msg *model.Message, event string, data map[string]any, senderID uuid.UUID) {
	if s.nats == nil {
		return
	}

	memberIDs, err := s.chats.GetMemberIDs(ctx, msg.ChatID)
	if err != nil {
		s.logger.Error("failed to get member IDs for reaction event", "chat_id", msg.ChatID, "error", err)
		memberIDs = nil
	}

	subject := fmt.Sprintf("orbit.chat.%s.message.updated", msg.ChatID.String())
	s.nats.Publish(subject, event, data, memberIDs, senderID.String())
}

func containsEmoji(allowed []string, emoji string) bool {
	for _, item := range allowed {
		if item == emoji {
			return true
		}
	}
	return false
}

func normalizeEmojiList(emojis []string) []string {
	if len(emojis) == 0 {
		return nil
	}

	result := make([]string, 0, len(emojis))
	seen := make(map[string]struct{}, len(emojis))
	for _, emoji := range emojis {
		emoji = strings.TrimSpace(emoji)
		if emoji == "" {
			continue
		}
		if _, ok := seen[emoji]; ok {
			continue
		}
		seen[emoji] = struct{}{}
		result = append(result, emoji)
	}

	return result
}
