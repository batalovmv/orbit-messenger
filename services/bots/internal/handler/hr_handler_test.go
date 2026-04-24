// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/bots/internal/model"
	"github.com/mst-corp/orbit/services/bots/internal/store"
)

type mockHRBotSvc struct {
	getBotFn        func(ctx context.Context, id uuid.UUID) (*model.Bot, error)
	checkBotScopeFn func(ctx context.Context, botID, chatID uuid.UUID, scope int64) error
}

func (m *mockHRBotSvc) GetBot(ctx context.Context, id uuid.UUID) (*model.Bot, error) {
	if m.getBotFn != nil {
		return m.getBotFn(ctx, id)
	}
	return nil, nil
}

func (m *mockHRBotSvc) CheckBotScope(ctx context.Context, botID, chatID uuid.UUID, scope int64) error {
	if m.checkBotScopeFn != nil {
		return m.checkBotScopeFn(ctx, botID, chatID, scope)
	}
	return nil
}

type mockHRStore struct {
	createFn  func(ctx context.Context, r *model.HRRequest) error
	getByIDFn func(ctx context.Context, id uuid.UUID) (*model.HRRequest, error)
	listFn    func(ctx context.Context, f store.HRRequestFilter) ([]model.HRRequest, error)
	decideFn  func(ctx context.Context, id, approverID uuid.UUID, approve bool, note string) (*model.HRRequest, error)
}

func (m *mockHRStore) Create(ctx context.Context, r *model.HRRequest) error {
	if m.createFn != nil {
		return m.createFn(ctx, r)
	}
	r.ID = uuid.New()
	r.Status = model.HRStatusPending
	r.CreatedAt = time.Now()
	r.UpdatedAt = r.CreatedAt
	return nil
}

func (m *mockHRStore) GetByID(ctx context.Context, id uuid.UUID) (*model.HRRequest, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, model.ErrHRRequestNotFound
}

func (m *mockHRStore) List(ctx context.Context, f store.HRRequestFilter) ([]model.HRRequest, error) {
	if m.listFn != nil {
		return m.listFn(ctx, f)
	}
	return nil, nil
}

func (m *mockHRStore) Decide(ctx context.Context, id, approverID uuid.UUID, approve bool, note string) (*model.HRRequest, error) {
	if m.decideFn != nil {
		return m.decideFn(ctx, id, approverID, approve, note)
	}
	return nil, nil
}

func setupHR(h *HRHandler) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: fiberErrorAdapter()})
	// Inject X-User-ID into Locals via simple middleware that mirrors getUserID().
	app.Use(func(c *fiber.Ctx) error {
		return c.Next()
	})
	h.Register(app)
	return app
}

func fiberErrorAdapter() fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		var appErr *apperror.AppError
		if errors.As(err, &appErr) {
			return c.Status(appErr.Status).JSON(fiber.Map{"error": appErr.Message})
		}
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
}

func doJSON(app *fiber.App, method, path string, userID uuid.UUID, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if userID != uuid.Nil {
		req.Header.Set("X-User-ID", userID.String())
	}
	resp, _ := app.Test(req, -1)
	rec := httptest.NewRecorder()
	rec.Code = resp.StatusCode
	if resp.Body != nil {
		b, _ := io.ReadAll(resp.Body)
		rec.Body.Write(b)
		resp.Body.Close()
	}
	return rec
}

func newHandler(svc HRBotService, st store.HRRequestStore) *HRHandler {
	return NewHRHandler(svc, st, slog.Default())
}

