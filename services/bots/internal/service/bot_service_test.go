package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/bots/internal/model"
	"github.com/mst-corp/orbit/services/bots/internal/store"
)

// Scope-enforcement tests — these guard against regression of the behavior
// introduced by commit 4d22128 where legacy installations (scopes=0 rows in
// DB before scope enforcement landed) must continue to work with full access.

func TestCheckBotScope_LegacyZeroScopesAllowsAll(t *testing.T) {
	botID := uuid.New()
	chatID := uuid.New()
	store := &svcTestInstallStore{
		getByBotAndChatFn: func(ctx context.Context, b, c uuid.UUID) (*model.BotInstallation, error) {
			return &model.BotInstallation{BotID: b, ChatID: c, Scopes: 0, IsActive: true}, nil
		},
	}
	svc := newSvcForScopeTest(store)

	// Any required scope must be allowed when installed scope is 0 (legacy).
	for _, required := range []int64{1, 2, 4, 1 << 10} {
		if err := svc.CheckBotScope(context.Background(), botID, chatID, required); err != nil {
			t.Errorf("legacy scopes=0 must allow required=%d, got err=%v", required, err)
		}
	}
}

func TestCheckBotScope_GrantedScopeAllows(t *testing.T) {
	grant := int64(1) | int64(4)
	store := &svcTestInstallStore{
		getByBotAndChatFn: func(ctx context.Context, b, c uuid.UUID) (*model.BotInstallation, error) {
			return &model.BotInstallation{BotID: b, ChatID: c, Scopes: grant, IsActive: true}, nil
		},
	}
	svc := newSvcForScopeTest(store)
	if err := svc.CheckBotScope(context.Background(), uuid.New(), uuid.New(), 4); err != nil {
		t.Fatalf("expected required=4 to match grant=%b, got %v", grant, err)
	}
}

func TestCheckBotScope_MissingScopeForbidden(t *testing.T) {
	store := &svcTestInstallStore{
		getByBotAndChatFn: func(ctx context.Context, b, c uuid.UUID) (*model.BotInstallation, error) {
			return &model.BotInstallation{BotID: b, ChatID: c, Scopes: 1, IsActive: true}, nil
		},
	}
	svc := newSvcForScopeTest(store)
	err := svc.CheckBotScope(context.Background(), uuid.New(), uuid.New(), 4)
	if err == nil {
		t.Fatalf("expected forbidden, got nil")
	}
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 403 {
		t.Fatalf("expected 403 AppError, got %v", err)
	}
}

func TestCheckBotScope_NotInstalledForbidden(t *testing.T) {
	store := &svcTestInstallStore{
		getByBotAndChatFn: func(ctx context.Context, b, c uuid.UUID) (*model.BotInstallation, error) {
			return nil, nil
		},
	}
	svc := newSvcForScopeTest(store)
	err := svc.CheckBotScope(context.Background(), uuid.New(), uuid.New(), 1)
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 403 {
		t.Fatalf("expected 403, got %v", err)
	}
}

func TestCheckBotScope_InactiveForbidden(t *testing.T) {
	store := &svcTestInstallStore{
		getByBotAndChatFn: func(ctx context.Context, b, c uuid.UUID) (*model.BotInstallation, error) {
			return &model.BotInstallation{Scopes: 0xFF, IsActive: false}, nil
		},
	}
	svc := newSvcForScopeTest(store)
	err := svc.CheckBotScope(context.Background(), uuid.New(), uuid.New(), 1)
	if err == nil {
		t.Fatalf("expected forbidden for inactive install, got nil")
	}
}

func TestSetBotAvatar_ValidURL(t *testing.T) {
	var captured string
	bots := &svcTestBotStore{
		setAvatarURLFn: func(ctx context.Context, id uuid.UUID, url string) error {
			captured = url
			return nil
		},
	}
	svc := NewBotService(bots, &svcTestTokenStore{}, &svcTestCmdStore{}, &svcTestInstallStore{}, "secret")
	if err := svc.SetBotAvatar(context.Background(), uuid.New(), "https://cdn.example.com/x.jpg"); err != nil {
		t.Fatalf("SetBotAvatar: %v", err)
	}
	if captured != "https://cdn.example.com/x.jpg" {
		t.Fatalf("URL not forwarded to store: %q", captured)
	}
}

func TestSetBotAvatar_EmptyURLRejected(t *testing.T) {
	svc := NewBotService(&svcTestBotStore{}, &svcTestTokenStore{}, &svcTestCmdStore{}, &svcTestInstallStore{}, "secret")
	err := svc.SetBotAvatar(context.Background(), uuid.New(), "   ")
	if err == nil {
		t.Fatalf("expected error for empty URL")
	}
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("expected 400, got %v", err)
	}
}

func TestSetBotAvatar_NotFound(t *testing.T) {
	bots := &svcTestBotStore{
		setAvatarURLFn: func(ctx context.Context, id uuid.UUID, url string) error {
			return model.ErrBotNotFound
		},
	}
	svc := NewBotService(bots, &svcTestTokenStore{}, &svcTestCmdStore{}, &svcTestInstallStore{}, "secret")
	err := svc.SetBotAvatar(context.Background(), uuid.New(), "https://x")
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("expected 404, got %v", err)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────

func newSvcForScopeTest(install *svcTestInstallStore) *BotService {
	return NewBotService(&svcTestBotStore{}, &svcTestTokenStore{}, &svcTestCmdStore{}, install, "secret")
}

type svcTestBotStore struct {
	store.BotStore
	setAvatarURLFn func(ctx context.Context, id uuid.UUID, url string) error
}

func (s *svcTestBotStore) SetAvatarURL(ctx context.Context, id uuid.UUID, url string) error {
	if s.setAvatarURLFn != nil {
		return s.setAvatarURLFn(ctx, id, url)
	}
	return nil
}

type svcTestTokenStore struct{ store.TokenStore }
type svcTestCmdStore struct{ store.CommandStore }

type svcTestInstallStore struct {
	store.InstallationStore
	getByBotAndChatFn func(ctx context.Context, botID, chatID uuid.UUID) (*model.BotInstallation, error)
}

func (s *svcTestInstallStore) GetByBotAndChat(ctx context.Context, botID, chatID uuid.UUID) (*model.BotInstallation, error) {
	if s.getByBotAndChatFn != nil {
		return s.getByBotAndChatFn(ctx, botID, chatID)
	}
	return nil, nil
}
