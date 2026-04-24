// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

// ─── PrivacySettingsStore ────────────────────────────────────────────────────

type PrivacySettingsStore interface {
	GetByUserID(ctx context.Context, userID uuid.UUID) (*model.PrivacySettings, error)
	GetByUserIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]*model.PrivacySettings, error)
	Upsert(ctx context.Context, settings *model.PrivacySettings) error
}

type privacySettingsStore struct {
	pool *pgxpool.Pool
}

func NewPrivacySettingsStore(pool *pgxpool.Pool) PrivacySettingsStore {
	return &privacySettingsStore{pool: pool}
}

func (s *privacySettingsStore) GetByUserID(ctx context.Context, userID uuid.UUID) (*model.PrivacySettings, error) {
	ps := &model.PrivacySettings{}
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, last_seen, avatar, phone, calls, groups, forwarded, created_at, updated_at
		 FROM privacy_settings
		 WHERE user_id = $1`,
		userID,
	).Scan(
		&ps.UserID, &ps.LastSeen, &ps.Avatar, &ps.Phone,
		&ps.Calls, &ps.Groups, &ps.Forwarded,
		&ps.CreatedAt, &ps.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		now := time.Now()
		return &model.PrivacySettings{
			UserID:    userID,
			LastSeen:  "everyone",
			Avatar:    "everyone",
			Phone:     "contacts",
			Calls:     "everyone",
			Groups:    "everyone",
			Forwarded: "everyone",
			CreatedAt: now,
			UpdatedAt: now,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("privacySettingsStore.GetByUserID: %w", err)
	}
	return ps, nil
}

func (s *privacySettingsStore) GetByUserIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]*model.PrivacySettings, error) {
	result := make(map[uuid.UUID]*model.PrivacySettings, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}

	rows, err := s.pool.Query(ctx,
		`SELECT user_id, last_seen, avatar, phone, calls, groups, forwarded, created_at, updated_at
		 FROM privacy_settings
		 WHERE user_id = ANY($1)`,
		userIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("privacySettingsStore.GetByUserIDs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		ps := &model.PrivacySettings{}
		if err := rows.Scan(
			&ps.UserID, &ps.LastSeen, &ps.Avatar, &ps.Phone,
			&ps.Calls, &ps.Groups, &ps.Forwarded,
			&ps.CreatedAt, &ps.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("privacySettingsStore.GetByUserIDs scan: %w", err)
		}
		result[ps.UserID] = ps
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("privacySettingsStore.GetByUserIDs rows: %w", err)
	}

	// Fill defaults for users with no row in privacy_settings
	now := time.Now()
	for _, uid := range userIDs {
		if _, ok := result[uid]; !ok {
			result[uid] = &model.PrivacySettings{
				UserID:    uid,
				LastSeen:  "everyone",
				Avatar:    "everyone",
				Phone:     "contacts",
				Calls:     "everyone",
				Groups:    "everyone",
				Forwarded: "everyone",
				CreatedAt: now,
				UpdatedAt: now,
			}
		}
	}

	return result, nil
}

func (s *privacySettingsStore) Upsert(ctx context.Context, settings *model.PrivacySettings) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO privacy_settings (user_id, last_seen, avatar, phone, calls, groups, forwarded, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
		 ON CONFLICT (user_id) DO UPDATE SET
		   last_seen  = EXCLUDED.last_seen,
		   avatar     = EXCLUDED.avatar,
		   phone      = EXCLUDED.phone,
		   calls      = EXCLUDED.calls,
		   groups     = EXCLUDED.groups,
		   forwarded  = EXCLUDED.forwarded,
		   updated_at = NOW()
		 RETURNING created_at, updated_at`,
		settings.UserID, settings.LastSeen, settings.Avatar, settings.Phone,
		settings.Calls, settings.Groups, settings.Forwarded,
	).Scan(&settings.CreatedAt, &settings.UpdatedAt)
}

// ─── BlockedUsersStore ───────────────────────────────────────────────────────

