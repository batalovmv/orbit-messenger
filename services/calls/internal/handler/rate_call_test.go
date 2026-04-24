// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/services/calls/internal/model"
	"github.com/mst-corp/orbit/services/calls/internal/service"
)

// newRateApp wires a Fiber app with a CallHandler backed by the provided
// mocks — enough for exercising the POST /calls/:id/rating endpoint.
func newRateApp(cs *mockCallStore, ps *mockParticipantStore) *fiber.App {
	app := fiber.New()
	nats := service.NewNoopNATSPublisher()
	svc := service.NewCallService(cs, ps, nats, slog.Default())
	h := NewCallHandler(svc, slog.Default(), "", "", "")
	h.Register(app)
	return app
}

func endedCall(id, initiator uuid.UUID) *model.Call {
	return &model.Call{
		ID:          id,
		Type:        model.CallTypeVoice,
		Mode:        model.CallModeP2P,
		InitiatorID: initiator,
		Status:      model.CallStatusEnded,
	}
}

func postRating(t *testing.T, app *fiber.App, callID, userID uuid.UUID, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("/calls/%s/rating", callID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

func TestRateCall_HappyPath(t *testing.T) {
	callID := uuid.New()
	userID := uuid.New()

	var rated struct {
		rating  int
		comment string
		user    uuid.UUID
	}

	cs := &mockCallStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Call, error) {
			return endedCall(id, uuid.New()), nil
		},
		rateFn: func(_ context.Context, _, u uuid.UUID, r int, c string) error {
			rated.rating = r
			rated.comment = c
			rated.user = u
			return nil
		},
	}
	ps := &mockParticipantStore{
		wasParticipantFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
	}

	app := newRateApp(cs, ps)
	resp := postRating(t, app, callID, userID, `{"rating":5,"comment":"great call"}`)

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}
	if rated.rating != 5 || rated.comment != "great call" || rated.user != userID {
		t.Errorf("unexpected rate args: %+v", rated)
	}
}

func TestRateCall_InitiatorBypassesParticipantCheck(t *testing.T) {
	callID := uuid.New()
	initiatorID := uuid.New()

	rateCalled := false
	cs := &mockCallStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Call, error) {
			return endedCall(id, initiatorID), nil
		},
		rateFn: func(_ context.Context, _, _ uuid.UUID, _ int, _ string) error {
			rateCalled = true
			return nil
		},
	}
	ps := &mockParticipantStore{
		// Simulate the edge case where the initiator never got a participant row.
		wasParticipantFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return false, nil },
	}

	app := newRateApp(cs, ps)
	resp := postRating(t, app, callID, initiatorID, `{"rating":4}`)

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}
	if !rateCalled {
		t.Errorf("rate store method was not invoked")
	}
}

func TestRateCall_InvalidRating(t *testing.T) {
	callID := uuid.New()
	userID := uuid.New()

	app := newRateApp(&mockCallStore{}, &mockParticipantStore{})
	for _, body := range []string{`{"rating":0}`, `{"rating":6}`, `{"rating":-1}`} {
		resp := postRating(t, app, callID, userID, body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("body %s: expected 400, got %d", body, resp.StatusCode)
		}
	}
}

func TestRateCall_MissingUserHeader(t *testing.T) {
	app := newRateApp(&mockCallStore{}, &mockParticipantStore{})
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("/calls/%s/rating", uuid.New()), bytes.NewBufferString(`{"rating":5}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestRateCall_NotParticipant(t *testing.T) {
	callID := uuid.New()
	userID := uuid.New()

	cs := &mockCallStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Call, error) {
			return endedCall(id, uuid.New()), nil
		},
	}
	ps := &mockParticipantStore{
		wasParticipantFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return false, nil },
	}

	app := newRateApp(cs, ps)
	resp := postRating(t, app, callID, userID, `{"rating":5}`)

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestRateCall_CallNotFinished(t *testing.T) {
	callID := uuid.New()
	userID := uuid.New()

	cs := &mockCallStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Call, error) {
			call := endedCall(id, uuid.New())
			call.Status = model.CallStatusActive
			return call, nil
		},
	}
	ps := &mockParticipantStore{
		wasParticipantFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
	}

	app := newRateApp(cs, ps)
	resp := postRating(t, app, callID, userID, `{"rating":5}`)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestRateCall_NotFound(t *testing.T) {
	callID := uuid.New()
	userID := uuid.New()

	cs := &mockCallStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Call, error) { return nil, nil },
	}
	app := newRateApp(cs, &mockParticipantStore{})
	resp := postRating(t, app, callID, userID, `{"rating":5}`)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestRateCall_AlreadyRated(t *testing.T) {
	callID := uuid.New()
	userID := uuid.New()

	cs := &mockCallStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Call, error) {
			return endedCall(id, uuid.New()), nil
		},
		rateFn: func(_ context.Context, _, _ uuid.UUID, _ int, _ string) error {
			return model.ErrAlreadyRated
		},
	}
	ps := &mockParticipantStore{
		wasParticipantFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
	}

	app := newRateApp(cs, ps)
	resp := postRating(t, app, callID, userID, `{"rating":5}`)

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409, got %d", resp.StatusCode)
	}
}

func TestRateCall_InvalidCallIDParam(t *testing.T) {
	app := newRateApp(&mockCallStore{}, &mockParticipantStore{})
	req, _ := http.NewRequest(http.MethodPost, "/calls/not-a-uuid/rating", bytes.NewBufferString(`{"rating":5}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

