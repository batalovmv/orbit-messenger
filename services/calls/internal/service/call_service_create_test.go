package service

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mst-corp/orbit/services/calls/internal/model"
)

type stubCallStore struct {
	createFn           func(ctx context.Context, call *model.Call) error
	getByIDFn          func(ctx context.Context, id uuid.UUID) (*model.Call, error)
	updateStatusFn     func(ctx context.Context, id uuid.UUID, status string, startedAt, endedAt *time.Time, durationSeconds *int) error
	getActiveForChatFn func(ctx context.Context, chatID uuid.UUID) (*model.Call, error)
	listByUserFn       func(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]model.Call, string, bool, error)
	deleteFn           func(ctx context.Context, id uuid.UUID) error
	isUserInChatFn     func(ctx context.Context, chatID, userID uuid.UUID) (bool, error)
	getChatMemberIDsFn func(ctx context.Context, chatID uuid.UUID) ([]string, error)
	expireRingingFn    func(ctx context.Context, threshold time.Duration) ([]model.Call, error)
	rateFn             func(ctx context.Context, callID, userID uuid.UUID, rating int, comment string) error
}

func (s *stubCallStore) Create(ctx context.Context, call *model.Call) error {
	if s.createFn != nil {
		return s.createFn(ctx, call)
	}
	return nil
}

func (s *stubCallStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Call, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(ctx, id)
	}
	return nil, nil
}

func (s *stubCallStore) UpdateStatus(ctx context.Context, id uuid.UUID, status string, startedAt, endedAt *time.Time, durationSeconds *int) error {
	if s.updateStatusFn != nil {
		return s.updateStatusFn(ctx, id, status, startedAt, endedAt, durationSeconds)
	}
	return nil
}

func (s *stubCallStore) GetActiveForChat(ctx context.Context, chatID uuid.UUID) (*model.Call, error) {
	if s.getActiveForChatFn != nil {
		return s.getActiveForChatFn(ctx, chatID)
	}
	return nil, nil
}

func (s *stubCallStore) ListByUser(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]model.Call, string, bool, error) {
	if s.listByUserFn != nil {
		return s.listByUserFn(ctx, userID, cursor, limit)
	}
	return nil, "", false, nil
}

func (s *stubCallStore) Delete(ctx context.Context, id uuid.UUID) error {
	if s.deleteFn != nil {
		return s.deleteFn(ctx, id)
	}
	return nil
}

func (s *stubCallStore) IsUserInChat(ctx context.Context, chatID, userID uuid.UUID) (bool, error) {
	if s.isUserInChatFn != nil {
		return s.isUserInChatFn(ctx, chatID, userID)
	}
	return true, nil
}

func (s *stubCallStore) GetChatMemberIDs(ctx context.Context, chatID uuid.UUID) ([]string, error) {
	if s.getChatMemberIDsFn != nil {
		return s.getChatMemberIDsFn(ctx, chatID)
	}
	return nil, nil
}

func (s *stubCallStore) ExpireRinging(ctx context.Context, threshold time.Duration) ([]model.Call, error) {
	if s.expireRingingFn != nil {
		return s.expireRingingFn(ctx, threshold)
	}
	return nil, nil
}

func (s *stubCallStore) Rate(ctx context.Context, callID, userID uuid.UUID, rating int, comment string) error {
	if s.rateFn != nil {
		return s.rateFn(ctx, callID, userID, rating, comment)
	}
	return nil
}

type stubParticipantStore struct {
	addFn            func(ctx context.Context, p *model.CallParticipant) error
	removeFn         func(ctx context.Context, callID, userID uuid.UUID) error
	updateMuteFn     func(ctx context.Context, callID, userID uuid.UUID, isMuted bool) error
	updateScreenFn   func(ctx context.Context, callID, userID uuid.UUID, isSharing bool) error
	listByCallFn     func(ctx context.Context, callID uuid.UUID) ([]model.CallParticipant, error)
	isParticipantFn  func(ctx context.Context, callID, userID uuid.UUID) (bool, error)
	wasParticipantFn func(ctx context.Context, callID, userID uuid.UUID) (bool, error)
}

func (s *stubParticipantStore) Add(ctx context.Context, p *model.CallParticipant) error {
	if s.addFn != nil {
		return s.addFn(ctx, p)
	}
	return nil
}

func (s *stubParticipantStore) Remove(ctx context.Context, callID, userID uuid.UUID) error {
	if s.removeFn != nil {
		return s.removeFn(ctx, callID, userID)
	}
	return nil
}

func (s *stubParticipantStore) UpdateMute(ctx context.Context, callID, userID uuid.UUID, isMuted bool) error {
	if s.updateMuteFn != nil {
		return s.updateMuteFn(ctx, callID, userID, isMuted)
	}
	return nil
}

func (s *stubParticipantStore) UpdateScreenShare(ctx context.Context, callID, userID uuid.UUID, isSharing bool) error {
	if s.updateScreenFn != nil {
		return s.updateScreenFn(ctx, callID, userID, isSharing)
	}
	return nil
}

