package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

// chatSearchIndexer is the minimal interface needed to keep the chat search index in sync.
type chatSearchIndexer interface {
	IndexChat(chatID, chatType, name, description string) error
	DeleteChat(chatID string) error
}

type ChatService struct {
	chats    store.ChatStore
	messages store.MessageStore
	nats     Publisher
	polls    messagePollHydrator
	search   chatSearchIndexer
}

type messagePollHydrator interface {
	HydrateMessagePolls(ctx context.Context, userID uuid.UUID, msgs []model.Message) error
}

// ChatServiceOption applies an optional configuration to ChatService.
type ChatServiceOption func(*ChatService)

// WithChatSearchIndexer attaches a search indexer to the ChatService.
func WithChatSearchIndexer(s chatSearchIndexer) ChatServiceOption {
	return func(c *ChatService) { c.search = s }
}

func NewChatService(
	chats store.ChatStore,
	messages store.MessageStore,
	nats Publisher,
	opts ...interface{},
) *ChatService {
	svc := &ChatService{chats: chats, messages: messages, nats: nats}
	for _, o := range opts {
		switch v := o.(type) {
		case messagePollHydrator:
			svc.polls = v
		case ChatServiceOption:
			v(svc)
		}
	}
	return svc
}

func (s *ChatService) IsMember(ctx context.Context, chatID, userID uuid.UUID) (bool, error) {
	isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return false, fmt.Errorf("check membership: %w", err)
	}
	return isMember, nil
}

func (s *ChatService) ListChats(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]model.ChatListItem, string, bool, error) {
	items, nextCursor, hasMore, err := s.chats.ListByUser(ctx, userID, cursor, limit)
	if err != nil {
		return nil, "", false, err
	}

	// Collect message IDs that have media types to batch-load attachments
	var msgIDs []uuid.UUID
	var pollMessages []model.Message
	pollIndicesByID := make(map[uuid.UUID]int)
	for i := range items {
		lm := items[i].LastMessage
		if lm == nil {
			continue
		}
		if lm.Type == "poll" {
			pollMessages = append(pollMessages, *lm)
			pollIndicesByID[lm.ID] = i
		}
		if lm.Type == "photo" || lm.Type == "video" || lm.Type == "voice" || lm.Type == "videonote" || lm.Type == "file" || lm.Type == "gif" {
			msgIDs = append(msgIDs, lm.ID)
		}
	}

	if s.polls != nil && len(pollMessages) > 0 {
		if pollErr := s.polls.HydrateMessagePolls(ctx, userID, pollMessages); pollErr != nil {
			slog.Error("failed to hydrate polls for last messages", "error", pollErr)
		} else {
			for i := range pollMessages {
				if idx, ok := pollIndicesByID[pollMessages[i].ID]; ok && items[idx].LastMessage != nil {
					items[idx].LastMessage.Poll = pollMessages[i].Poll
				}
			}
		}
	}

	if len(msgIDs) > 0 {
		mediaMap, mediaErr := s.messages.GetMediaByMessageIDs(ctx, msgIDs)
		if mediaErr != nil {
			slog.Error("failed to load media for last messages", "error", mediaErr)
		} else {
			for i := range items {
				lm := items[i].LastMessage
				if lm == nil {
					continue
				}
				if atts, ok := mediaMap[lm.ID]; ok {
					items[i].LastMessage.MediaAttachments = atts
				}
			}
		}
	}

	return items, nextCursor, hasMore, nil
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

	member, memberErr := s.chats.GetMember(ctx, chatID, userID)
	if memberErr != nil {
		return nil, fmt.Errorf("get member preferences: %w", memberErr)
	}
	if member != nil {
		chat.IsPinned = member.IsPinned
		chat.IsMuted = member.IsMuted
		chat.IsArchived = member.IsArchived
	}

	return chat, nil
}

func (s *ChatService) CreateDirectChat(ctx context.Context, userID, otherUserID uuid.UUID) (*model.Chat, error) {
	if userID == otherUserID {
		return nil, apperror.BadRequest("Cannot create DM with yourself")
	}

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

	if s.nats != nil {
		memberIDs := []string{userID.String(), otherUserID.String()}
		s.nats.Publish(
			fmt.Sprintf("orbit.chat.%s.lifecycle", chat.ID),
			"chat_created",
			chat,
			memberIDs,
			userID.String(),
		)
	}

	// Direct chats are not indexed (IndexChat skips them).
	s.indexChat(chat)

	return chat, nil
}

