package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
	"github.com/redis/go-redis/v9"
)

type MessageService struct {
	messages     store.MessageStore
	chats        store.ChatStore
	blockedStore store.BlockedUsersStore
	nats         Publisher
	redis        *redis.Client
}

func NewMessageService(messages store.MessageStore, chats store.ChatStore, blockedStore store.BlockedUsersStore, nats Publisher, rdb *redis.Client) *MessageService {
	return &MessageService{messages: messages, chats: chats, blockedStore: blockedStore, nats: nats, redis: rdb}
}

func (s *MessageService) ListMessages(ctx context.Context, chatID, userID uuid.UUID, cursor string, limit int) ([]model.Message, string, bool, error) {
	isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return nil, "", false, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, "", false, apperror.Forbidden("Not a member of this chat")
	}

	return s.messages.ListByChat(ctx, chatID, cursor, limit)
}

func (s *MessageService) FindByDate(ctx context.Context, chatID, userID uuid.UUID, date time.Time, limit int) ([]model.Message, string, bool, error) {
	isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return nil, "", false, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, "", false, apperror.Forbidden("Not a member of this chat")
	}

	return s.messages.FindByChatAndDate(ctx, chatID, date, limit)
}

func (s *MessageService) GetMessage(ctx context.Context, msgID, userID uuid.UUID) (*model.Message, error) {
	msg, err := s.messages.GetByID(ctx, msgID)
	if err != nil {
		return nil, fmt.Errorf("get message: %w", err)
	}
	if msg == nil || msg.IsDeleted {
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

func (s *MessageService) ViewOneTimeMessage(ctx context.Context, msgID, userID uuid.UUID) (*model.Message, error) {
	msg, err := s.messages.MarkOneTimeViewed(ctx, msgID, userID)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, apperror.NotFound("Message not found")
		case errors.Is(err, store.ErrMessageForbidden):
			return nil, apperror.Forbidden("Not a member of this chat")
		case errors.Is(err, store.ErrMessageNotOneTime):
			return nil, apperror.BadRequest("Message is not one-time media")
		default:
			return nil, fmt.Errorf("view one-time message: %w", err)
		}
	}

	s.enrichMessageMedia(ctx, msg)
	s.publishMessageUpdated(ctx, msg)

	return msg, nil
}