type BlockedUsersStore interface {
	List(ctx context.Context, userID uuid.UUID, limit int) ([]model.BlockedUser, error)
	IsBlocked(ctx context.Context, userID, targetID uuid.UUID) (bool, error)
	Block(ctx context.Context, userID, blockedID uuid.UUID) error
	Unblock(ctx context.Context, userID, blockedID uuid.UUID) error
}

type blockedUsersStore struct {
	pool *pgxpool.Pool
}

func NewBlockedUsersStore(pool *pgxpool.Pool) BlockedUsersStore {
	return &blockedUsersStore{pool: pool}
}

func (s *blockedUsersStore) List(ctx context.Context, userID uuid.UUID, limit int) ([]model.BlockedUser, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx,
		`SELECT b.user_id, b.blocked_user_id, b.created_at,
		        u.display_name, u.avatar_url
		 FROM blocked_users b
		 JOIN users u ON u.id = b.blocked_user_id
		 WHERE b.user_id = $1
		 ORDER BY b.created_at DESC
		 LIMIT $2`,
		userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("blockedUsersStore.List: %w", err)
	}
	defer rows.Close()

	var result []model.BlockedUser
	for rows.Next() {
		var bu model.BlockedUser
		if err := rows.Scan(
			&bu.UserID, &bu.BlockedUserID, &bu.CreatedAt,
			&bu.DisplayName, &bu.AvatarURL,
		); err != nil {
			return nil, fmt.Errorf("blockedUsersStore.List scan: %w", err)
		}
		result = append(result, bu)
	}
	return result, rows.Err()
}

func (s *blockedUsersStore) IsBlocked(ctx context.Context, userID, targetID uuid.UUID) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(
		   SELECT 1 FROM blocked_users
		   WHERE user_id = $1 AND blocked_user_id = $2
		 )`,
		userID, targetID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("blockedUsersStore.IsBlocked: %w", err)
	}
	return exists, nil
}

func (s *blockedUsersStore) Block(ctx context.Context, userID, blockedID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO blocked_users (user_id, blocked_user_id, created_at)
		 VALUES ($1, $2, NOW())`,
		userID, blockedID,
	)
	if err != nil {
		// pgx surfaces unique-violation as a pgconn.PgError with Code "23505"
		if isPgUniqueViolation(err) {
			return apperror.Conflict("user is already blocked")
		}
		return fmt.Errorf("blockedUsersStore.Block: %w", err)
	}
	return nil
}

func (s *blockedUsersStore) Unblock(ctx context.Context, userID, blockedID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM blocked_users
		 WHERE user_id = $1 AND blocked_user_id = $2`,
		userID, blockedID,
	)
	if err != nil {
		return fmt.Errorf("blockedUsersStore.Unblock: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NotFound("blocked user not found")
	}
	return nil
}

// ─── UserSettingsStore ───────────────────────────────────────────────────────

type UserSettingsStore interface {
	GetByUserID(ctx context.Context, userID uuid.UUID) (*model.UserSettings, error)
	Upsert(ctx context.Context, settings *model.UserSettings) error
	GetGlobalNotifySettings(ctx context.Context, userID uuid.UUID) (*model.GlobalNotifySettings, error)
	UpdateGlobalNotifySettings(ctx context.Context, userID uuid.UUID, s *model.GlobalNotifySettings) error
}

type userSettingsStore struct {
	pool *pgxpool.Pool
}

func NewUserSettingsStore(pool *pgxpool.Pool) UserSettingsStore {
	return &userSettingsStore{pool: pool}
}

func (s *userSettingsStore) GetByUserID(ctx context.Context, userID uuid.UUID) (*model.UserSettings, error) {
	us := &model.UserSettings{}
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, theme, language, font_size, send_by_enter, dnd_from, dnd_until, default_translate_lang, can_translate, can_translate_chats, created_at, updated_at, notify_users_muted, notify_groups_muted, notify_users_preview, notify_groups_preview FROM user_settings WHERE user_id = $1`,
		userID,
	).Scan(
		&us.UserID, &us.Theme, &us.Language, &us.FontSize, &us.SendByEnter,
		&us.DNDFrom, &us.DNDUntil, &us.DefaultTranslateLang,
		&us.CanTranslate, &us.CanTranslateChats,
		&us.CreatedAt, &us.UpdatedAt,
		&us.NotifyUsersMuted, &us.NotifyGroupsMuted,
		&us.NotifyUsersPreview, &us.NotifyGroupsPreview,
	)
	if err == pgx.ErrNoRows {
		now := time.Now()
		return &model.UserSettings{
			UserID:              userID,
			Theme:               "auto",
			Language:            "ru",
			FontSize:            16,
			SendByEnter:         true,
			NotifyUsersPreview:  true,
			NotifyGroupsPreview: true,
			CreatedAt:           now,
			UpdatedAt:           now,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("userSettingsStore.GetByUserID: %w", err)
	}
	return us, nil
}