func (s *stubParticipantStore) ListByCall(ctx context.Context, callID uuid.UUID) ([]model.CallParticipant, error) {
	if s.listByCallFn != nil {
		return s.listByCallFn(ctx, callID)
	}
	return nil, nil
}

func (s *stubParticipantStore) IsParticipant(ctx context.Context, callID, userID uuid.UUID) (bool, error) {
	if s.isParticipantFn != nil {
		return s.isParticipantFn(ctx, callID, userID)
	}
	return false, nil
}

func (s *stubParticipantStore) WasParticipant(ctx context.Context, callID, userID uuid.UUID) (bool, error) {
	if s.wasParticipantFn != nil {
		return s.wasParticipantFn(ctx, callID, userID)
	}
	return false, nil
}

func TestCreateCall_ReturnsExistingCallOnUniqueViolation(t *testing.T) {
	chatID := uuid.New()
	initiatorID := uuid.New()
	existing := &model.Call{
		ID:          uuid.New(),
		Type:        model.CallTypeVoice,
		Mode:        model.CallModeGroup,
		ChatID:      chatID,
		InitiatorID: initiatorID,
		Status:      model.CallStatusRinging,
	}

	var activeLookupCount atomic.Int32
	callStore := &stubCallStore{
		getActiveForChatFn: func(ctx context.Context, gotChatID uuid.UUID) (*model.Call, error) {
			if gotChatID != chatID {
				t.Fatalf("expected chatID %s, got %s", chatID, gotChatID)
			}
			if activeLookupCount.Add(1) == 1 {
				return nil, nil
			}
			return existing, nil
		},
		isUserInChatFn: func(ctx context.Context, gotChatID, gotUserID uuid.UUID) (bool, error) {
			return gotChatID == chatID && gotUserID == initiatorID, nil
		},
		createFn: func(ctx context.Context, call *model.Call) error {
			return &pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"}
		},
	}
	participants := &stubParticipantStore{
		addFn: func(ctx context.Context, p *model.CallParticipant) error {
			t.Fatal("participant add must not run when create falls back to existing call")
			return nil
		},
	}

	svc := NewCallService(callStore, participants, NewNoopNATSPublisher(), slog.Default())
	call, err := svc.CreateCall(context.Background(), initiatorID, CreateCallRequest{
		ChatID: chatID,
		Type:   model.CallTypeVoice,
		Mode:   model.CallModeGroup,
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if call == nil || call.ID != existing.ID {
		t.Fatalf("expected existing call %s, got %+v", existing.ID, call)
	}
	if call.SfuWsURL != "/api/v1/calls/"+existing.ID.String()+"/sfu-ws" {
		t.Fatalf("expected SFU URL to be attached, got %q", call.SfuWsURL)
	}
}

func TestCreateCall_ConcurrentRequestsReturnSameCall(t *testing.T) {
	chatID := uuid.New()
	initiatorID := uuid.New()

	var (
		mu         sync.Mutex
		activeCall *model.Call
	)
	precheckBarrier := make(chan struct{})
	var precheckCount atomic.Int32

	callStore := &stubCallStore{
		getActiveForChatFn: func(ctx context.Context, gotChatID uuid.UUID) (*model.Call, error) {
			if gotChatID != chatID {
				t.Fatalf("expected chatID %s, got %s", chatID, gotChatID)
			}
			if precheckCount.Add(1) <= 2 {
				if precheckCount.Load() == 2 {
					close(precheckBarrier)
				}
				<-precheckBarrier
				return nil, nil
			}

			mu.Lock()
			defer mu.Unlock()
			return activeCall, nil
		},
		isUserInChatFn: func(ctx context.Context, gotChatID, gotUserID uuid.UUID) (bool, error) {
			return gotChatID == chatID && gotUserID == initiatorID, nil
		},
		createFn: func(ctx context.Context, call *model.Call) error {
			mu.Lock()
			defer mu.Unlock()
			if activeCall != nil {
				return &pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"}
			}
			now := time.Now().UTC()
			call.CreatedAt = now
			call.UpdatedAt = now
			cloned := *call
			activeCall = &cloned
			return nil
		},
		getChatMemberIDsFn: func(ctx context.Context, gotChatID uuid.UUID) ([]string, error) {
			return []string{initiatorID.String()}, nil
		},
	}
	participants := &stubParticipantStore{
		addFn: func(ctx context.Context, p *model.CallParticipant) error {
			return nil
		},
	}

	svc := NewCallService(callStore, participants, NewNoopNATSPublisher(), slog.Default())

	results := make(chan *model.Call, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			call, err := svc.CreateCall(context.Background(), initiatorID, CreateCallRequest{
				ChatID: chatID,
				Type:   model.CallTypeVoice,
				Mode:   model.CallModeP2P,
			})
			results <- call
			errs <- err
		}()
	}

	var returned []*model.Call
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("create call %d failed: %v", i, err)
		}
		returned = append(returned, <-results)
	}

	if returned[0] == nil || returned[1] == nil {
		t.Fatalf("expected both calls to return a call, got %+v", returned)
	}
	if returned[0].ID != returned[1].ID {
		t.Fatalf("expected both concurrent requests to return the same call ID, got %s and %s", returned[0].ID, returned[1].ID)
	}
}