func (s *MessageService) SendMessage(ctx context.Context, chatID, senderID uuid.UUID, content string, entities json.RawMessage, replyToID *uuid.UUID, msgType string) (*model.Message, error) {
	chat, err := s.chats.GetByID(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("get chat: %w", err)
	}
	if chat == nil {
		return nil, apperror.NotFound("Chat not found")
	}

	member, err := s.chats.GetMember(ctx, chatID, senderID)
	if err != nil {
		return nil, fmt.Errorf("get member: %w", err)
	}
	if member == nil {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	if !permissions.CanPerform(member.Role, chat.Type, member.Permissions, chat.DefaultPermissions, permissions.CanSendMessages) {
		return nil, apperror.Forbidden("You don't have permission to send messages")
	}

	// Block check: in direct chats, check if either user has blocked the other
	if chat.Type == "direct" && s.blockedStore != nil {
		members, _, _, err := s.chats.GetMembers(ctx, chatID, "", 2)
		if err != nil {
			return nil, fmt.Errorf("get dm members: %w", err)
		}
		for _, m := range members {
			if m.UserID == senderID {
				continue
			}
			// Check if recipient blocked the sender
			blocked, err := s.blockedStore.IsBlocked(ctx, m.UserID, senderID)
			if err != nil {
				return nil, fmt.Errorf("check blocked: %w", err)
			}
			if blocked {
				return nil, apperror.Forbidden("You cannot send messages to this user")
			}
			// Check if sender blocked the recipient
			blocked, err = s.blockedStore.IsBlocked(ctx, senderID, m.UserID)
			if err != nil {
				return nil, fmt.Errorf("check blocked: %w", err)
			}
			if blocked {
				return nil, apperror.Forbidden("You have blocked this user")
			}
		}
	}

	// Slow mode: check Redis TTL key (admin/owner bypass)
	if chat.SlowModeSeconds > 0 && !permissions.IsAdminOrOwner(member.Role) {
		redisKey := fmt.Sprintf("slowmode:%s:%s", chatID, senderID)
		exists, err := s.redis.Exists(ctx, redisKey).Result()
		if err != nil {
			slog.Error("redis slow mode check failed", "error", err)
			return nil, apperror.Internal("Slow mode check temporarily unavailable")
		}
		if exists > 0 {
			ttl, ttlErr := s.redis.TTL(ctx, redisKey).Result()
			waitSec := int(ttl.Seconds())
			if ttlErr != nil || waitSec <= 0 {
				waitSec = chat.SlowModeSeconds
			}
			return nil, apperror.TooManyRequests(fmt.Sprintf("Slow mode: wait %d seconds", waitSec))
		}
	}

	if msgType == "" {
		msgType = "text"
	}

	// Validate reply_to_id belongs to the same chat
	if replyToID != nil {
		replyMsg, err := s.messages.GetByID(ctx, *replyToID)
		if err != nil {
			return nil, fmt.Errorf("check reply message: %w", err)
		}
		if replyMsg == nil || replyMsg.IsDeleted {
			return nil, apperror.BadRequest("Reply message not found")
		}
		if replyMsg.ChatID != chatID {
			return nil, apperror.BadRequest("Cannot reply to a message from a different chat")
		}
	}

	msg := &model.Message{
		ChatID:    chatID,
		SenderID:  &senderID,
		Type:      msgType,
		Content:   &content,
		Entities:  entities,
		ReplyToID: replyToID,
	}
	if err := s.messages.Create(ctx, msg); err != nil {
		return nil, fmt.Errorf("create message: %w", err)
	}

	// Fetch full message with sender info
	full, err := s.messages.GetByID(ctx, msg.ID)
	if err != nil {
		return msg, nil // Still return the message even if we can't get full info
	}

	// Slow mode: set cooldown after successful send
	if chat.SlowModeSeconds > 0 && !permissions.IsAdminOrOwner(member.Role) {
		redisKey := fmt.Sprintf("slowmode:%s:%s", chatID, senderID)
		if err := s.redis.Set(ctx, redisKey, "1", time.Duration(chat.SlowModeSeconds)*time.Second).Err(); err != nil {
			slog.Error("redis slow mode set failed", "error", err, "chat_id", chatID, "user_id", senderID)
		}
	}

	// Publish to NATS
	memberIDs, err := s.chats.GetMemberIDs(ctx, chatID)
	if err != nil {
		slog.Error("failed to get member IDs for NATS publish", "chat_id", chatID, "error", err)
	}
	subject := fmt.Sprintf("orbit.chat.%s.message.new", chatID.String())

	// Channel anonymous posting: hide sender for non-signatures channels
	if chat.Type == "channel" && !chat.IsSignatures {
		anonMsg := *full
		anonMsg.SenderID = nil
		anonMsg.SenderName = ""
		anonMsg.SenderAvatarURL = nil
		// Omit senderID from NATS envelope to prevent leaking real author
		s.nats.Publish(subject, "new_message", &anonMsg, memberIDs)
	} else {
		s.nats.Publish(subject, "new_message", full, memberIDs, senderID.String())
	}

	// Parse @mention entities and notify mentioned users
	if len(entities) > 0 {
		var ents []struct {
			Type   string `json:"type"`
			UserID string `json:"user_id"`
		}
		if json.Unmarshal(entities, &ents) == nil {
			for _, e := range ents {
				if e.Type == "mention" && e.UserID != "" {
					// Validate user_id is a proper UUID to prevent NATS subject injection
					if _, err := uuid.Parse(e.UserID); err != nil {
						continue
					}
					mentionSubject := fmt.Sprintf("orbit.user.%s.mention", e.UserID)
					s.nats.Publish(mentionSubject, "mention", map[string]interface{}{
						"chat_id":    chatID.String(),
						"message_id": msg.ID.String(),
						"sender_id":  senderID.String(),
					}, []string{e.UserID})
				}
			}
		}
	}

	return full, nil
}

func (s *MessageService) EditMessage(ctx context.Context, msgID, userID uuid.UUID, content string, entities json.RawMessage) (*model.Message, error) {
	msg, err := s.messages.GetByID(ctx, msgID)
	if err != nil {
		return nil, fmt.Errorf("get message: %w", err)
	}
	if msg == nil {
		return nil, apperror.NotFound("Message not found")
	}
	if msg.SenderID == nil || *msg.SenderID != userID {
		return nil, apperror.Forbidden("You can only edit your own messages")
	}
	if msg.IsDeleted {
		return nil, apperror.BadRequest("Cannot edit a deleted message")
	}

	msg.Content = &content
	msg.Entities = entities
	if err := s.messages.Update(ctx, msg); err != nil {
		return nil, fmt.Errorf("update message: %w", err)
	}

	// Fetch updated
	updated, err := s.messages.GetByID(ctx, msgID)
	if err != nil {
		return msg, nil
	}

	memberIDs, err := s.chats.GetMemberIDs(ctx, msg.ChatID)
	if err != nil {
		slog.Error("failed to get member IDs for NATS publish", "chat_id", msg.ChatID, "error", err)
	}
	subject := fmt.Sprintf("orbit.chat.%s.message.updated", msg.ChatID.String())
	s.nats.Publish(subject, "message_updated", updated, memberIDs)

	return updated, nil
}

func (s *MessageService) DeleteMessage(ctx context.Context, msgID, userID uuid.UUID) error {
	// Atomic ownership check + soft delete to prevent TOCTOU race
	chatID, seqNum, err := s.messages.SoftDeleteAuthorized(ctx, msgID, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperror.NotFound("Message not found")
		}
		if err.Error() == "forbidden" {
			return apperror.Forbidden("Only the author or chat admin can delete messages")
		}
		return fmt.Errorf("delete message: %w", err)
	}

	memberIDs, err := s.chats.GetMemberIDs(ctx, chatID)
	if err != nil {
		slog.Error("failed to get member IDs for NATS publish", "chat_id", chatID, "error", err)
	}
	subject := fmt.Sprintf("orbit.chat.%s.message.deleted", chatID.String())
	s.nats.Publish(subject, "message_deleted", map[string]interface{}{
		"id":              msgID.String(),
		"chat_id":         chatID.String(),
		"sequence_number": seqNum,
	}, memberIDs)

	return nil
}