func (s *userSettingsStore) Upsert(ctx context.Context, settings *model.UserSettings) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_settings (user_id, theme, language, font_size, send_by_enter, dnd_from, dnd_until, default_translate_lang, can_translate, can_translate_chats, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW()) ON CONFLICT (user_id) DO UPDATE SET theme = EXCLUDED.theme, language = EXCLUDED.language, font_size = EXCLUDED.font_size, send_by_enter = EXCLUDED.send_by_enter, dnd_from = EXCLUDED.dnd_from, dnd_until = EXCLUDED.dnd_until, default_translate_lang = EXCLUDED.default_translate_lang, can_translate = EXCLUDED.can_translate, can_translate_chats = EXCLUDED.can_translate_chats, updated_at = NOW()`,
		settings.UserID, settings.Theme, settings.Language, settings.FontSize, settings.SendByEnter,
		settings.DNDFrom, settings.DNDUntil, settings.DefaultTranslateLang,
		settings.CanTranslate, settings.CanTranslateChats,
	)
	if err != nil {
		return fmt.Errorf("userSettingsStore.Upsert: %w", err)
	}
	return nil
}

func (s *userSettingsStore) GetGlobalNotifySettings(ctx context.Context, userID uuid.UUID) (*model.GlobalNotifySettings, error) {
	gs := &model.GlobalNotifySettings{
		UsersPreview:  true,
		GroupsPreview: true,
	}
	err := s.pool.QueryRow(ctx,
		`SELECT notify_users_muted, notify_groups_muted,
		        notify_users_preview, notify_groups_preview
		 FROM user_settings WHERE user_id = $1`,
		userID,
	).Scan(
		&gs.UsersMuted, &gs.GroupsMuted,
		&gs.UsersPreview, &gs.GroupsPreview,
	)
	if err == pgx.ErrNoRows {
		return gs, nil
	}
	if err != nil {
		return nil, fmt.Errorf("userSettingsStore.GetGlobalNotifySettings: %w", err)
	}
	return gs, nil
}

func (s *userSettingsStore) UpdateGlobalNotifySettings(ctx context.Context, userID uuid.UUID, gs *model.GlobalNotifySettings) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_settings (user_id, notify_users_muted, notify_groups_muted,
		                            notify_users_preview, notify_groups_preview,
		                            created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		 ON CONFLICT (user_id) DO UPDATE SET
		   notify_users_muted   = EXCLUDED.notify_users_muted,
		   notify_groups_muted  = EXCLUDED.notify_groups_muted,
		   notify_users_preview = EXCLUDED.notify_users_preview,
		   notify_groups_preview = EXCLUDED.notify_groups_preview,
		   updated_at           = NOW()`,
		userID, gs.UsersMuted, gs.GroupsMuted,
		gs.UsersPreview, gs.GroupsPreview,
	)
	if err != nil {
		return fmt.Errorf("userSettingsStore.UpdateGlobalNotifySettings: %w", err)
	}
	return nil
}

// ─── NotificationSettingsStore ───────────────────────────────────────────────

type NotificationSettingsStore interface {
	Get(ctx context.Context, userID, chatID uuid.UUID) (*model.NotificationSettings, error)
	Upsert(ctx context.Context, settings *model.NotificationSettings) error
	Delete(ctx context.Context, userID, chatID uuid.UUID) error
	ListMutedUserIDs(ctx context.Context, chatID uuid.UUID, userIDs []string) ([]string, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]model.NotificationSettings, error)
}

