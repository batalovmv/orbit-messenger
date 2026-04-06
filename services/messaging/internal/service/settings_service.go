package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

// SettingsService handles privacy settings, user settings, notification settings, and blocked users.
type SettingsService struct {
	privacyStore      store.PrivacySettingsStore
	blockedStore      store.BlockedUsersStore
	userSettingsStore store.UserSettingsStore
	notifStore        store.NotificationSettingsStore
	chatStore         store.ChatStore
}

// NewSettingsService creates a new SettingsService.
func NewSettingsService(
	privacyStore store.PrivacySettingsStore,
	blockedStore store.BlockedUsersStore,
	userSettingsStore store.UserSettingsStore,
	notifStore store.NotificationSettingsStore,
	chatStore store.ChatStore,
) *SettingsService {
	return &SettingsService{
		privacyStore:      privacyStore,
		blockedStore:      blockedStore,
		userSettingsStore: userSettingsStore,
		notifStore:        notifStore,
		chatStore:         chatStore,
	}
}

// — Privacy Settings —

// GetPrivacySettings returns the privacy settings for a user.
func (s *SettingsService) GetPrivacySettings(ctx context.Context, userID uuid.UUID) (*model.PrivacySettings, error) {
	ps, err := s.privacyStore.GetByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get privacy settings: %w", err)
	}
	return ps, nil
}

// UpdatePrivacySettings validates and persists privacy settings for a user.
func (s *SettingsService) UpdatePrivacySettings(
	ctx context.Context,
	userID uuid.UUID,
	lastSeen, avatar, phone, calls, groups, forwarded string,
) (*model.PrivacySettings, error) {
	fields := map[string]string{
		"last_seen": lastSeen,
		"avatar":    avatar,
		"phone":     phone,
		"calls":     calls,
		"groups":    groups,
		"forwarded": forwarded,
	}
	for field, val := range fields {
		if !model.ValidPrivacyValues[val] {
			return nil, apperror.BadRequest(fmt.Sprintf("invalid value %q for field %s", val, field))
		}
	}

	ps := &model.PrivacySettings{
		UserID:    userID,
		LastSeen:  lastSeen,
		Avatar:    avatar,
		Phone:     phone,
		Calls:     calls,
		Groups:    groups,
		Forwarded: forwarded,
	}
	if err := s.privacyStore.Upsert(ctx, ps); err != nil {
		return nil, fmt.Errorf("upsert privacy settings: %w", err)
	}
	return ps, nil
}

// — Blocked Users —

// ListBlockedUsers returns the list of users blocked by userID.
func (s *SettingsService) ListBlockedUsers(ctx context.Context, userID uuid.UUID, limit int) ([]model.BlockedUser, error) {
	users, err := s.blockedStore.List(ctx, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list blocked users: %w", err)
	}
	return users, nil
}

// IsBlocked reports whether targetID has been blocked by userID.
func (s *SettingsService) IsBlocked(ctx context.Context, userID, targetID uuid.UUID) (bool, error) {
	blocked, err := s.blockedStore.IsBlocked(ctx, userID, targetID)
	if err != nil {
		return false, fmt.Errorf("check blocked: %w", err)
	}
	return blocked, nil
}

// BlockUser blocks targetID for userID.
func (s *SettingsService) BlockUser(ctx context.Context, userID, targetID uuid.UUID) error {
	if userID == targetID {
		return apperror.BadRequest("cannot block yourself")
	}
	if err := s.blockedStore.Block(ctx, userID, targetID); err != nil {
		return fmt.Errorf("block user: %w", err)
	}
	return nil
}

// UnblockUser removes a block placed by userID on targetID.
func (s *SettingsService) UnblockUser(ctx context.Context, userID, targetID uuid.UUID) error {
	if err := s.blockedStore.Unblock(ctx, userID, targetID); err != nil {
		return fmt.Errorf("unblock user: %w", err)
	}
	return nil
}

// — User Settings —

// GetUserSettings returns the application settings for a user.
func (s *SettingsService) GetUserSettings(ctx context.Context, userID uuid.UUID) (*model.UserSettings, error) {
	us, err := s.userSettingsStore.GetByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user settings: %w", err)
	}
	return us, nil
}

