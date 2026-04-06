package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

type mockPushSubscriptionStore struct {
	createFn      func(ctx context.Context, sub *model.PushSubscription) error
	deleteFn      func(ctx context.Context, userID uuid.UUID, endpoint string) error
	listByUserFn  func(ctx context.Context, userID uuid.UUID) ([]model.PushSubscription, error)
	countByUserFn func(ctx context.Context, userID uuid.UUID) (int, error)
}

func (m *mockPushSubscriptionStore) Create(ctx context.Context, sub *model.PushSubscription) error {
	if m.createFn != nil {
		return m.createFn(ctx, sub)
	}
	return nil
}

func (m *mockPushSubscriptionStore) Delete(ctx context.Context, userID uuid.UUID, endpoint string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, userID, endpoint)
	}
	return nil
}

func (m *mockPushSubscriptionStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]model.PushSubscription, error) {
	if m.listByUserFn != nil {
		return m.listByUserFn(ctx, userID)
	}
	return nil, nil
}

func (m *mockPushSubscriptionStore) CountByUser(ctx context.Context, userID uuid.UUID) (int, error) {
	if m.countByUserFn != nil {
		return m.countByUserFn(ctx, userID)
	}
	return 0, nil
}

type noopPrivacySettingsStore struct{}

func (s *noopPrivacySettingsStore) GetByUserID(context.Context, uuid.UUID) (*model.PrivacySettings, error) {
	return &model.PrivacySettings{}, nil
}

func (s *noopPrivacySettingsStore) GetByUserIDs(_ context.Context, userIDs []uuid.UUID) (map[uuid.UUID]*model.PrivacySettings, error) {
	result := make(map[uuid.UUID]*model.PrivacySettings, len(userIDs))
	for _, uid := range userIDs {
		result[uid] = &model.PrivacySettings{}
	}
	return result, nil
}

func (s *noopPrivacySettingsStore) Upsert(context.Context, *model.PrivacySettings) error {
	return nil
}

type noopBlockedUsersStore struct{}

func (s *noopBlockedUsersStore) List(context.Context, uuid.UUID, int) ([]model.BlockedUser, error) {
	return nil, nil
}

func (s *noopBlockedUsersStore) IsBlocked(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}

func (s *noopBlockedUsersStore) Block(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (s *noopBlockedUsersStore) Unblock(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

type noopUserSettingsStore struct{}

func (s *noopUserSettingsStore) GetByUserID(context.Context, uuid.UUID) (*model.UserSettings, error) {
	return &model.UserSettings{}, nil
}

func (s *noopUserSettingsStore) Upsert(context.Context, *model.UserSettings) error {
	return nil
}

func (s *noopUserSettingsStore) GetGlobalNotifySettings(context.Context, uuid.UUID) (*model.GlobalNotifySettings, error) {
	return &model.GlobalNotifySettings{UsersPreview: true, GroupsPreview: true, ChannelsPreview: true}, nil
}

func (s *noopUserSettingsStore) UpdateGlobalNotifySettings(context.Context, uuid.UUID, *model.GlobalNotifySettings) error {
	return nil
}

type mockNotificationSettingsStore struct {
	getFn              func(ctx context.Context, userID, chatID uuid.UUID) (*model.NotificationSettings, error)
	upsertFn           func(ctx context.Context, settings *model.NotificationSettings) error
	deleteFn           func(ctx context.Context, userID, chatID uuid.UUID) error
	listMutedUserIDsFn func(ctx context.Context, chatID uuid.UUID, userIDs []string) ([]string, error)
}

func (m *mockNotificationSettingsStore) Get(ctx context.Context, userID, chatID uuid.UUID) (*model.NotificationSettings, error) {
	if m.getFn != nil {
		return m.getFn(ctx, userID, chatID)
	}
	return nil, nil
}

func (m *mockNotificationSettingsStore) Upsert(ctx context.Context, settings *model.NotificationSettings) error {
	if m.upsertFn != nil {
		return m.upsertFn(ctx, settings)
	}
	return nil
}

func (m *mockNotificationSettingsStore) Delete(ctx context.Context, userID, chatID uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, userID, chatID)
	}
	return nil
}

func (m *mockNotificationSettingsStore) ListMutedUserIDs(ctx context.Context, chatID uuid.UUID, userIDs []string) ([]string, error) {
	if m.listMutedUserIDsFn != nil {
		return m.listMutedUserIDsFn(ctx, chatID, userIDs)
	}
	return nil, nil
}

