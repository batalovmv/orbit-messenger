package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

type ChatService struct {
	chats store.ChatStore
}

func NewChatService(chats store.ChatStore) *ChatService {
	return &ChatService{chats: chats}
}

func (s *ChatService) ListChats(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]model.ChatListItem, string, bool, error) {
	return s.chats.ListByUser(ctx, userID, cursor, limit)
}

func (s *ChatService) GetChat(ctx context.Context, chatID, userID uuid.UUID) (*model.Chat, error) {
	isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	chat, err := s.chats.GetByID(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("get chat: %w", err)
	}
	if chat == nil {
		return nil, apperror.NotFound("Chat not found")
	}
	return chat, nil
}

func (s *ChatService) CreateDirectChat(ctx context.Context, userID, otherUserID uuid.UUID) (*model.Chat, error) {
	if userID == otherUserID {
		return nil, apperror.BadRequest("Cannot create DM with yourself")
	}

	// Check if DM already exists
	existing, err := s.chats.GetDirectChat(ctx, userID, otherUserID)
	if err != nil {
		return nil, fmt.Errorf("check existing DM: %w", err)
	}
	if existing != nil {
		chat, err := s.chats.GetByID(ctx, *existing)
		if err != nil {
			return nil, fmt.Errorf("get existing chat: %w", err)
		}
		return chat, nil
	}

	chat, err := s.chats.CreateDirectChat(ctx, userID, otherUserID)
	if err != nil {
		return nil, fmt.Errorf("create DM: %w", err)
	}
	return chat, nil
}

func (s *ChatService) CreateGroup(ctx context.Context, userID uuid.UUID, name, description string) (*model.Chat, error) {
	if name == "" {
		return nil, apperror.BadRequest("Group name is required")
	}

	chat := &model.Chat{
		Type:        "group",
		Name:        &name,
		Description: &description,
		CreatedBy:   &userID,
	}
	if err := s.chats.Create(ctx, chat); err != nil {
		return nil, fmt.Errorf("create group: %w", err)
	}

	// Add creator as owner
	if err := s.chats.AddMember(ctx, chat.ID, userID, "owner"); err != nil {
		return nil, fmt.Errorf("add owner: %w", err)
	}

	return chat, nil
}

func (s *ChatService) GetMembers(ctx context.Context, chatID, userID uuid.UUID, cursor string, limit int) ([]model.ChatMember, string, bool, error) {
	isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return nil, "", false, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, "", false, apperror.Forbidden("Not a member of this chat")
	}

	return s.chats.GetMembers(ctx, chatID, cursor, limit)
}

// GetMemberIDs returns just the user IDs of chat members (lightweight, for internal use).
func (s *ChatService) GetMemberIDs(ctx context.Context, chatID uuid.UUID) ([]string, error) {
	return s.chats.GetMemberIDs(ctx, chatID)
}