func (s *MessageService) ForwardMessages(ctx context.Context, messageIDs []uuid.UUID, toChatID, senderID uuid.UUID) ([]model.Message, error) {
	chat, err := s.chats.GetByID(ctx, toChatID)
	if err != nil {
		return nil, fmt.Errorf("get target chat: %w", err)
	}
	if chat == nil {
		return nil, apperror.NotFound("Target chat not found")
	}

	member, err := s.chats.GetMember(ctx, toChatID, senderID)
	if err != nil {
		return nil, fmt.Errorf("get member: %w", err)
	}
	if member == nil {
		return nil, apperror.Forbidden("Not a member of the target chat")
	}

	if !permissions.CanPerform(member.Role, chat.Type, member.Permissions, chat.DefaultPermissions, permissions.CanSendMessages) {
		return nil, apperror.Forbidden("You don't have permission to send messages")
	}

	// Block check in direct chats
	if chat.Type == "direct" && s.blockedStore != nil {
		members, _, _, err := s.chats.GetMembers(ctx, toChatID, "", 2)
		if err != nil {
			return nil, fmt.Errorf("get dm members: %w", err)
		}
		for _, m := range members {
			if m.UserID == senderID {
				continue
			}
			blocked, err := s.blockedStore.IsBlocked(ctx, m.UserID, senderID)
			if err != nil {
				return nil, fmt.Errorf("check blocked: %w", err)
			}
			if blocked {
				return nil, apperror.Forbidden("You cannot send messages to this user")
			}
			blocked, err = s.blockedStore.IsBlocked(ctx, senderID, m.UserID)
			if err != nil {
				return nil, fmt.Errorf("check blocked: %w", err)
			}
			if blocked {
				return nil, apperror.Forbidden("You have blocked this user")
			}
		}
	}

	// Slow mode check
	if chat.SlowModeSeconds > 0 && !permissions.IsAdminOrOwner(member.Role) {
		redisKey := fmt.Sprintf("slowmode:%s:%s", toChatID, senderID)
		exists, err := s.redis.Exists(ctx, redisKey).Result()
		if err != nil {
			slog.Error("redis slow mode check failed", "error", err)
			return nil, apperror.Internal("Slow mode check temporarily unavailable")
		}
		if exists > 0 {
			return nil, apperror.TooManyRequests("Slow mode active")
		}
	}

	// Batch fetch all messages in one query (instead of N queries)
	originals, err := s.messages.GetByIDs(ctx, messageIDs)
	if err != nil {
		return nil, fmt.Errorf("batch fetch messages: %w", err)
	}

	// IDOR: check membership in each unique source chat (1 query per unique chat, not per message)
	sourceChatChecked := make(map[uuid.UUID]bool)
	var toForward []model.Message
	var origIDs []uuid.UUID
	for i := range originals {
		orig := &originals[i]
		if orig.IsDeleted {
			continue
		}
		if allowed, checked := sourceChatChecked[orig.ChatID]; checked {
			if !allowed {
				continue
			}
		} else {
			isSourceMember, _, err := s.chats.IsMember(ctx, orig.ChatID, senderID)
			sourceChatChecked[orig.ChatID] = err == nil && isSourceMember
			if err != nil || !isSourceMember {
				continue
			}
		}
		toForward = append(toForward, model.Message{
			ChatID:        toChatID,
			SenderID:      &senderID,
			Type:          orig.Type,
			Content:       orig.Content,
			Entities:      orig.Entities,
			ForwardedFrom: orig.SenderID,
		})
		origIDs = append(origIDs, orig.ID)
	}

	if len(toForward) == 0 {
		return nil, apperror.BadRequest("No valid messages to forward")
	}

	created, err := s.messages.CreateForwarded(ctx, toForward)
	if err != nil {
		return nil, fmt.Errorf("create forwarded: %w", err)
	}

	// Copy media attachments from originals to forwarded messages
	if len(origIDs) == len(created) {
		origMediaMap, mediaErr := s.messages.GetMediaByMessageIDs(ctx, origIDs)
		if mediaErr != nil {
			slog.Error("failed to get media for forwarded messages", "error", mediaErr)
		} else {
			for i, origID := range origIDs {
				attachments, ok := origMediaMap[origID]
				if !ok || len(attachments) == 0 {
					continue
				}
				mediaIDs := make([]string, len(attachments))
				for j, a := range attachments {
					mediaIDs[j] = a.MediaID
				}
				if copyErr := s.messages.CopyMediaLinks(ctx, created[i].ID, mediaIDs); copyErr != nil {
					slog.Error("failed to copy media for forwarded message", "orig_id", origID, "new_id", created[i].ID, "error", copyErr)
				}
			}
		}
	}

	// Publish events for each forwarded message
	memberIDs, err := s.chats.GetMemberIDs(ctx, toChatID)
	if err != nil {
		slog.Error("failed to get member IDs for NATS publish", "chat_id", toChatID, "error", err)
	}
	subject := fmt.Sprintf("orbit.chat.%s.message.new", toChatID.String())
	for _, m := range created {
		s.nats.Publish(subject, "new_message", m, memberIDs, senderID.String())
	}

	return created, nil
}

