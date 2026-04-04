package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

func newTestScheduledService(
	ss *mockScheduledMessageStore,
	ms *mockMessageStore,
	ps *mockPollStore,
	cs *mockChatStore,
	rec *RecordingPublisher,
) *ScheduledMessageService {
	return NewScheduledMessageService(ss, ms, ps, cs, nil, rec, nil, slog.Default())
}

func schedAssertAppError(t *testing.T, err error, wantStatus int) {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError with status %d, got %T: %v", wantStatus, err, err)
	}
	if appErr.Status != wantStatus {
		t.Fatalf("expected status %d, got %d: %s", wantStatus, appErr.Status, appErr.Message)
	}
}

// --- Schedule ---

func TestSchedule_NotMember(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()
	future := time.Now().Add(24 * time.Hour)

	cs := &mockChatStore{
		getByIDFn: func(ctx context.Context, cID uuid.UUID) (*model.Chat, error) {
			return &model.Chat{ID: cID, Type: "group"}, nil
		},
		getMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (*model.ChatMember, error) {
			return nil, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestScheduledService(&mockScheduledMessageStore{}, &mockMessageStore{}, nil, cs, rec)

	_, err := svc.Schedule(context.Background(), chatID, userID, ScheduleMessageInput{
		Content:     "hello",
		Type:        "text",
		ScheduledAt: future,
	})
	schedAssertAppError(t, err, 403)
}

func TestSchedule_PastTime(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()
	past := time.Now().Add(-1 * time.Hour)

	cs := &mockChatStore{
		getByIDFn: func(ctx context.Context, cID uuid.UUID) (*model.Chat, error) {
			return &model.Chat{ID: cID, Type: "group", DefaultPermissions: 1}, nil
		},
		getMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{UserID: uID, Role: "member", Permissions: -1}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestScheduledService(&mockScheduledMessageStore{}, &mockMessageStore{}, nil, cs, rec)

	_, err := svc.Schedule(context.Background(), chatID, userID, ScheduleMessageInput{
		Content:     "hello",
		Type:        "text",
		ScheduledAt: past,
	})
	schedAssertAppError(t, err, 400)
}

func TestSchedule_EmptyContent(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()
	future := time.Now().Add(24 * time.Hour)

	cs := &mockChatStore{
		getByIDFn: func(ctx context.Context, cID uuid.UUID) (*model.Chat, error) {
			return &model.Chat{ID: cID, Type: "group", DefaultPermissions: 1}, nil
		},
		getMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{UserID: uID, Role: "member", Permissions: -1}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestScheduledService(&mockScheduledMessageStore{}, &mockMessageStore{}, nil, cs, rec)

	_, err := svc.Schedule(context.Background(), chatID, userID, ScheduleMessageInput{
		Content:     "",
		Type:        "text",
		ScheduledAt: future,
	})
	schedAssertAppError(t, err, 400)
}

func TestSchedule_Success(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()
	future := time.Now().Add(24 * time.Hour)

	cs := &mockChatStore{
		getByIDFn: func(ctx context.Context, cID uuid.UUID) (*model.Chat, error) {
			return &model.Chat{ID: cID, Type: "group", DefaultPermissions: 1}, nil
		},
		getMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{UserID: uID, Role: "member", Permissions: -1}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestScheduledService(&mockScheduledMessageStore{}, &mockMessageStore{}, nil, cs, rec)

	msg, err := svc.Schedule(context.Background(), chatID, userID, ScheduleMessageInput{
		Content:     "Happy birthday!",
		Type:        "text",
		ScheduledAt: future,
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if msg == nil {
		t.Fatal("expected scheduled message, got nil")
	}
	if msg.ChatID != chatID {
		t.Fatalf("expected chatID %s, got %s", chatID, msg.ChatID)
	}
}

// --- Edit ---

func TestEditScheduled_NotFound(t *testing.T) {
	ss := &mockScheduledMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.ScheduledMessage, error) {
			return nil, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestScheduledService(ss, &mockMessageStore{}, nil, &mockChatStore{}, rec)

	content := "updated"
	_, err := svc.Edit(context.Background(), uuid.New(), uuid.New(), &content, nil, nil)
	schedAssertAppError(t, err, 404)
}

func TestEditScheduled_NotOwner(t *testing.T) {
	msgID := uuid.New()
	ownerID := uuid.New()
	otherID := uuid.New()

	ss := &mockScheduledMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.ScheduledMessage, error) {
			return &model.ScheduledMessage{ID: msgID, SenderID: ownerID}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestScheduledService(ss, &mockMessageStore{}, nil, &mockChatStore{}, rec)

	content := "updated"
	_, err := svc.Edit(context.Background(), msgID, otherID, &content, nil, nil)
	schedAssertAppError(t, err, 403)
}

func TestEditScheduled_AlreadySent(t *testing.T) {
	msgID := uuid.New()
	userID := uuid.New()

	ss := &mockScheduledMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.ScheduledMessage, error) {
			return &model.ScheduledMessage{ID: msgID, SenderID: userID, IsSent: true}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestScheduledService(ss, &mockMessageStore{}, nil, &mockChatStore{}, rec)

	content := "updated"
	_, err := svc.Edit(context.Background(), msgID, userID, &content, nil, nil)
	schedAssertAppError(t, err, 400)
}

func TestEditScheduled_RescheduleToPast(t *testing.T) {
	msgID := uuid.New()
	userID := uuid.New()
	future := time.Now().Add(24 * time.Hour)

	ss := &mockScheduledMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.ScheduledMessage, error) {
			return &model.ScheduledMessage{ID: msgID, SenderID: userID, ScheduledAt: future}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestScheduledService(ss, &mockMessageStore{}, nil, &mockChatStore{}, rec)

	past := time.Now().Add(-1 * time.Hour)
	_, err := svc.Edit(context.Background(), msgID, userID, nil, nil, &past)
	schedAssertAppError(t, err, 400)
}

// --- Delete ---

func TestDeleteScheduled_NotFound(t *testing.T) {
	ss := &mockScheduledMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.ScheduledMessage, error) {
			return nil, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestScheduledService(ss, &mockMessageStore{}, nil, &mockChatStore{}, rec)

	err := svc.Delete(context.Background(), uuid.New(), uuid.New())
	schedAssertAppError(t, err, 404)
}

func TestDeleteScheduled_NotOwner(t *testing.T) {
	msgID := uuid.New()
	ownerID := uuid.New()
	otherID := uuid.New()

	ss := &mockScheduledMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.ScheduledMessage, error) {
			return &model.ScheduledMessage{ID: msgID, SenderID: ownerID}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestScheduledService(ss, &mockMessageStore{}, nil, &mockChatStore{}, rec)

	err := svc.Delete(context.Background(), msgID, otherID)
	schedAssertAppError(t, err, 403)
}

// --- SendNow ---

func TestSendNow_NotFound(t *testing.T) {
	ss := &mockScheduledMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.ScheduledMessage, error) {
			return nil, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestScheduledService(ss, &mockMessageStore{}, nil, &mockChatStore{}, rec)

	_, err := svc.SendNow(context.Background(), uuid.New(), uuid.New())
	schedAssertAppError(t, err, 404)
}

func TestSendNow_AlreadySent(t *testing.T) {
	msgID := uuid.New()
	userID := uuid.New()

	ss := &mockScheduledMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.ScheduledMessage, error) {
			return &model.ScheduledMessage{ID: msgID, SenderID: userID, IsSent: true}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestScheduledService(ss, &mockMessageStore{}, nil, &mockChatStore{}, rec)

	_, err := svc.SendNow(context.Background(), msgID, userID)
	schedAssertAppError(t, err, 400)
}

func TestSendNow_NotOwner(t *testing.T) {
	msgID := uuid.New()
	ownerID := uuid.New()
	otherID := uuid.New()

	ss := &mockScheduledMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.ScheduledMessage, error) {
			return &model.ScheduledMessage{ID: msgID, SenderID: ownerID}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestScheduledService(ss, &mockMessageStore{}, nil, &mockChatStore{}, rec)

	_, err := svc.SendNow(context.Background(), msgID, otherID)
	schedAssertAppError(t, err, 403)
}

// --- DeliverPending ---

func TestDeliverPending_NoPendingMessages(t *testing.T) {
	ss := &mockScheduledMessageStore{
		listPendingFn: func(ctx context.Context, now time.Time) ([]model.ScheduledMessage, error) {
			return nil, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestScheduledService(ss, &mockMessageStore{}, nil, &mockChatStore{}, rec)

	count, err := svc.DeliverPending(context.Background())
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 delivered, got %d", count)
	}
}

func TestDeliverPending_DeliversAndMarksSent(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()
	msgID := uuid.New()
	content := "Happy birthday!"

	ss := &mockScheduledMessageStore{
		listPendingFn: func(ctx context.Context, now time.Time) ([]model.ScheduledMessage, error) {
			return []model.ScheduledMessage{
				{ID: msgID, ChatID: chatID, SenderID: userID, Content: &content, Type: "text"},
			}, nil
		},
		markSentFn: func(ctx context.Context, id uuid.UUID) error {
			if id != msgID {
				t.Fatalf("expected markSent for %s, got %s", msgID, id)
			}
			return nil
		},
	}
	ms := &mockMessageStore{
		createFn: func(ctx context.Context, msg *model.Message) error {
			msg.ID = uuid.New()
			msg.SequenceNumber = 1
			return nil
		},
	}
	cs := &mockChatStore{
		getByIDFn: func(ctx context.Context, cID uuid.UUID) (*model.Chat, error) {
			return &model.Chat{ID: cID, Type: "group", DefaultPermissions: 1}, nil
		},
		getMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{UserID: uID, Role: "member", Permissions: -1}, nil
		},
		getMemberIDsFn: func(ctx context.Context, cID uuid.UUID) ([]string, error) {
			return []string{userID.String()}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestScheduledService(ss, ms, nil, cs, rec)

	count, err := svc.DeliverPending(context.Background())
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 delivered, got %d", count)
	}

	events := rec.FindByEvent("new_message")
	if len(events) != 1 {
		t.Fatalf("expected 1 new_message event, got %d", len(events))
	}
}

func TestSendNow_ScheduledPollCreatesPoll(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()
	scheduledID := uuid.New()
	messageID := uuid.New()

	var createdPoll *model.Poll
	ss := &mockScheduledMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.ScheduledMessage, error) {
			if id != scheduledID {
				t.Fatalf("expected scheduled id %s, got %s", scheduledID, id)
			}
			return &model.ScheduledMessage{
				ID:       scheduledID,
				ChatID:   chatID,
				SenderID: userID,
				Type:     "poll",
				Content:  strPtrOrNil("Where?"),
				PollPayload: &model.ScheduledPollPayload{
					Question:    "Where?",
					Options:     []string{"Office", "Cafe"},
					IsAnonymous: true,
				},
			}, nil
		},
		markSentFn: func(ctx context.Context, id uuid.UUID) error {
			if id != scheduledID {
				t.Fatalf("expected markSent for %s, got %s", scheduledID, id)
			}
			return nil
		},
	}
	ms := &mockMessageStore{
		createFn: func(ctx context.Context, msg *model.Message) error {
			msg.ID = messageID
			msg.SequenceNumber = 10
			return nil
		},
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Message, error) {
			return &model.Message{
				ID:             messageID,
				ChatID:         chatID,
				SenderID:       &userID,
				Type:           "poll",
				SequenceNumber: 10,
				Content:        strPtrOrNil("Where?"),
			}, nil
		},
	}
	ps := &mockPollStore{
		createFn: func(ctx context.Context, poll *model.Poll) error {
			createdPoll = poll
			poll.ID = uuid.New()
			for i := range poll.Options {
				poll.Options[i].ID = uuid.New()
				poll.Options[i].PollID = poll.ID
			}
			return nil
		},
	}
	cs := &mockChatStore{
		getByIDFn: func(ctx context.Context, cID uuid.UUID) (*model.Chat, error) {
			return &model.Chat{ID: cID, Type: "group", DefaultPermissions: 1}, nil
		},
		getMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{UserID: uID, Role: "member", Permissions: -1}, nil
		},
		getMemberIDsFn: func(ctx context.Context, cID uuid.UUID) ([]string, error) {
			return []string{userID.String()}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestScheduledService(ss, ms, ps, cs, rec)

	delivered, err := svc.SendNow(context.Background(), scheduledID, userID)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if delivered == nil || delivered.Poll == nil {
		t.Fatal("expected delivered poll message")
	}
	if createdPoll == nil {
		t.Fatal("expected poll store Create to be called")
	}
	if createdPoll.Question != "Where?" {
		t.Fatalf("expected poll question to match, got %q", createdPoll.Question)
	}

	events := rec.FindByEvent("new_message")
	if len(events) != 1 {
		t.Fatalf("expected 1 new_message event, got %d", len(events))
	}
}
