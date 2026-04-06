package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

func newPollApp(ps *mockPollStore, ms *mockMessageStore, cs *mockChatStore) *fiber.App {
	app := fiber.New()
	nats := service.NewNoopNATSPublisher()
	svc := service.NewPollService(ps, ms, cs, nats, slog.Default())
	h := NewPollHandler(svc, slog.Default())
	h.Register(app)
	return app
}

// --- Vote ---

func TestVote_MissingUserID(t *testing.T) {
	app := newPollApp(&mockPollStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/messages/"+uuid.New().String()+"/poll/vote",
		bytes.NewBufferString(`{"option_ids":["`+uuid.New().String()+`"]}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestVote_InvalidMessageID(t *testing.T) {
	app := newPollApp(&mockPollStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/messages/bad-id/poll/vote",
		bytes.NewBufferString(`{"option_ids":["`+uuid.New().String()+`"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestVote_EmptyOptionIDs(t *testing.T) {
	app := newPollApp(&mockPollStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/messages/"+uuid.New().String()+"/poll/vote",
		bytes.NewBufferString(`{"option_ids":[]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestVote_InvalidOptionID(t *testing.T) {
	app := newPollApp(&mockPollStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/messages/"+uuid.New().String()+"/poll/vote",
		bytes.NewBufferString(`{"option_ids":["not-a-uuid"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- Unvote ---

func TestUnvote_MissingUserID(t *testing.T) {
	app := newPollApp(&mockPollStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodDelete, "/messages/"+uuid.New().String()+"/poll/vote", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestUnvote_InvalidMessageID(t *testing.T) {
	app := newPollApp(&mockPollStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodDelete, "/messages/bad-id/poll/vote", nil)
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- ClosePoll ---

func TestClosePoll_MissingUserID(t *testing.T) {
	app := newPollApp(&mockPollStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/messages/"+uuid.New().String()+"/poll/close", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestClosePoll_InvalidMessageID(t *testing.T) {
	app := newPollApp(&mockPollStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/messages/bad-id/poll/close", nil)
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGetVoters_UsesCursorPagination(t *testing.T) {
	msgID := uuid.New()
	userID := uuid.New()
	pollID := uuid.New()
	optionID := uuid.New()
	chatID := uuid.New()

	var gotLimit int
	var gotCursor string

	app := newPollApp(
		&mockPollStore{
			getByMessageIDFn: func(_ context.Context, mID uuid.UUID) (*model.Poll, error) {
				return &model.Poll{
					ID:        pollID,
					MessageID: mID,
					Options:   []model.PollOption{{ID: optionID}},
				}, nil
			},
			getVotersFn: func(_ context.Context, pID, oID uuid.UUID, limit int, cursor string) ([]model.PollVote, string, bool, error) {
				gotLimit = limit
				gotCursor = cursor
				return []model.PollVote{{PollID: pID, OptionID: oID, UserID: userID}}, "next-cursor", true, nil
			},
		},
		&mockMessageStore{
			getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Message, error) {
				return &model.Message{ID: id, ChatID: chatID}, nil
			},
		},
		&mockChatStore{
			isMemberFn: func(_ context.Context, cID, uID uuid.UUID) (bool, string, error) {
				return true, "member", nil
			},
		},
	)

	req, _ := http.NewRequest(
		http.MethodGet,
		"/messages/"+msgID.String()+"/poll/voters?option_id="+optionID.String()+"&limit=25&cursor=cursor-token",
		nil,
	)
	req.Header.Set("X-User-ID", userID.String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Data    []model.PollVote `json:"data"`
		Cursor  string           `json:"cursor"`
		HasMore bool             `json:"has_more"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if gotLimit != 25 {
		t.Fatalf("expected limit 25, got %d", gotLimit)
	}
	if gotCursor != "cursor-token" {
		t.Fatalf("expected cursor token to be forwarded, got %q", gotCursor)
	}
	if body.Cursor != "next-cursor" {
		t.Fatalf("expected next cursor, got %q", body.Cursor)
	}
	if !body.HasMore {
		t.Fatal("expected has_more to be true")
	}
	if len(body.Data) != 1 {
		t.Fatalf("expected 1 voter, got %d", len(body.Data))
	}
}