func (s *MessageService) PinMessage(ctx context.Context, chatID, msgID, userID uuid.UUID) error {
	member, err := s.chats.GetMember(ctx, chatID, userID)
	if err != nil {
		return fmt.Errorf("get member: %w", err)
	}
	if member == nil {
		return apperror.Forbidden("Not a member of this chat")
	}

	chat, err := s.chats.GetByID(ctx, chatID)
	if err != nil || chat == nil {
		return apperror.NotFound("Chat not found")
	}

	if !permissions.CanPerform(member.Role, chat.Type, member.Permissions, chat.DefaultPermissions, permissions.CanPinMessages) {
		return apperror.Forbidden("Not enough permissions to pin")
	}

	if err := s.messages.Pin(ctx, chatID, msgID); err != nil {
		return err
	}

	s.publishPinEvent(ctx, chatID, msgID, true)
	return nil
}

func (s *MessageService) UnpinMessage(ctx context.Context, chatID, msgID, userID uuid.UUID) error {
	isMember, role, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return apperror.Forbidden("Not a member of this chat")
	}

	// In non-DM chats, only owner/admin can unpin (consistent with UnpinAll)
	if role != "owner" && role != "admin" {
		chat, err := s.chats.GetByID(ctx, chatID)
		if err != nil {
			return fmt.Errorf("get chat: %w", err)
		}
		if chat != nil && chat.Type != "direct" {
			return apperror.Forbidden("Only admins can unpin messages")
		}
	}

	if err := s.messages.Unpin(ctx, chatID, msgID); err != nil {
		return err
	}

	s.publishPinEvent(ctx, chatID, msgID, false)
	return nil
}