func (m *mockNotificationSettingsStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]model.NotificationSettings, error) {
	return nil, nil
}

func newSettingsApp(pushStore *mockPushSubscriptionStore, notifStore *mockNotificationSettingsStore, internalSecret string) *fiber.App {
	app := fiber.New()
	settingsSvc := service.NewSettingsService(
		&noopPrivacySettingsStore{},
		&noopBlockedUsersStore{},
		&noopUserSettingsStore{},
		notifStore,
		&mockChatStore{},
	)
	h := NewSettingsHandler(settingsSvc, pushStore, slog.Default(), internalSecret)
	h.Register(app)
	return app
}

func TestGetInternalPushSubscriptions_RequiresInternalToken(t *testing.T) {
	app := newSettingsApp(&mockPushSubscriptionStore{}, &mockNotificationSettingsStore{}, "secret")

	req, _ := http.NewRequest(http.MethodGet, "/internal/push-subscriptions/"+uuid.New().String(), nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestGetInternalPushSubscriptions_Success(t *testing.T) {
	userID := uuid.New()
	app := newSettingsApp(
		&mockPushSubscriptionStore{
			listByUserFn: func(_ context.Context, gotUserID uuid.UUID) ([]model.PushSubscription, error) {
				if gotUserID != userID {
					t.Fatalf("unexpected user ID: want %s, got %s", userID, gotUserID)
				}
				return []model.PushSubscription{
					{
						ID:        uuid.New(),
						UserID:    userID,
						Endpoint:  "https://push.example/1",
						P256DH:    "p256",
						Auth:      "auth",
						CreatedAt: time.Now(),
					},
				}, nil
			},
		},
		&mockNotificationSettingsStore{},
		"secret",
	)

	req, _ := http.NewRequest(http.MethodGet, "/internal/push-subscriptions/"+userID.String(), nil)
	req.Header.Set("X-Internal-Token", "secret")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	var subscriptions []model.PushSubscription
	if err := json.NewDecoder(resp.Body).Decode(&subscriptions); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(subscriptions) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subscriptions))
	}
	if subscriptions[0].Endpoint != "https://push.example/1" {
		t.Fatalf("unexpected endpoint: %s", subscriptions[0].Endpoint)
	}
}

func TestDeleteInternalPushSubscription_Success(t *testing.T) {
	userID := uuid.New()
	var deletedEndpoint string

	app := newSettingsApp(
		&mockPushSubscriptionStore{
			deleteFn: func(_ context.Context, gotUserID uuid.UUID, endpoint string) error {
				if gotUserID != userID {
					t.Fatalf("unexpected user ID: want %s, got %s", userID, gotUserID)
				}
				deletedEndpoint = endpoint
				return nil
			},
		},
		&mockNotificationSettingsStore{},
		"secret",
	)

	req, _ := http.NewRequest(
		http.MethodDelete,
		"/internal/push-subscriptions/"+userID.String()+"?endpoint=https%3A%2F%2Fpush.example%2Fdead",
		nil,
	)
	req.Header.Set("X-Internal-Token", "secret")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	if deletedEndpoint != "https://push.example/dead" {
		t.Fatalf("unexpected endpoint: %s", deletedEndpoint)
	}
}

func TestListMutedUsers_Success(t *testing.T) {
	chatID := uuid.New()
	userOne := uuid.New().String()
	userTwo := uuid.New().String()

	app := newSettingsApp(
		&mockPushSubscriptionStore{},
		&mockNotificationSettingsStore{
			listMutedUserIDsFn: func(_ context.Context, gotChatID uuid.UUID, gotUserIDs []string) ([]string, error) {
				if gotChatID != chatID {
					t.Fatalf("unexpected chat ID: want %s, got %s", chatID, gotChatID)
				}
				if len(gotUserIDs) != 2 || gotUserIDs[0] != userOne || gotUserIDs[1] != userTwo {
					t.Fatalf("unexpected user IDs: %+v", gotUserIDs)
				}
				return []string{userTwo}, nil
			},
		},
		"secret",
	)

	body := bytes.NewBufferString(`{"chat_id":"` + chatID.String() + `","user_ids":["` + userOne + `","` + userTwo + `"]}`)
	req, _ := http.NewRequest(http.MethodPost, "/internal/notification-settings/muted-users", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", "secret")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	var payload struct {
		MutedUserIDs []string `json:"muted_user_ids"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.MutedUserIDs) != 1 || payload.MutedUserIDs[0] != userTwo {
		t.Fatalf("unexpected muted user ids: %+v", payload.MutedUserIDs)
	}
}