// CreateChat creates a group chat. memberIDs are added after creation (owner is always added).
func (s *ChatService) CreateChat(ctx context.Context, userID uuid.UUID, chatType, name, description string, memberIDs []uuid.UUID) (*model.Chat, error) {
	if name == "" {
		return nil, apperror.BadRequest("Chat name is required")
	}
	if len(name) > 255 {
		return nil, apperror.BadRequest("Chat name too long (max 255 characters)")
	}
	if len(description) > 2048 {
		return nil, apperror.BadRequest("Description too long (max 2048 characters)")
	}
	if chatType != "group" {
		return nil, apperror.BadRequest("Invalid chat type")
	}

	chat := &model.Chat{
		Type:               chatType,
		Name:               &name,
		Description:        &description,
		CreatedBy:          &userID,
		DefaultPermissions: permissions.DefaultGroupPermissions,
	}

	if err := s.chats.Create(ctx, chat); err != nil {
		return nil, fmt.Errorf("create %s: %w", chatType, err)
	}

	if err := s.chats.AddMember(ctx, chat.ID, userID, "owner"); err != nil {
		return nil, fmt.Errorf("add owner: %w", err)
	}

	if len(memberIDs) > 0 {
		if err := s.chats.AddMembers(ctx, chat.ID, memberIDs, "member"); err != nil {
			slog.Error("add initial members failed", "chatID", chat.ID, "err", err)
		}
	}

	allMemberIDs, err := s.chats.GetMemberIDs(ctx, chat.ID)
	if err != nil {
		slog.Error("get member IDs after create", "chatID", chat.ID, "err", err)
	}

	s.nats.Publish(
		fmt.Sprintf("orbit.chat.%s.lifecycle", chat.ID),
		"chat_created",
		chat,
		allMemberIDs,
		userID.String(),
	)

	s.indexChat(chat)

	return chat, nil
}

