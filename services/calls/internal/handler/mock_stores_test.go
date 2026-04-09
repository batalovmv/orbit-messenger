package handler

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mst-corp/orbit/services/calls/internal/model"
)

// ---------------------------------------------------------------------------
// Mock CallStore
// ---------------------------------------------------------------------------

type mockCallStore struct {
	createFn            func(ctx context.Context, call *model.Call) error
	getByIDFn           func(ctx context.Context, id uuid.UUID) (*model.Call, error)
	updateStatusFn      func(ctx context.Context, id uuid.UUID, status string, startedAt, endedAt *time.Time, durationSeconds *int) error
	getActiveForChatFn  func(ctx context.Context, chatID uuid.UUID) (*model.Call, error)
	listByUserFn        func(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]model.Call, string, bool, error)
	deleteFn            func(ctx context.Context, id uuid.UUID) error
	isUserInChatFn      func(ctx context.Context, chatID, userID uuid.UUID) (bool, error)
	getChatMemberIDsFn  func(ctx context.Context, chatID uuid.UUID) ([]string, error)
	expireRingingFn     func(ctx context.Context, threshold time.Duration) ([]model.Call, error)
	rateFn              func(ctx context.Context, callID, userID uuid.UUID, rating int, comment string) error
}

func (m *mockCallStore) Create(ctx context.Context, call *model.Call) error {
	if m.createFn != nil {
		return m.createFn(ctx, call)
	}
	return nil
}

func (m *mockCallStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Call, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockCallStore) UpdateStatus(ctx context.Context, id uuid.UUID, status string, startedAt, endedAt *time.Time, durationSeconds *int) error {
	if m.updateStatusFn != nil {
		return m.updateStatusFn(ctx, id, status, startedAt, endedAt, durationSeconds)
	}
	return nil
}

func (m *mockCallStore) GetActiveForChat(ctx context.Context, chatID uuid.UUID) (*model.Call, error) {
	if m.getActiveForChatFn != nil {
		return m.getActiveForChatFn(ctx, chatID)
	}
	return nil, nil
}

func (m *mockCallStore) ListByUser(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]model.Call, string, bool, error) {
	if m.listByUserFn != nil {
		return m.listByUserFn(ctx, userID, cursor, limit)
	}
	return nil, "", false, nil
}

func (m *mockCallStore) Delete(ctx context.Context, id uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

func (m *mockCallStore) IsUserInChat(ctx context.Context, chatID, userID uuid.UUID) (bool, error) {
	if m.isUserInChatFn != nil {
		return m.isUserInChatFn(ctx, chatID, userID)
	}
	return true, nil
}

func (m *mockCallStore) GetChatMemberIDs(ctx context.Context, chatID uuid.UUID) ([]string, error) {
	if m.getChatMemberIDsFn != nil {
		return m.getChatMemberIDsFn(ctx, chatID)
	}
	return nil, nil
}

func (m *mockCallStore) ExpireRinging(ctx context.Context, threshold time.Duration) ([]model.Call, error) {
	if m.expireRingingFn != nil {
		return m.expireRingingFn(ctx, threshold)
	}
	return nil, nil
}

func (m *mockCallStore) Rate(ctx context.Context, callID, userID uuid.UUID, rating int, comment string) error {
	if m.rateFn != nil {
		return m.rateFn(ctx, callID, userID, rating, comment)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mock ParticipantStore
// ---------------------------------------------------------------------------

type mockParticipantStore struct {
	addFn              func(ctx context.Context, p *model.CallParticipant) error
	removeFn           func(ctx context.Context, callID, userID uuid.UUID) error
	updateMuteFn       func(ctx context.Context, callID, userID uuid.UUID, isMuted bool) error
	updateScreenFn     func(ctx context.Context, callID, userID uuid.UUID, isSharing bool) error
	listByCallFn       func(ctx context.Context, callID uuid.UUID) ([]model.CallParticipant, error)
	isParticipantFn    func(ctx context.Context, callID, userID uuid.UUID) (bool, error)
	wasParticipantFn   func(ctx context.Context, callID, userID uuid.UUID) (bool, error)
}

func (m *mockParticipantStore) Add(ctx context.Context, p *model.CallParticipant) error {
	if m.addFn != nil {
		return m.addFn(ctx, p)
	}
	return nil
}

func (m *mockParticipantStore) Remove(ctx context.Context, callID, userID uuid.UUID) error {
	if m.removeFn != nil {
		return m.removeFn(ctx, callID, userID)
	}
	return nil
}

func (m *mockParticipantStore) UpdateMute(ctx context.Context, callID, userID uuid.UUID, isMuted bool) error {
	if m.updateMuteFn != nil {
		return m.updateMuteFn(ctx, callID, userID, isMuted)
	}
	return nil
}

func (m *mockParticipantStore) UpdateScreenShare(ctx context.Context, callID, userID uuid.UUID, isSharing bool) error {
	if m.updateScreenFn != nil {
		return m.updateScreenFn(ctx, callID, userID, isSharing)
	}
	return nil
}

func (m *mockParticipantStore) ListByCall(ctx context.Context, callID uuid.UUID) ([]model.CallParticipant, error) {
	if m.listByCallFn != nil {
		return m.listByCallFn(ctx, callID)
	}
	return nil, nil
}

func (m *mockParticipantStore) IsParticipant(ctx context.Context, callID, userID uuid.UUID) (bool, error) {
	if m.isParticipantFn != nil {
		return m.isParticipantFn(ctx, callID, userID)
	}
	return false, nil
}

func (m *mockParticipantStore) WasParticipant(ctx context.Context, callID, userID uuid.UUID) (bool, error) {
	if m.wasParticipantFn != nil {
		return m.wasParticipantFn(ctx, callID, userID)
	}
	return false, nil
}
