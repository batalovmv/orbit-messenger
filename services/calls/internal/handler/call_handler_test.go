package handler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/services/calls/internal/model"
	"github.com/mst-corp/orbit/services/calls/internal/service"
)

func newCallLifecycleApp(cs *mockCallStore, ps *mockParticipantStore) *fiber.App {
	app := fiber.New()
	nats := service.NewNoopNATSPublisher()
	svc := service.NewCallService(cs, ps, nats, slog.Default())
	h := NewCallHandler(svc, slog.Default(), "", "", "")
	h.Register(app)
	return app
}

func performCallAction(t *testing.T, app *fiber.App, method, path string, userID uuid.UUID) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, path, nil)
	req.Header.Set("X-User-ID", userID.String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

func requireStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected %d, got %d: %s", want, resp.StatusCode, raw)
	}
}

func ringingCall(callID, chatID, initiatorID uuid.UUID) *model.Call {
	return &model.Call{
		ID:          callID,
		Type:        model.CallTypeVoice,
		Mode:        model.CallModeP2P,
		ChatID:      chatID,
		InitiatorID: initiatorID,
		Status:      model.CallStatusRinging,
	}
}

func activeCall(callID, chatID, initiatorID uuid.UUID) *model.Call {
	call := ringingCall(callID, chatID, initiatorID)
	call.Status = model.CallStatusActive
	return call
}

func TestDeclineCall_StrangerForbidden(t *testing.T) {
	callID := uuid.New()
	chatID := uuid.New()
	initiatorID := uuid.New()
	strangerID := uuid.New()

	cs := &mockCallStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Call, error) {
			return ringingCall(id, chatID, initiatorID), nil
		},
		isUserInChatFn: func(_ context.Context, gotChatID, gotUserID uuid.UUID) (bool, error) {
			if gotChatID != chatID || gotUserID != strangerID {
				t.Fatalf("unexpected membership check: chat=%s user=%s", gotChatID, gotUserID)
			}
			return false, nil
		},
	}

	app := newCallLifecycleApp(cs, &mockParticipantStore{})
	resp := performCallAction(t, app, http.MethodPut, fmt.Sprintf("/calls/%s/decline", callID), strangerID)
	requireStatus(t, resp, http.StatusForbidden)
}

func TestDeclineCall_MemberSuccess(t *testing.T) {
	callID := uuid.New()
	chatID := uuid.New()
	initiatorID := uuid.New()
	memberID := uuid.New()
	updated := false

	cs := &mockCallStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Call, error) {
			return ringingCall(id, chatID, initiatorID), nil
		},
		isUserInChatFn: func(_ context.Context, gotChatID, gotUserID uuid.UUID) (bool, error) {
			if gotChatID != chatID || gotUserID != memberID {
				t.Fatalf("unexpected membership check: chat=%s user=%s", gotChatID, gotUserID)
			}
			return true, nil
		},
		updateStatusFn: func(_ context.Context, id uuid.UUID, status string, _, _ *time.Time, _ *int) error {
			if id != callID || status != model.CallStatusDeclined {
				t.Fatalf("unexpected decline update: id=%s status=%s", id, status)
			}
			updated = true
			return nil
		},
	}

	app := newCallLifecycleApp(cs, &mockParticipantStore{})
	resp := performCallAction(t, app, http.MethodPut, fmt.Sprintf("/calls/%s/decline", callID), memberID)
	requireStatus(t, resp, http.StatusOK)
	if !updated {
		t.Fatal("expected UpdateStatus to be called")
	}
}

func TestEndCall_StrangerForbidden(t *testing.T) {
	callID := uuid.New()
	chatID := uuid.New()
	initiatorID := uuid.New()
	strangerID := uuid.New()

	cs := &mockCallStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Call, error) {
			return activeCall(id, chatID, initiatorID), nil
		},
	}
	ps := &mockParticipantStore{
		isParticipantFn: func(_ context.Context, gotCallID, gotUserID uuid.UUID) (bool, error) {
			if gotCallID != callID || gotUserID != strangerID {
				t.Fatalf("unexpected participant check: call=%s user=%s", gotCallID, gotUserID)
			}
			return false, nil
		},
	}

	app := newCallLifecycleApp(cs, ps)
	resp := performCallAction(t, app, http.MethodPut, fmt.Sprintf("/calls/%s/end", callID), strangerID)
	requireStatus(t, resp, http.StatusForbidden)
}

func TestEndCall_ParticipantSuccess(t *testing.T) {
	callID := uuid.New()
	chatID := uuid.New()
	initiatorID := uuid.New()
	participantID := uuid.New()
	updated := false

	cs := &mockCallStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Call, error) {
			return activeCall(id, chatID, initiatorID), nil
		},
		updateStatusFn: func(_ context.Context, id uuid.UUID, status string, _, _ *time.Time, _ *int) error {
			if id != callID || status != model.CallStatusEnded {
				t.Fatalf("unexpected end update: id=%s status=%s", id, status)
			}
			updated = true
			return nil
		},
	}
	ps := &mockParticipantStore{
		isParticipantFn: func(_ context.Context, gotCallID, gotUserID uuid.UUID) (bool, error) {
			if gotCallID != callID || gotUserID != participantID {
				t.Fatalf("unexpected participant check: call=%s user=%s", gotCallID, gotUserID)
			}
			return true, nil
		},
	}

	app := newCallLifecycleApp(cs, ps)
	resp := performCallAction(t, app, http.MethodPut, fmt.Sprintf("/calls/%s/end", callID), participantID)
	requireStatus(t, resp, http.StatusOK)
	if !updated {
		t.Fatal("expected UpdateStatus to be called")
	}
}

func TestRemoveParticipant_StrangerForbidden(t *testing.T) {
	callID := uuid.New()
	chatID := uuid.New()
	initiatorID := uuid.New()
	strangerID := uuid.New()
	targetID := uuid.New()
	removed := false

	cs := &mockCallStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Call, error) {
			return activeCall(id, chatID, initiatorID), nil
		},
	}
	ps := &mockParticipantStore{
		removeFn: func(_ context.Context, _, _ uuid.UUID) error {
			removed = true
			return nil
		},
	}

	app := newCallLifecycleApp(cs, ps)
	resp := performCallAction(t, app, http.MethodDelete, fmt.Sprintf("/calls/%s/participants/%s", callID, targetID), strangerID)
	requireStatus(t, resp, http.StatusForbidden)
	if removed {
		t.Fatal("remove should not be called for stranger")
	}
}

func TestRemoveParticipant_InitiatorSuccess(t *testing.T) {
	callID := uuid.New()
	chatID := uuid.New()
	initiatorID := uuid.New()
	targetID := uuid.New()
	removed := false

	cs := &mockCallStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Call, error) {
			return activeCall(id, chatID, initiatorID), nil
		},
	}
	ps := &mockParticipantStore{
		removeFn: func(_ context.Context, gotCallID, gotUserID uuid.UUID) error {
			if gotCallID != callID || gotUserID != targetID {
				t.Fatalf("unexpected remove target: call=%s user=%s", gotCallID, gotUserID)
			}
			removed = true
			return nil
		},
	}

	app := newCallLifecycleApp(cs, ps)
	resp := performCallAction(t, app, http.MethodDelete, fmt.Sprintf("/calls/%s/participants/%s", callID, targetID), initiatorID)
	requireStatus(t, resp, http.StatusOK)
	if !removed {
		t.Fatal("expected participant removal")
	}
}