func TestHRCreate_Happy(t *testing.T) {
	botID := uuid.New()
	chatID := uuid.New()
	userID := uuid.New()

	var saved *model.HRRequest
	h := newHandler(&mockHRBotSvc{}, &mockHRStore{
		createFn: func(_ context.Context, r *model.HRRequest) error {
			r.ID = uuid.New()
			r.Status = model.HRStatusPending
			saved = r
			return nil
		},
	})
	app := setupHR(h)

	body := fiber.Map{
		"chat_id":      chatID.String(),
		"request_type": "vacation",
		"start_date":   "2026-05-01",
		"end_date":     "2026-05-10",
		"reason":       "family",
	}
	rec := doJSON(app, "POST", "/bots/"+botID.String()+"/hr/requests", userID, body)
	if rec.Code != 201 {
		t.Fatalf("expected 201, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if saved == nil || saved.UserID != userID || saved.ChatID != chatID || saved.BotID != botID {
		t.Fatalf("unexpected stored request: %+v", saved)
	}
	if saved.RequestType != "vacation" {
		t.Fatalf("expected vacation, got %s", saved.RequestType)
	}
}

func TestHRCreate_InvalidRequestType(t *testing.T) {
	h := newHandler(&mockHRBotSvc{}, &mockHRStore{})
	app := setupHR(h)

	body := fiber.Map{
		"chat_id":      uuid.New().String(),
		"request_type": "unlimited_pto",
		"start_date":   "2026-05-01",
		"end_date":     "2026-05-10",
	}
	rec := doJSON(app, "POST", "/bots/"+uuid.New().String()+"/hr/requests", uuid.New(), body)
	if rec.Code != 400 {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHRCreate_InvalidDateRange(t *testing.T) {
	h := newHandler(&mockHRBotSvc{}, &mockHRStore{})
	app := setupHR(h)

	body := fiber.Map{
		"chat_id":      uuid.New().String(),
		"request_type": "day_off",
		"start_date":   "2026-05-10",
		"end_date":     "2026-05-01",
	}
	rec := doJSON(app, "POST", "/bots/"+uuid.New().String()+"/hr/requests", uuid.New(), body)
	if rec.Code != 400 {
		t.Fatalf("expected 400 for inverted dates, got %d", rec.Code)
	}
}

func TestHRCreate_BotNotInstalled(t *testing.T) {
	h := newHandler(&mockHRBotSvc{
		checkBotScopeFn: func(_ context.Context, _, _ uuid.UUID, _ int64) error {
			return apperror.Forbidden("Bot not installed")
		},
	}, &mockHRStore{})
	app := setupHR(h)

	body := fiber.Map{
		"chat_id":      uuid.New().String(),
		"request_type": "vacation",
		"start_date":   "2026-05-01",
		"end_date":     "2026-05-10",
	}
	rec := doJSON(app, "POST", "/bots/"+uuid.New().String()+"/hr/requests", uuid.New(), body)
	if rec.Code != 403 {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestHRCreate_Unauthenticated(t *testing.T) {
	h := newHandler(&mockHRBotSvc{}, &mockHRStore{})
	app := setupHR(h)

	body := fiber.Map{
		"chat_id":      uuid.New().String(),
		"request_type": "vacation",
		"start_date":   "2026-05-01",
		"end_date":     "2026-05-10",
	}
	rec := doJSON(app, "POST", "/bots/"+uuid.New().String()+"/hr/requests", uuid.Nil, body)
	if rec.Code != 401 {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestHRDecide_OwnerApproves(t *testing.T) {
	ownerID := uuid.New()
	botID := uuid.New()
	reqID := uuid.New()

	h := newHandler(
		&mockHRBotSvc{
			getBotFn: func(_ context.Context, id uuid.UUID) (*model.Bot, error) {
				return &model.Bot{ID: id, OwnerID: ownerID}, nil
			},
		},
		&mockHRStore{
			getByIDFn: func(_ context.Context, id uuid.UUID) (*model.HRRequest, error) {
				return &model.HRRequest{ID: id, BotID: botID, Status: model.HRStatusPending}, nil
			},
			decideFn: func(_ context.Context, id, approverID uuid.UUID, approve bool, note string) (*model.HRRequest, error) {
				if !approve {
					t.Fatalf("expected approve=true")
				}
				if approverID != ownerID {
					t.Fatalf("approver mismatch")
				}
				return &model.HRRequest{ID: id, BotID: botID, Status: model.HRStatusApproved}, nil
			},
		},
	)
	app := setupHR(h)

	rec := doJSON(app, "PATCH",
		"/bots/"+botID.String()+"/hr/requests/"+reqID.String(),
		ownerID,
		fiber.Map{"decision": "approve"},
	)
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHRDecide_NonOwnerForbidden(t *testing.T) {
	ownerID := uuid.New()
	stranger := uuid.New()
	botID := uuid.New()

	h := newHandler(
		&mockHRBotSvc{
			getBotFn: func(_ context.Context, id uuid.UUID) (*model.Bot, error) {
				return &model.Bot{ID: id, OwnerID: ownerID}, nil
			},
		},
		&mockHRStore{},
	)
	app := setupHR(h)

	rec := doJSON(app, "PATCH",
		"/bots/"+botID.String()+"/hr/requests/"+uuid.New().String(),
		stranger,
		fiber.Map{"decision": "approve"},
	)
	if rec.Code != 403 {
		t.Fatalf("expected 403, got %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHRDecide_AlreadyFinal(t *testing.T) {
	ownerID := uuid.New()
	botID := uuid.New()
	reqID := uuid.New()

	h := newHandler(
		&mockHRBotSvc{
			getBotFn: func(_ context.Context, id uuid.UUID) (*model.Bot, error) {
				return &model.Bot{ID: id, OwnerID: ownerID}, nil
			},
		},
		&mockHRStore{
			getByIDFn: func(_ context.Context, id uuid.UUID) (*model.HRRequest, error) {
				return &model.HRRequest{ID: id, BotID: botID, Status: model.HRStatusApproved}, nil
			},
			decideFn: func(_ context.Context, _, _ uuid.UUID, _ bool, _ string) (*model.HRRequest, error) {
				return nil, model.ErrHRRequestAlreadyFinal
			},
		},
	)
	app := setupHR(h)

	rec := doJSON(app, "PATCH",
		"/bots/"+botID.String()+"/hr/requests/"+reqID.String(),
		ownerID,
		fiber.Map{"decision": "reject", "note": "duplicate"},
	)
	if rec.Code != 409 {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
}

func TestHRDecide_CrossBotRejected(t *testing.T) {
	ownerID := uuid.New()
	botA := uuid.New()
	botB := uuid.New()
	reqID := uuid.New()

	h := newHandler(
		&mockHRBotSvc{
			getBotFn: func(_ context.Context, id uuid.UUID) (*model.Bot, error) {
				return &model.Bot{ID: id, OwnerID: ownerID}, nil
			},
		},
		&mockHRStore{
			getByIDFn: func(_ context.Context, id uuid.UUID) (*model.HRRequest, error) {
				// Request belongs to botA, but caller queries via botB.
				return &model.HRRequest{ID: id, BotID: botA, Status: model.HRStatusPending}, nil
			},
		},
	)
	app := setupHR(h)

	rec := doJSON(app, "PATCH",
		"/bots/"+botB.String()+"/hr/requests/"+reqID.String(),
		ownerID,
		fiber.Map{"decision": "approve"},
	)
	if rec.Code != 404 {
		t.Fatalf("expected 404 for cross-bot request, got %d", rec.Code)
	}
}

func TestHRList_OwnerSeesAll_NonOwnerSeesOwn(t *testing.T) {
	ownerID := uuid.New()
	stranger := uuid.New()
	botID := uuid.New()

	var receivedFilter store.HRRequestFilter
	svc := &mockHRBotSvc{
		getBotFn: func(_ context.Context, id uuid.UUID) (*model.Bot, error) {
			return &model.Bot{ID: id, OwnerID: ownerID}, nil
		},
	}
	st := &mockHRStore{
		listFn: func(_ context.Context, f store.HRRequestFilter) ([]model.HRRequest, error) {
			receivedFilter = f
			return []model.HRRequest{}, nil
		},
	}
	h := newHandler(svc, st)
	app := setupHR(h)

	// Owner call — no user scoping.
	rec := doJSON(app, "GET", "/bots/"+botID.String()+"/hr/requests", ownerID, nil)
	if rec.Code != 200 {
		t.Fatalf("owner: expected 200, got %d", rec.Code)
	}
	if receivedFilter.UserID != nil {
		t.Fatalf("owner: expected no user filter, got %v", receivedFilter.UserID)
	}

	// Stranger call — auto-scoped to own user.
	rec = doJSON(app, "GET", "/bots/"+botID.String()+"/hr/requests", stranger, nil)
	if rec.Code != 200 {
		t.Fatalf("stranger: expected 200, got %d", rec.Code)
	}
	if receivedFilter.UserID == nil || *receivedFilter.UserID != stranger {
		t.Fatalf("stranger: expected user filter=%s, got %v", stranger, receivedFilter.UserID)
	}
}