func (s *MessageService) UnpinAll(ctx context.Context, chatID, userID uuid.UUID) error {
	isMember, role, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return apperror.Forbidden("Not a member of this chat")
	}

	// In DM chats, any member can unpin all. In groups, only owner/admin.
	if role != "owner" && role != "admin" {
		chat, err := s.chats.GetByID(ctx, chatID)
		if err != nil {
			return fmt.Errorf("get chat: %w", err)
		}
		if chat != nil && chat.Type != "direct" {
			return apperror.Forbidden("Only admins can unpin all messages")
		}
	}

	return s.messages.UnpinAll(ctx, chatID)
}

func (s *MessageService) publishPinEvent(ctx context.Context, chatID, msgID uuid.UUID, pinned bool) {
	msg, err := s.messages.GetByID(ctx, msgID)
	if err != nil || msg == nil {
		slog.Error("failed to get message for pin event", "msg_id", msgID, "error", err)
		return
	}
	memberIDs, err := s.chats.GetMemberIDs(ctx, chatID)
	if err != nil {
		slog.Error("failed to get member IDs for pin event", "chat_id", chatID, "error", err)
		return
	}
	eventType := "message_pinned"
	if !pinned {
		eventType = "message_unpinned"
	}
	subject := fmt.Sprintf("orbit.chat.%s.message.%s", chatID.String(), eventType)
	s.nats.Publish(subject, eventType, map[string]interface{}{
		"id":              msgID.String(),
		"chat_id":         chatID.String(),
		"sequence_number": msg.SequenceNumber,
		"is_pinned":       pinned,
	}, memberIDs)
}

func (s *MessageService) ListPinned(ctx context.Context, chatID, userID uuid.UUID) ([]model.Message, error) {
	isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	return s.messages.ListPinned(ctx, chatID)
}

func (s *MessageService) publishMessageUpdated(ctx context.Context, msg *model.Message) {
	if msg == nil {
		return
	}

	memberIDs, err := s.chats.GetMemberIDs(ctx, msg.ChatID)
	if err != nil {
		slog.Error("failed to get member IDs for NATS publish", "chat_id", msg.ChatID, "error", err)
	}

	subject := fmt.Sprintf("orbit.chat.%s.message.updated", msg.ChatID.String())
	s.nats.Publish(subject, "message_updated", msg, memberIDs)
}