// UpdateUserSettings validates and persists application settings for a user.
func (s *SettingsService) UpdateUserSettings(
	ctx context.Context,
	userID uuid.UUID,
	theme, language string,
	fontSize int,
	sendByEnter bool,
	dndFrom, dndUntil *string,
) (*model.UserSettings, error) {
	if !model.ValidThemes[theme] {
		return nil, apperror.BadRequest(fmt.Sprintf("invalid theme %q", theme))
	}
	if fontSize < 10 || fontSize > 30 {
		return nil, apperror.BadRequest("font_size must be between 10 and 30")
	}
	if l := len(language); l < 2 || l > 10 {
		return nil, apperror.BadRequest("language must be 2–10 characters")
	}

	us := &model.UserSettings{
		UserID:      userID,
		Theme:       theme,
		Language:    language,
		FontSize:    fontSize,
		SendByEnter: sendByEnter,
		DNDFrom:     dndFrom,
		DNDUntil:    dndUntil,
	}
	if err := s.userSettingsStore.Upsert(ctx, us); err != nil {
		return nil, fmt.Errorf("upsert user settings: %w", err)
	}
	return us, nil
}

// — Notification Settings —

// requireChatMembership verifies that userID is a member of chatID.
func (s *SettingsService) requireChatMembership(ctx context.Context, userID, chatID uuid.UUID) error {
	isMember, _, err := s.chatStore.IsMember(ctx, chatID, userID)
	if err != nil {
		return fmt.Errorf("check chat membership: %w", err)
	}
	if !isMember {
		return apperror.Forbidden("not a member of this chat")
	}
	return nil
}

// GetNotificationSettings returns per-chat notification settings for a user.
func (s *SettingsService) GetNotificationSettings(ctx context.Context, userID, chatID uuid.UUID) (*model.NotificationSettings, error) {
	if err := s.requireChatMembership(ctx, userID, chatID); err != nil {
		return nil, err
	}
	ns, err := s.notifStore.Get(ctx, userID, chatID)
	if err != nil {
		return nil, fmt.Errorf("get notification settings: %w", err)
	}
	return ns, nil
}

// UpdateNotificationSettings persists per-chat notification settings for a user.
func (s *SettingsService) UpdateNotificationSettings(
	ctx context.Context,
	userID, chatID uuid.UUID,
	mutedUntil *time.Time,
	sound string,
	showPreview bool,
) (*model.NotificationSettings, error) {
	if err := s.requireChatMembership(ctx, userID, chatID); err != nil {
		return nil, err
	}
	ns := &model.NotificationSettings{
		UserID:      userID,
		ChatID:      chatID,
		MutedUntil:  mutedUntil,
		Sound:       sound,
		ShowPreview: showPreview,
	}
	if err := s.notifStore.Upsert(ctx, ns); err != nil {
		return nil, fmt.Errorf("upsert notification settings: %w", err)
	}
	return ns, nil
}

// DeleteNotificationSettings removes per-chat notification overrides for a user,
// restoring default notification behaviour for that chat.
func (s *SettingsService) DeleteNotificationSettings(ctx context.Context, userID, chatID uuid.UUID) error {
	if err := s.requireChatMembership(ctx, userID, chatID); err != nil {
		return err
	}
	if err := s.notifStore.Delete(ctx, userID, chatID); err != nil {
		return fmt.Errorf("delete notification settings: %w", err)
	}
	return nil
}

// GetMutedUserIDs returns the subset of user IDs that currently have chat notifications muted.
func (s *SettingsService) GetMutedUserIDs(ctx context.Context, chatID uuid.UUID, userIDs []string) ([]string, error) {
	mutedUserIDs, err := s.notifStore.ListMutedUserIDs(ctx, chatID, userIDs)
	if err != nil {
		return nil, fmt.Errorf("list muted user ids: %w", err)
	}

	return mutedUserIDs, nil
}

func (s *SettingsService) ListNotificationExceptions(ctx context.Context, userID uuid.UUID) ([]model.NotificationSettings, error) {
	return s.notifStore.ListByUser(ctx, userID)
}

func (s *SettingsService) GetGlobalNotifySettings(ctx context.Context, userID uuid.UUID) (*model.GlobalNotifySettings, error) {
	return s.userSettingsStore.GetGlobalNotifySettings(ctx, userID)
}

func (s *SettingsService) UpdateGlobalNotifySettings(ctx context.Context, userID uuid.UUID, gs *model.GlobalNotifySettings) error {
	return s.userSettingsStore.UpdateGlobalNotifySettings(ctx, userID, gs)
}
