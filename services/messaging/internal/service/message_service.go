package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

type MessageService struct {
	messages store.MessageStore
	chats    store.ChatStore
	nats     *NATSPublisher
}

func NewMessageService(messages store.MessageStore, chats store.ChatStore, nats *NATSPublisher) *MessageService {
	return &MessageService{messages: messages, chats: chats, nats: nats}
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

func (s *MessageService) SendMessage(ctx context.Context, chatID, senderID uuid.UUID, content string, entities json.RawMessage, replyToID *uuid.UUID, msgType string) (*model.Message, error) {
	isMember, _, err := s.chats.IsMember(ctx, chatID, senderID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	if msgType == "" {
		msgType = "text"
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

	// Publish to NATS
	memberIDs, err := s.chats.GetMemberIDs(ctx, chatID)
	if err != nil {
		slog.Error("failed to get member IDs for NATS publish", "chat_id", chatID, "error", err)
	}
	subject := fmt.Sprintf("orbit.chat.%s.message.new", chatID.String())
	s.nats.Publish(subject, "new_message", full, memberIDs, senderID.String())

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
	msg, err := s.messages.GetByID(ctx, msgID)
	if err != nil {
		return fmt.Errorf("get message: %w", err)
	}
	if msg == nil {
		return apperror.NotFound("Message not found")
	}

	// Check delete permission:
	// - DM (direct): both participants can delete any message
	// - Group/channel: only author or admin/owner can delete
	isAuthor := msg.SenderID != nil && *msg.SenderID == userID
	if !isAuthor {
		isMember, role, err := s.chats.IsMember(ctx, msg.ChatID, userID)
		if err != nil {
			return fmt.Errorf("check membership: %w", err)
		}
		if !isMember {
			return apperror.Forbidden("Not a member of this chat")
		}

		chat, err := s.chats.GetByID(ctx, msg.ChatID)
		if err != nil {
			return fmt.Errorf("get chat: %w", err)
		}

		// In direct chats, both participants can delete any message
		isDirect := chat != nil && chat.Type == "direct"
		isAdmin := role == "owner" || role == "admin"
		if !isDirect && !isAdmin {
			return apperror.Forbidden("Only the author or chat admin can delete messages")
		}
	}

	if err := s.messages.SoftDelete(ctx, msgID); err != nil {
		return fmt.Errorf("soft delete: %w", err)
	}

	memberIDs, err := s.chats.GetMemberIDs(ctx, msg.ChatID)
	if err != nil {
		slog.Error("failed to get member IDs for NATS publish", "chat_id", msg.ChatID, "error", err)
	}
	subject := fmt.Sprintf("orbit.chat.%s.message.deleted", msg.ChatID.String())
	s.nats.Publish(subject, "message_deleted", map[string]interface{}{
		"id":              msgID.String(),
		"chat_id":         msg.ChatID.String(),
		"sequence_number": msg.SequenceNumber,
	}, memberIDs)

	return nil
}

func (s *MessageService) ForwardMessages(ctx context.Context, messageIDs []uuid.UUID, toChatID, senderID uuid.UUID) ([]model.Message, error) {
	isMember, _, err := s.chats.IsMember(ctx, toChatID, senderID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, apperror.Forbidden("Not a member of the target chat")
	}

	// Batch fetch all messages in one query (instead of N queries)
	originals, err := s.messages.GetByIDs(ctx, messageIDs)
	if err != nil {
		return nil, fmt.Errorf("batch fetch messages: %w", err)
	}

	// IDOR: check membership in each unique source chat (1 query per unique chat, not per message)
	sourceChatChecked := make(map[uuid.UUID]bool)
	var toForward []model.Message
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
	}

	if len(toForward) == 0 {
		return nil, apperror.BadRequest("No valid messages to forward")
	}

	created, err := s.messages.CreateForwarded(ctx, toForward)
	if err != nil {
		return nil, fmt.Errorf("create forwarded: %w", err)
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
	isMember, role, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return apperror.Forbidden("Not a member of this chat")
	}
	if role != "owner" && role != "admin" && role != "member" {
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