// SendMediaMessage creates a message with media attachments.
func (s *MessageService) SendMediaMessage(ctx context.Context, chatID, senderID uuid.UUID,
	content string, entities json.RawMessage, replyToID *uuid.UUID, msgType string,
	mediaIDs []uuid.UUID, isSpoiler bool, groupedID *string) (*model.Message, error) {

	chat, err := s.chats.GetByID(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("get chat: %w", err)
	}
	if chat == nil {
		return nil, apperror.NotFound("Chat not found")
	}

	member, err := s.chats.GetMember(ctx, chatID, senderID)
	if err != nil {
		return nil, fmt.Errorf("get member: %w", err)
	}
	if member == nil {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	if !permissions.CanPerform(member.Role, chat.Type, member.Permissions, chat.DefaultPermissions, permissions.CanSendMedia) {
		return nil, apperror.Forbidden("You don't have permission to send media")
	}

	// Block check: in direct chats, check if either user has blocked the other
	if chat.Type == "direct" && s.blockedStore != nil {
		members, _, _, err := s.chats.GetMembers(ctx, chatID, "", 2)
		if err != nil {
			return nil, fmt.Errorf("get dm members: %w", err)
		}
		for _, m := range members {
			if m.UserID == senderID {
				continue
			}
			blocked, err := s.blockedStore.IsBlocked(ctx, m.UserID, senderID)
			if err != nil {
				return nil, fmt.Errorf("check blocked: %w", err)
			}
			if blocked {
				return nil, apperror.Forbidden("You cannot send messages to this user")
			}
			blocked, err = s.blockedStore.IsBlocked(ctx, senderID, m.UserID)
			if err != nil {
				return nil, fmt.Errorf("check blocked: %w", err)
			}
			if blocked {
				return nil, apperror.Forbidden("You have blocked this user")
			}
		}
	}

	// Slow mode check
	if chat.SlowModeSeconds > 0 && !permissions.IsAdminOrOwner(member.Role) {
		redisKey := fmt.Sprintf("slowmode:%s:%s", chatID, senderID)
		exists, err := s.redis.Exists(ctx, redisKey).Result()
		if err != nil {
			slog.Error("redis slow mode check failed", "error", err)
			return nil, apperror.Internal("Slow mode check temporarily unavailable")
		}
		if exists > 0 {
			ttl, ttlErr := s.redis.TTL(ctx, redisKey).Result()
			waitSec := int(ttl.Seconds())
			if ttlErr != nil || waitSec <= 0 {
				waitSec = chat.SlowModeSeconds
			}
			return nil, apperror.TooManyRequests(fmt.Sprintf("Slow mode: wait %d seconds", waitSec))
		}
	}

	// Validate reply_to_id belongs to the same chat
	if replyToID != nil {
		replyMsg, err := s.messages.GetByID(ctx, *replyToID)
		if err != nil {
			return nil, fmt.Errorf("check reply message: %w", err)
		}
		if replyMsg == nil || replyMsg.IsDeleted {
			return nil, apperror.BadRequest("Reply message not found")
		}
		if replyMsg.ChatID != chatID {
			return nil, apperror.BadRequest("Cannot reply to a message from a different chat")
		}
	}

	// Auto-detect message type from first media if not provided
	if msgType == "" && len(mediaIDs) > 0 {
		msgType = "photo" // will be overridden by frontend typically
	}
	if msgType == "" {
		msgType = "text"
	}

	msg := &model.Message{
		ChatID:    chatID,
		SenderID:  &senderID,
		Type:      msgType,
		Content:   strPtrOrNil(content),
		Entities:  entities,
		ReplyToID: replyToID,
		GroupedID: groupedID,
	}

	if err := s.messages.CreateWithMedia(ctx, msg, mediaIDs, isSpoiler); err != nil {
		return nil, fmt.Errorf("create media message: %w", err)
	}

	// Fetch full message with sender info + media
	full, err := s.messages.GetByID(ctx, msg.ID)
	if err != nil {
		return msg, nil
	}

	// Enrich with media attachments
	s.enrichMessageMedia(ctx, full)

	// Slow mode cooldown
	if chat.SlowModeSeconds > 0 && !permissions.IsAdminOrOwner(member.Role) {
		redisKey := fmt.Sprintf("slowmode:%s:%s", chatID, senderID)
		if err := s.redis.Set(ctx, redisKey, "1", time.Duration(chat.SlowModeSeconds)*time.Second).Err(); err != nil {
			slog.Error("redis slow mode set failed", "error", err, "chat_id", chatID, "user_id", senderID)
		}
	}

	// Publish to NATS
	memberIDs, err := s.chats.GetMemberIDs(ctx, chatID)
	if err != nil {
		slog.Error("failed to get member IDs for NATS publish", "chat_id", chatID, "error", err)
	}
	subject := fmt.Sprintf("orbit.chat.%s.message.new", chatID.String())
	if chat.Type == "channel" && !chat.IsSignatures {
		anonMsg := *full
		anonMsg.SenderID = nil
		anonMsg.SenderName = ""
		anonMsg.SenderAvatarURL = nil
		s.nats.Publish(subject, "new_message", &anonMsg, memberIDs)
	} else {
		s.nats.Publish(subject, "new_message", full, memberIDs, senderID.String())
	}

	return full, nil
}

// enrichMessageMedia loads media attachments for a single message.
func (s *MessageService) enrichMessageMedia(ctx context.Context, msg *model.Message) {
	if msg == nil {
		return
	}
	mediaMap, err := s.messages.GetMediaByMessageIDs(ctx, []uuid.UUID{msg.ID})
	if err != nil {
		slog.Error("failed to load media for message", "msg_id", msg.ID, "error", err)
		return
	}
	if atts, ok := mediaMap[msg.ID]; ok {
		msg.MediaAttachments = atts
	}
}

// EnrichMessagesMedia loads media attachments for a batch of messages. Avoids N+1.
func (s *MessageService) EnrichMessagesMedia(ctx context.Context, msgs []model.Message) {
	if len(msgs) == 0 {
		return
	}

	// Collect IDs of messages that might have media
	var mediaMessageIDs []uuid.UUID
	for i := range msgs {
		switch msgs[i].Type {
		case "photo", "video", "file", "voice", "videonote", "gif", "sticker":
			mediaMessageIDs = append(mediaMessageIDs, msgs[i].ID)
		}
	}
	if len(mediaMessageIDs) == 0 {
		return
	}

	mediaMap, err := s.messages.GetMediaByMessageIDs(ctx, mediaMessageIDs)
	if err != nil {
		slog.Error("failed to batch-load media", "error", err)
		return
	}

	for i := range msgs {
		if atts, ok := mediaMap[msgs[i].ID]; ok {
			msgs[i].MediaAttachments = atts
		}
	}
}

// ListSharedMedia returns media in a chat, optionally filtered by type.
func (s *MessageService) ListSharedMedia(ctx context.Context, chatID, userID uuid.UUID, mediaType string, cursor string, limit int) ([]model.SharedMediaItem, string, bool, error) {
	isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return nil, "", false, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, "", false, apperror.Forbidden("Not a member of this chat")
	}

	return s.messages.ListSharedMedia(ctx, chatID, mediaType, cursor, limit)
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *MessageService) MarkRead(ctx context.Context, chatID, userID, lastReadMsgID uuid.UUID) error {
	isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return apperror.Forbidden("Not a member of this chat")
	}

	if err := s.messages.UpdateReadPointer(ctx, chatID, userID, lastReadMsgID); err != nil {
		return fmt.Errorf("update read pointer: %w", err)
	}

	// Publish read event
	memberIDs, err := s.chats.GetMemberIDs(ctx, chatID)
	if err != nil {
		slog.Error("failed to get member IDs for NATS publish", "chat_id", chatID, "error", err)
	}
	subject := fmt.Sprintf("orbit.chat.%s.messages.read", chatID.String())
	s.nats.Publish(subject, "messages_read", map[string]interface{}{
		"chat_id":              chatID.String(),
		"user_id":              userID.String(),
		"last_read_message_id": lastReadMsgID.String(),
	}, memberIDs)

	return nil
}