type notificationSettingsStore struct {
	pool *pgxpool.Pool
}

func NewNotificationSettingsStore(pool *pgxpool.Pool) NotificationSettingsStore {
	return &notificationSettingsStore{pool: pool}
}

func (s *notificationSettingsStore) Get(ctx context.Context, userID, chatID uuid.UUID) (*model.NotificationSettings, error) {
	ns := &model.NotificationSettings{}
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, chat_id, muted_until, sound, show_preview
		 FROM notification_settings
		 WHERE user_id = $1 AND chat_id = $2`,
		userID, chatID,
	).Scan(&ns.UserID, &ns.ChatID, &ns.MutedUntil, &ns.Sound, &ns.ShowPreview)
	if err == pgx.ErrNoRows {
		return nil, apperror.NotFound("notification settings not found")
	}
	if err != nil {
		return nil, fmt.Errorf("notificationSettingsStore.Get: %w", err)
	}
	return ns, nil
}

func (s *notificationSettingsStore) Upsert(ctx context.Context, settings *model.NotificationSettings) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO notification_settings (user_id, chat_id, muted_until, sound, show_preview)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (user_id, chat_id) DO UPDATE SET
		   muted_until  = EXCLUDED.muted_until,
		   sound        = EXCLUDED.sound,
		   show_preview = EXCLUDED.show_preview`,
		settings.UserID, settings.ChatID, settings.MutedUntil,
		settings.Sound, settings.ShowPreview,
	)
	if err != nil {
		return fmt.Errorf("notificationSettingsStore.Upsert: %w", err)
	}
	return nil
}

func (s *notificationSettingsStore) Delete(ctx context.Context, userID, chatID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM notification_settings
		 WHERE user_id = $1 AND chat_id = $2`,
		userID, chatID,
	)
	if err != nil {
		return fmt.Errorf("notificationSettingsStore.Delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NotFound("notification settings not found")
	}
	return nil
}

func (s *notificationSettingsStore) ListMutedUserIDs(ctx context.Context, chatID uuid.UUID, userIDs []string) ([]string, error) {
	if len(userIDs) == 0 {
		return []string{}, nil
	}

	rows, err := s.pool.Query(ctx,
		`SELECT user_id::text
		 FROM notification_settings
		 WHERE chat_id = $1
		   AND user_id = ANY($2::uuid[])
		   AND muted_until IS NOT NULL
		   AND muted_until > NOW()`,
		chatID, userIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("notificationSettingsStore.ListMutedUserIDs: %w", err)
	}
	defer rows.Close()

	mutedUserIDs := make([]string, 0, len(userIDs))
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return nil, fmt.Errorf("notificationSettingsStore.ListMutedUserIDs scan: %w", err)
		}
		mutedUserIDs = append(mutedUserIDs, userID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notificationSettingsStore.ListMutedUserIDs rows: %w", err)
	}

	return mutedUserIDs, nil
}

func (s *notificationSettingsStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]model.NotificationSettings, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_id, chat_id, muted_until, sound, show_preview
		 FROM notification_settings
		 WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("notificationSettingsStore.ListByUser: %w", err)
	}
	defer rows.Close()

	var result []model.NotificationSettings
	for rows.Next() {
		var ns model.NotificationSettings
		if err := rows.Scan(&ns.UserID, &ns.ChatID, &ns.MutedUntil, &ns.Sound, &ns.ShowPreview); err != nil {
			return nil, fmt.Errorf("notificationSettingsStore.ListByUser scan: %w", err)
		}
		result = append(result, ns)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notificationSettingsStore.ListByUser rows: %w", err)
	}
	return result, nil
}

// ─── NotificationOverrideStore ────────────────────────────────────────────────

type NotificationOverrideStore interface {
	Upsert(ctx context.Context, userID, chatID uuid.UUID, priority string) error
	Delete(ctx context.Context, userID, chatID uuid.UUID) error
}

type notificationOverrideStore struct {
	pool *pgxpool.Pool
}

func NewNotificationOverrideStore(pool *pgxpool.Pool) NotificationOverrideStore {
	return &notificationOverrideStore{pool: pool}
}

func (s *notificationOverrideStore) Upsert(ctx context.Context, userID, chatID uuid.UUID, priority string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO chat_notification_overrides (user_id, chat_id, priority_override, created_at, updated_at)
		 VALUES ($1, $2, $3, NOW(), NOW())
		 ON CONFLICT (user_id, chat_id) DO UPDATE SET
		   priority_override = EXCLUDED.priority_override,
		   updated_at = NOW()`,
		userID, chatID, priority)
	if err != nil {
		return fmt.Errorf("notificationOverrideStore.Upsert: %w", err)
	}
	return nil
}

