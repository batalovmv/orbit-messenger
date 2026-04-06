package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

// userSearchIndexer is the minimal interface needed to update the user search index.
type userSearchIndexer interface {
	IndexUser(userID, displayName, email, role string) error
}

type UserService struct {
	users   store.UserStore
	chats   store.ChatStore
	privacy store.PrivacySettingsStore
	search  userSearchIndexer
}

func NewUserService(users store.UserStore, chats store.ChatStore, privacy store.PrivacySettingsStore, search ...userSearchIndexer) *UserService {
	svc := &UserService{users: users, chats: chats, privacy: privacy}
	if len(search) > 0 {
		svc.search = search[0]
	}
	return svc
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

func (s *UserService) UpdateProfile(ctx context.Context, userID uuid.UUID, displayName *string, bio, phone, avatarURL, customStatus, customStatusEmoji *string) (*model.User, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		return nil, apperror.NotFound("User not found")
	}

	if displayName != nil {
		u.DisplayName = *displayName
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

	if s.search != nil {
		if indexErr := s.search.IndexUser(u.ID.String(), u.DisplayName, u.Email, u.Role); indexErr != nil {
			// Non-fatal: search index is eventually consistent.
			slog.WarnContext(ctx, "search: failed to index user", "user_id", u.ID, "error", indexErr)
		}
	}

	return u, nil
}

func (s *UserService) SearchUsers(ctx context.Context, viewerID uuid.UUID, query string, limit int) ([]model.User, error) {
	var users []model.User
	var err error
	if query == "" {
		users, err = s.users.ListAll(ctx, limit)
	} else {
		users, err = s.users.Search(ctx, query, limit)
	}
	if err != nil {
		return nil, err
	}
	if err := s.applyPrivacyToList(ctx, viewerID, users); err != nil {
		return nil, err
	}
	return users, nil
}

// applyPrivacyToList strips restricted fields from each user in the list
// based on that user's privacy settings and whether the viewer is a contact.
func (s *UserService) applyPrivacyToList(ctx context.Context, viewerID uuid.UUID, users []model.User) error {
	if len(users) == 0 {
		return nil
	}

	// Collect IDs, skip self (self sees own full profile via GetMe)
	ids := make([]uuid.UUID, 0, len(users))
	for _, u := range users {
		if u.ID != viewerID {
			ids = append(ids, u.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	privacyMap, err := s.privacy.GetByUserIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("get privacy settings: %w", err)
	}

	// Determine contact set: fetch all DM partner IDs of viewerID in one call
	contactIDs, err := s.chats.GetContactIDs(ctx, viewerID)
	if err != nil {
		return fmt.Errorf("get contact ids: %w", err)
	}
	contactSet := make(map[uuid.UUID]bool, len(contactIDs))
	for _, id := range contactIDs {
		if parsed, err := uuid.Parse(id); err == nil {
			contactSet[parsed] = true
		}
	}

	for i := range users {
		u := &users[i]
		if u.ID == viewerID {
			continue
		}
		// Email is never exposed to others
		u.Email = ""

		ps, ok := privacyMap[u.ID]
		if !ok {
			// No privacy row → defaults: phone=contacts, rest=everyone
			u.Phone = nil
			continue
		}

		isContact := contactSet[u.ID]

		if ps.LastSeen == "nobody" || (ps.LastSeen == "contacts" && !isContact) {
			u.LastSeenAt = nil
			u.Status = ""
		}
		if ps.Avatar == "nobody" || (ps.Avatar == "contacts" && !isContact) {
			u.AvatarURL = nil
		}
		if ps.Phone == "nobody" || (ps.Phone == "contacts" && !isContact) {
			u.Phone = nil
		}
	}

	return nil
}
