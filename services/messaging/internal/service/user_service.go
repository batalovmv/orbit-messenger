package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

type UserService struct {
	users   store.UserStore
	chats   store.ChatStore
	privacy store.PrivacySettingsStore
}

func NewUserService(users store.UserStore, chats store.ChatStore, privacy store.PrivacySettingsStore) *UserService {
	return &UserService{users: users, chats: chats, privacy: privacy}
}

func (s *UserService) GetContactIDs(ctx context.Context, userID uuid.UUID) ([]string, error) {
	return s.chats.GetContactIDs(ctx, userID)
}

func (s *UserService) GetMe(ctx context.Context, userID uuid.UUID) (*model.User, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		return nil, apperror.NotFound("User not found")
	}
	return u, nil
}

func (s *UserService) GetUser(ctx context.Context, userID uuid.UUID) (*model.User, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		return nil, apperror.NotFound("User not found")
	}
	return u, nil
}

// GetUserForViewer returns a user profile with privacy settings applied.
// Fields are stripped based on the target's privacy preferences and
// whether the viewer is a "contact" (shares a direct chat with the target).
func (s *UserService) GetUserForViewer(ctx context.Context, viewerID, targetID uuid.UUID) (*model.User, error) {
	u, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		return nil, apperror.NotFound("User not found")
	}

	// Own profile — return everything
	if viewerID == targetID {
		return u, nil
	}

	// Strip PII that is never shown to others
	u.Email = ""
	u.Phone = nil

	// Fetch privacy settings
	ps, err := s.privacy.GetByUserID(ctx, targetID)
	if err != nil {
		return nil, fmt.Errorf("get privacy settings: %w", err)
	}

	// Determine if viewer is a "contact" (has a direct chat with target)
	isContact := false
	if ps.LastSeen == "contacts" || ps.Avatar == "contacts" || ps.Phone == "contacts" {
		dmID, _ := s.chats.GetDirectChat(ctx, viewerID, targetID)
		isContact = dmID != nil
	}

	// Apply last_seen privacy
	if ps.LastSeen == "nobody" || (ps.LastSeen == "contacts" && !isContact) {
		u.LastSeenAt = nil
		u.Status = ""
	}

	// Apply avatar privacy
	if ps.Avatar == "nobody" || (ps.Avatar == "contacts" && !isContact) {
		u.AvatarURL = nil
	}

	// Apply phone privacy
	if ps.Phone == "nobody" || (ps.Phone == "contacts" && !isContact) {
		u.Phone = nil
	}

	return u, nil
}

func (s *UserService) UpdateProfile(ctx context.Context, userID uuid.UUID, displayName string, bio, phone, avatarURL, customStatus, customStatusEmoji *string) (*model.User, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		return nil, apperror.NotFound("User not found")
	}

	if displayName != "" {
		u.DisplayName = displayName
	}
	if bio != nil {
		u.Bio = bio
	}
	if phone != nil {
		u.Phone = phone
	}
	if avatarURL != nil {
		u.AvatarURL = avatarURL
	}
	if customStatus != nil {
		u.CustomStatus = customStatus
	}
	if customStatusEmoji != nil {
		u.CustomStatusEmoji = customStatusEmoji
	}

	if err := s.users.Update(ctx, u); err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}
	return u, nil
}

func (s *UserService) SearchUsers(ctx context.Context, query string, limit int) ([]model.User, error) {
	if query == "" {
		return s.users.ListAll(ctx, limit)
	}
	return s.users.Search(ctx, query, limit)
}