func (s *ChatService) UpdateChat(ctx context.Context, chatID, userID uuid.UUID, name, description, avatarURL *string) (*model.Chat, error) {
	if name != nil && len(*name) > 255 {
		return nil, apperror.BadRequest("Chat name too long (max 255 characters)")
	}
	if description != nil && len(*description) > 2048 {
		return nil, apperror.BadRequest("Description too long (max 2048 characters)")
	}

	member, err := s.chats.GetMember(ctx, chatID, userID)
	if err != nil {
		return nil, fmt.Errorf("get member: %w", err)
	}
	if member == nil {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	chat, err := s.chats.GetByID(ctx, chatID)
	if err != nil || chat == nil {
		return nil, apperror.NotFound("Chat not found")
	}

	if !permissions.CanPerform(member.Role, chat.Type, member.Permissions, chat.DefaultPermissions, permissions.CanChangeInfo) {
		return nil, apperror.Forbidden("No permission to edit chat info")
	}

	if err := s.chats.UpdateChat(ctx, chatID, name, description, avatarURL); err != nil {
		return nil, fmt.Errorf("update chat: %w", err)
	}

	chat, err = s.chats.GetByID(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("get updated chat: %w", err)
	}

	memberIDs, mErr := s.chats.GetMemberIDs(ctx, chatID)
	if mErr != nil {
		slog.WarnContext(ctx, "failed to get member IDs for NATS publish", "chat_id", chatID, "error", mErr)
	}
	s.nats.Publish(
		fmt.Sprintf("orbit.chat.%s.lifecycle", chatID),
		"chat_updated",
		chat,
		memberIDs,
		userID.String(),
	)

	s.indexChat(chat)

	return chat, nil
}

func (s *ChatService) ClearChatPhoto(ctx context.Context, chatID, userID uuid.UUID) (*model.Chat, error) {
	member, err := s.chats.GetMember(ctx, chatID, userID)
	if err != nil {
		return nil, fmt.Errorf("get member: %w", err)
	}
	if member == nil {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	chat, err := s.chats.GetByID(ctx, chatID)
	if err != nil || chat == nil {
		return nil, apperror.NotFound("Chat not found")
	}

	if !permissions.CanPerform(member.Role, chat.Type, member.Permissions, chat.DefaultPermissions, permissions.CanChangeInfo) {
		return nil, apperror.Forbidden("No permission to edit chat info")
	}

	if err := s.chats.ClearChatPhoto(ctx, chatID); err != nil {
		return nil, fmt.Errorf("clear chat photo: %w", err)
	}

	chat, err = s.chats.GetByID(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("get updated chat: %w", err)
	}

	memberIDs, mErr := s.chats.GetMemberIDs(ctx, chatID)
	if mErr != nil {
		slog.WarnContext(ctx, "failed to get member IDs for NATS publish", "chat_id", chatID, "error", mErr)
	}
	s.nats.Publish(
		fmt.Sprintf("orbit.chat.%s.lifecycle", chatID),
		"chat_updated",
		chat,
		memberIDs,
		userID.String(),
	)

	s.indexChat(chat)

	return chat, nil
}

func (s *ChatService) DeleteChat(ctx context.Context, chatID, userID uuid.UUID) error {
	_, role, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if role != "owner" {
		return apperror.Forbidden("Only the owner can delete the chat")
	}

	memberIDs, mErr := s.chats.GetMemberIDs(ctx, chatID)
	if mErr != nil {
		slog.WarnContext(ctx, "failed to get member IDs for NATS publish", "chat_id", chatID, "error", mErr)
	}

	if err := s.chats.DeleteChat(ctx, chatID); err != nil {
		return fmt.Errorf("delete chat: %w", err)
	}

	s.nats.Publish(
		fmt.Sprintf("orbit.chat.%s.lifecycle", chatID),
		"chat_deleted",
		map[string]string{"chat_id": chatID.String()},
		memberIDs,
		userID.String(),
	)

	if s.search != nil {
		if err := s.search.DeleteChat(chatID.String()); err != nil {
			slog.WarnContext(ctx, "search: failed to delete chat from index", "chat_id", chatID, "error", err)
		}
	}

	return nil
}

func (s *ChatService) AddMembers(ctx context.Context, chatID, userID uuid.UUID, newMemberIDs []uuid.UUID) error {
	member, err := s.chats.GetMember(ctx, chatID, userID)
	if err != nil {
		return fmt.Errorf("get member: %w", err)
	}
	if member == nil {
		return apperror.Forbidden("Not a member of this chat")
	}

	if !permissions.IsAdminOrOwner(member.Role) && !permissions.Has(member.Permissions, permissions.CanAddMembers) {
		return apperror.Forbidden("No permission to add members")
	}

	if err := s.chats.AddMembers(ctx, chatID, newMemberIDs, "member"); err != nil {
		return fmt.Errorf("add members: %w", err)
	}

	allMemberIDs, mErr := s.chats.GetMemberIDs(ctx, chatID)
	if mErr != nil {
		slog.WarnContext(ctx, "failed to get member IDs for NATS publish", "chat_id", chatID, "error", mErr)
	}

	for _, id := range newMemberIDs {
		s.nats.Publish(
			fmt.Sprintf("orbit.chat.%s.member.added", chatID),
			"chat_member_added",
			map[string]string{"chat_id": chatID.String(), "user_id": id.String()},
			allMemberIDs,
			userID.String(),
		)
	}

	return nil
}

func (s *ChatService) RemoveMember(ctx context.Context, chatID, userID, targetID uuid.UUID) error {
	// Self-leave is allowed only for actual members
	if userID == targetID {
		isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
		if err != nil {
			return fmt.Errorf("check membership: %w", err)
		}
		if !isMember {
			return apperror.Forbidden("Not a member of this chat")
		}
		if err := s.chats.RemoveMember(ctx, chatID, targetID); err != nil {
			return fmt.Errorf("leave chat: %w", err)
		}
		memberIDs, mErr := s.chats.GetMemberIDs(ctx, chatID)
		if mErr != nil {
			slog.WarnContext(ctx, "failed to get member IDs for NATS publish", "chat_id", chatID, "error", mErr)
		}
		s.nats.Publish(
			fmt.Sprintf("orbit.chat.%s.member.removed", chatID),
			"chat_member_removed",
			map[string]string{"chat_id": chatID.String(), "user_id": targetID.String()},
			memberIDs,
			userID.String(),
		)
		return nil
	}

	actor, err := s.chats.GetMember(ctx, chatID, userID)
	if err != nil || actor == nil {
		return apperror.Forbidden("Not a member of this chat")
	}

	target, err := s.chats.GetMember(ctx, chatID, targetID)
	if err != nil || target == nil {
		return apperror.NotFound("Target user is not a member")
	}

	if target.Role == "owner" {
		return apperror.Forbidden("Cannot remove the owner")
	}
	if target.Role == "admin" && actor.Role != "owner" {
		return apperror.Forbidden("Only the owner can remove admins")
	}
	if !permissions.IsAdminOrOwner(actor.Role) && !permissions.Has(actor.Permissions, permissions.CanBanUsers) {
		return apperror.Forbidden("No permission to remove members")
	}

	memberIDs, mErr := s.chats.GetMemberIDs(ctx, chatID)
	if mErr != nil {
		slog.WarnContext(ctx, "failed to get member IDs for NATS publish", "chat_id", chatID, "error", mErr)
	}

	if err := s.chats.RemoveMember(ctx, chatID, targetID); err != nil {
		return fmt.Errorf("remove member: %w", err)
	}

	s.nats.Publish(
		fmt.Sprintf("orbit.chat.%s.member.removed", chatID),
		"chat_member_removed",
		map[string]string{"chat_id": chatID.String(), "user_id": targetID.String()},
		memberIDs,
		userID.String(),
	)

	return nil
}

func (s *ChatService) UpdateMemberRole(ctx context.Context, chatID, userID, targetID uuid.UUID, newRole string, newPerms int64, customTitle *string) error {
	actor, err := s.chats.GetMember(ctx, chatID, userID)
	if err != nil || actor == nil {
		return apperror.Forbidden("Not a member of this chat")
	}

	target, err := s.chats.GetMember(ctx, chatID, targetID)
	if err != nil || target == nil {
		return apperror.NotFound("Target user is not a member")
	}

	if target.Role == "owner" {
		return apperror.Forbidden("Cannot change the owner's role")
	}

	// Promote to admin or demote from admin: owner only
	if newRole == "admin" || target.Role == "admin" {
		if actor.Role != "owner" {
			return apperror.Forbidden("Only the owner can promote or demote admins")
		}
	} else {
		if !permissions.IsAdminOrOwner(actor.Role) && !permissions.Has(actor.Permissions, permissions.CanBanUsers) {
			return apperror.Forbidden("No permission to change member roles")
		}
	}

	// When demoting to member, reset permissions to 0 so chat defaults apply.
	// Only admins/owners should have custom permission overrides.
	if newRole == "member" {
		newPerms = 0
	}

	if err := s.chats.UpdateMemberRole(ctx, chatID, targetID, newRole, newPerms, customTitle); err != nil {
		return fmt.Errorf("update member role: %w", err)
	}

	memberIDs, mErr := s.chats.GetMemberIDs(ctx, chatID)
	if mErr != nil {
		slog.WarnContext(ctx, "failed to get member IDs for NATS publish", "chat_id", chatID, "error", mErr)
	}
	s.nats.Publish(
		fmt.Sprintf("orbit.chat.%s.member.updated", chatID),
		"chat_member_updated",
		map[string]string{"chat_id": chatID.String(), "user_id": targetID.String(), "role": newRole},
		memberIDs,
		userID.String(),
	)

	return nil
}

func (s *ChatService) UpdateDefaultPermissions(ctx context.Context, chatID, userID uuid.UUID, perms int64) error {
	actor, err := s.chats.GetMember(ctx, chatID, userID)
	if err != nil || actor == nil {
		return apperror.Forbidden("Not a member of this chat")
	}

	if !permissions.IsAdminOrOwner(actor.Role) {
		return apperror.Forbidden("No permission to change default permissions")
	}

	if err := s.chats.UpdateDefaultPermissions(ctx, chatID, perms); err != nil {
		return fmt.Errorf("update default permissions: %w", err)
	}

	memberIDs, mErr := s.chats.GetMemberIDs(ctx, chatID)
	if mErr != nil {
		slog.WarnContext(ctx, "failed to get member IDs for NATS publish", "chat_id", chatID, "error", mErr)
	}
	s.nats.Publish(
		fmt.Sprintf("orbit.chat.%s.lifecycle", chatID),
		"chat_updated",
		map[string]interface{}{"chat_id": chatID.String(), "default_permissions": perms},
		memberIDs,
		userID.String(),
	)

	return nil
}

func (s *ChatService) UpdateMemberPermissions(ctx context.Context, chatID, userID, targetID uuid.UUID, perms int64) error {
	actor, err := s.chats.GetMember(ctx, chatID, userID)
	if err != nil || actor == nil {
		return apperror.Forbidden("Not a member of this chat")
	}

	target, err := s.chats.GetMember(ctx, chatID, targetID)
	if err != nil || target == nil {
		return apperror.NotFound("Target user is not a member")
	}

	if target.Role == "owner" {
		return apperror.Forbidden("Cannot change the owner's permissions")
	}

	if !permissions.IsAdminOrOwner(actor.Role) {
		return apperror.Forbidden("No permission to change member permissions")
	}

	if err := s.chats.UpdateMemberPermissions(ctx, chatID, targetID, perms); err != nil {
		return fmt.Errorf("update member permissions: %w", err)
	}

	memberIDs, mErr := s.chats.GetMemberIDs(ctx, chatID)
	if mErr != nil {
		slog.WarnContext(ctx, "failed to get member IDs for NATS publish", "chat_id", chatID, "error", mErr)
	}
	s.nats.Publish(
		fmt.Sprintf("orbit.chat.%s.member.updated", chatID),
		"chat_member_updated",
		map[string]interface{}{"chat_id": chatID.String(), "user_id": targetID.String(), "permissions": perms},
		memberIDs,
		userID.String(),
	)

	return nil
}

func (s *ChatService) UpdateMemberPreferences(
	ctx context.Context,
	chatID, userID uuid.UUID,
	prefs model.ChatMemberPreferences,
) (*model.ChatMember, error) {
	if prefs.IsPinned == nil && prefs.IsMuted == nil && prefs.IsArchived == nil {
		return nil, apperror.BadRequest("At least one preference must be provided")
	}

	member, err := s.chats.GetMember(ctx, chatID, userID)
	if err != nil {
		return nil, fmt.Errorf("get member: %w", err)
	}
	if member == nil {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	updated, err := s.chats.UpdateMemberPreferences(ctx, chatID, userID, prefs)
	if err != nil {
		return nil, fmt.Errorf("update member preferences: %w", err)
	}
	if updated == nil {
		return nil, apperror.NotFound("Member not found")
	}

	return updated, nil
}

func (s *ChatService) SetSlowMode(ctx context.Context, chatID, userID uuid.UUID, seconds int) error {
	if seconds < 0 || seconds > 86400 {
		return apperror.BadRequest("Slow mode seconds must be between 0 and 86400 (24 hours)")
	}

	actor, err := s.chats.GetMember(ctx, chatID, userID)
	if err != nil || actor == nil {
		return apperror.Forbidden("Not a member of this chat")
	}

	if !permissions.IsAdminOrOwner(actor.Role) {
		return apperror.Forbidden("Only admins and owners can set slow mode")
	}

	if err := s.chats.SetSlowMode(ctx, chatID, seconds); err != nil {
		return fmt.Errorf("set slow mode: %w", err)
	}

	memberIDs, mErr := s.chats.GetMemberIDs(ctx, chatID)
	if mErr != nil {
		slog.WarnContext(ctx, "failed to get member IDs for NATS publish", "chat_id", chatID, "error", mErr)
	}
	s.nats.Publish(
		fmt.Sprintf("orbit.chat.%s.lifecycle", chatID),
		"chat_updated",
		map[string]interface{}{"chat_id": chatID.String(), "slow_mode_seconds": seconds},
		memberIDs,
		userID.String(),
	)

	return nil
}

func (s *ChatService) GetAdmins(ctx context.Context, chatID, userID uuid.UUID) ([]model.ChatMember, error) {
	isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	admins, err := s.chats.GetAdmins(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("get admins: %w", err)
	}
	return admins, nil
}

func (s *ChatService) GetMember(ctx context.Context, chatID, userID, targetID uuid.UUID) (*model.ChatMember, error) {
	isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	member, err := s.chats.GetMember(ctx, chatID, targetID)
	if err != nil {
		return nil, fmt.Errorf("get member: %w", err)
	}
	if member == nil {
		return nil, apperror.NotFound("Member not found")
	}
	return member, nil
}

func (s *ChatService) SearchMembers(ctx context.Context, chatID, userID uuid.UUID, query string, limit int) ([]model.ChatMember, error) {
	isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	members, err := s.chats.SearchMembers(ctx, chatID, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search members: %w", err)
	}
	return members, nil
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

func (s *ChatService) GetCommonChats(ctx context.Context, userA, userB uuid.UUID, limit int) ([]model.Chat, error) {
	return s.chats.GetCommonChats(ctx, userA, userB, limit)
}

func (s *ChatService) GetOrCreateSavedChat(ctx context.Context, userID uuid.UUID) (*model.Chat, error) {
	return s.chats.GetOrCreateSavedChat(ctx, userID)
}

// indexChat upserts a chat document into the search index. Non-fatal — logs on error.
func (s *ChatService) indexChat(chat *model.Chat) {
	if s.search == nil || chat == nil {
		return
	}
	name := ""
	if chat.Name != nil {
		name = *chat.Name
	}
	description := ""
	if chat.Description != nil {
		description = *chat.Description
	}
	if err := s.search.IndexChat(chat.ID.String(), chat.Type, name, description); err != nil {
		slog.Warn("search: failed to index chat", "chat_id", chat.ID, "error", err)
	}
}