func (s *notificationOverrideStore) Delete(ctx context.Context, userID, chatID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM chat_notification_overrides WHERE user_id = $1 AND chat_id = $2`,
		userID, chatID)
	if err != nil {
		return fmt.Errorf("notificationOverrideStore.Delete: %w", err)
	}
	return nil
}

// ─── PushSubscriptionStore ───────────────────────────────────────────────────

type PushSubscriptionStore interface {
	Create(ctx context.Context, sub *model.PushSubscription) error
	Delete(ctx context.Context, userID uuid.UUID, endpoint string) error
	ListByUser(ctx context.Context, userID uuid.UUID) ([]model.PushSubscription, error)
	CountByUser(ctx context.Context, userID uuid.UUID) (int, error)
}

type pushSubscriptionStore struct {
	pool *pgxpool.Pool
}

func NewPushSubscriptionStore(pool *pgxpool.Pool) PushSubscriptionStore {
	return &pushSubscriptionStore{pool: pool}
}

func (s *pushSubscriptionStore) Create(ctx context.Context, sub *model.PushSubscription) error {
	// Atomic cap enforcement: only insert if the user has fewer than 10 subscriptions,
	// or if the endpoint already exists (upsert). Prevents TOCTOU race on the cap check.
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth, user_agent, created_at)
		 SELECT $1, $2, $3, $4, $5, $6, NOW()
		 WHERE (SELECT COUNT(*) FROM push_subscriptions WHERE user_id = $2) < 10
		    OR EXISTS (SELECT 1 FROM push_subscriptions WHERE user_id = $2 AND endpoint = $3)
		 ON CONFLICT (user_id, endpoint) DO UPDATE SET
		   p256dh     = EXCLUDED.p256dh,
		   auth       = EXCLUDED.auth,
		   user_agent = EXCLUDED.user_agent`,
		sub.ID, sub.UserID, sub.Endpoint, sub.P256DH, sub.Auth, sub.UserAgent,
	)
	if err != nil {
		return fmt.Errorf("pushSubscriptionStore.Create: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("pushSubscriptionStore.Create: %w", model.ErrPushSubscriptionLimitReached)
	}
	return nil
}

func (s *pushSubscriptionStore) Delete(ctx context.Context, userID uuid.UUID, endpoint string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM push_subscriptions
		 WHERE user_id = $1 AND endpoint = $2`,
		userID, endpoint,
	)
	if err != nil {
		return fmt.Errorf("pushSubscriptionStore.Delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NotFound("push subscription not found")
	}
	return nil
}

func (s *pushSubscriptionStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]model.PushSubscription, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, endpoint, p256dh, auth, user_agent, created_at
		 FROM push_subscriptions
		 WHERE user_id = $1
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("pushSubscriptionStore.ListByUser: %w", err)
	}
	defer rows.Close()

	var subs []model.PushSubscription
	for rows.Next() {
		var sub model.PushSubscription
		if err := rows.Scan(
			&sub.ID, &sub.UserID, &sub.Endpoint, &sub.P256DH,
			&sub.Auth, &sub.UserAgent, &sub.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("pushSubscriptionStore.ListByUser scan: %w", err)
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

func (s *pushSubscriptionStore) CountByUser(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM push_subscriptions WHERE user_id = $1`,
		userID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("pushSubscriptionStore.CountByUser: %w", err)
	}
	return count, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// isPgUniqueViolation returns true when err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505).
func isPgUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	type pgErr interface{ SQLState() string }
	if e, ok := err.(pgErr); ok {
		return e.SQLState() == "23505"
	}
	return false
}
