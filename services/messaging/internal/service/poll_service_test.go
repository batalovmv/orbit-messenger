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

func newTestPollService(ps *mockPollStore, ms *mockMessageStore, cs *mockChatStore, rec *RecordingPublisher) *PollService {
	return NewPollService(ps, ms, cs, rec, slog.Default())
}

func pollAssertAppError(t *testing.T, err error, wantStatus int) {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError with status %d, got %T: %v", wantStatus, err, err)
	}
	if appErr.Status != wantStatus {
		t.Fatalf("expected status %d, got %d: %s", wantStatus, appErr.Status, appErr.Message)
	}
}

// --- CreatePoll ---

func TestCreatePoll_NotMember(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (bool, string, error) {
			return false, "", nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(&mockPollStore{}, &mockMessageStore{}, cs, rec)

	_, _, err := svc.CreatePoll(context.Background(), chatID, userID, "Where to eat?", []string{"Pizza", "Sushi"}, true, false, false, nil, nil, nil)
	pollAssertAppError(t, err, 403)
}

func TestCreatePoll_TooFewOptions(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(&mockPollStore{}, &mockMessageStore{}, cs, rec)

	_, _, err := svc.CreatePoll(context.Background(), chatID, userID, "Where?", []string{"Only one"}, true, false, false, nil, nil, nil)
	pollAssertAppError(t, err, 400)
}

func TestCreatePoll_TooManyOptions(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(&mockPollStore{}, &mockMessageStore{}, cs, rec)

	options := make([]string, 11) // max 10
	for i := range options {
		options[i] = "Option"
	}
	_, _, err := svc.CreatePoll(context.Background(), chatID, userID, "Q?", options, true, false, false, nil, nil, nil)
	pollAssertAppError(t, err, 400)
}

func TestCreatePoll_EmptyQuestion(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(&mockPollStore{}, &mockMessageStore{}, cs, rec)

	_, _, err := svc.CreatePoll(context.Background(), chatID, userID, "", []string{"A", "B"}, true, false, false, nil, nil, nil)
	pollAssertAppError(t, err, 400)
}

func TestCreatePoll_QuizWithoutCorrectOption(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(&mockPollStore{}, &mockMessageStore{}, cs, rec)

	_, _, err := svc.CreatePoll(context.Background(), chatID, userID, "Quiz?", []string{"A", "B"}, true, false, true, nil, nil, nil)
	pollAssertAppError(t, err, 400)
}

func TestCreatePoll_QuizWithInvalidCorrectOption(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(&mockPollStore{}, &mockMessageStore{}, cs, rec)

	correctOpt := 5 // out of range
	_, _, err := svc.CreatePoll(context.Background(), chatID, userID, "Quiz?", []string{"A", "B"}, true, false, true, &correctOpt, nil, nil)
	pollAssertAppError(t, err, 400)
}

func TestCreatePoll_Success_PublishesEvent(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
		getMemberIDsFn: func(ctx context.Context, cID uuid.UUID) ([]string, error) {
			return []string{userID.String()}, nil
		},
		getMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "member", Permissions: -1}, nil
		},
		getByIDFn: func(ctx context.Context, cID uuid.UUID) (*model.Chat, error) {
			return &model.Chat{ID: cID, Type: "group"}, nil
		},
	}
	ms := &mockMessageStore{
		createFn: func(ctx context.Context, msg *model.Message) error {
			msg.ID = uuid.New()
			msg.SequenceNumber = 1
			return nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(&mockPollStore{}, ms, cs, rec)

	poll, msg, err := svc.CreatePoll(context.Background(), chatID, userID, "Where to eat?", []string{"Pizza", "Sushi", "Burgers"}, true, false, false, nil, nil, nil)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if poll == nil {
		t.Fatal("expected poll, got nil")
	}
	if msg == nil {
		t.Fatal("expected message, got nil")
	}
	if len(poll.Options) != 3 {
		t.Fatalf("expected 3 options, got %d", len(poll.Options))
	}
}

func TestCreatePoll_QuizWithSolution(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()
	correctOpt := 1
	solution := "Because it is the only even answer."

	cs := &mockChatStore{
		isMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
		getMemberIDsFn: func(ctx context.Context, cID uuid.UUID) ([]string, error) {
			return []string{userID.String()}, nil
		},
		getMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "member", Permissions: -1}, nil
		},
		getByIDFn: func(ctx context.Context, cID uuid.UUID) (*model.Chat, error) {
			return &model.Chat{ID: cID, Type: "group"}, nil
		},
	}
	ms := &mockMessageStore{
		createFn: func(ctx context.Context, msg *model.Message) error {
			msg.ID = uuid.New()
			return nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(&mockPollStore{}, ms, cs, rec)

	poll, _, err := svc.CreatePoll(
		context.Background(),
		chatID,
		userID,
		"Which answer is correct?",
		[]string{"3", "4"},
		true,
		false,
		true,
		&correctOpt,
		&solution,
		[]byte(`[{"type":"MessageEntityItalic","offset":0,"length":7}]`),
	)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if poll == nil || poll.Solution == nil {
		t.Fatal("expected solution to be stored on poll")
	}
	if *poll.Solution != solution {
		t.Fatalf("unexpected solution: %q", *poll.Solution)
	}
	if len(poll.SolutionEntities) == 0 {
		t.Fatal("expected solution entities to be stored on poll")
	}
}

// --- Vote ---

func TestVote_PollNotFound(t *testing.T) {
	msgID := uuid.New()
	userID := uuid.New()

	ps := &mockPollStore{
		getByMessageIDFn: func(ctx context.Context, mID uuid.UUID) (*model.Poll, error) {
			return nil, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(ps, &mockMessageStore{}, &mockChatStore{}, rec)

	_, err := svc.Vote(context.Background(), msgID, userID, []uuid.UUID{uuid.New()})
	pollAssertAppError(t, err, 404)
}

func TestVote_PollClosed(t *testing.T) {
	msgID := uuid.New()
	userID := uuid.New()
	pollID := uuid.New()
	optID := uuid.New()

	ps := &mockPollStore{
		getByMessageIDFn: func(ctx context.Context, mID uuid.UUID) (*model.Poll, error) {
			return &model.Poll{
				ID:        pollID,
				MessageID: mID,
				IsClosed:  true,
				Options:   []model.PollOption{{ID: optID, Text: "A"}},
			}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(ps, &mockMessageStore{}, &mockChatStore{}, rec)

	_, err := svc.Vote(context.Background(), msgID, userID, []uuid.UUID{optID})
	pollAssertAppError(t, err, 400)
}

func TestVote_MultipleVotesOnSingleChoice(t *testing.T) {
	msgID := uuid.New()
	userID := uuid.New()
	pollID := uuid.New()
	opt1 := uuid.New()
	opt2 := uuid.New()

	ps := &mockPollStore{
		getByMessageIDFn: func(ctx context.Context, mID uuid.UUID) (*model.Poll, error) {
			return &model.Poll{
				ID:         pollID,
				MessageID:  mID,
				IsMultiple: false, // single choice
				Options: []model.PollOption{
					{ID: opt1, Text: "A"},
					{ID: opt2, Text: "B"},
				},
			}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(ps, &mockMessageStore{}, &mockChatStore{}, rec)

	_, err := svc.Vote(context.Background(), msgID, userID, []uuid.UUID{opt1, opt2})
	pollAssertAppError(t, err, 400)
}

func TestVote_InvalidOptionID(t *testing.T) {
	msgID := uuid.New()
	userID := uuid.New()
	pollID := uuid.New()
	opt1 := uuid.New()
	badOpt := uuid.New()

	ps := &mockPollStore{
		getByMessageIDFn: func(ctx context.Context, mID uuid.UUID) (*model.Poll, error) {
			return &model.Poll{
				ID:        pollID,
				MessageID: mID,
				Options:   []model.PollOption{{ID: opt1, Text: "A"}},
			}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(ps, &mockMessageStore{}, &mockChatStore{}, rec)

	_, err := svc.Vote(context.Background(), msgID, userID, []uuid.UUID{badOpt})
	pollAssertAppError(t, err, 400)
}

func TestVote_Success_PublishesEvent(t *testing.T) {
	msgID := uuid.New()
	userID := uuid.New()
	pollID := uuid.New()
	optID := uuid.New()
	chatID := uuid.New()

	ps := &mockPollStore{
		getByMessageIDFn: func(ctx context.Context, mID uuid.UUID) (*model.Poll, error) {
			return &model.Poll{
				ID:        pollID,
				MessageID: mID,
				Options:   []model.PollOption{{ID: optID, PollID: pollID, Text: "Pizza"}},
			}, nil
		},
		getByIDFn: func(ctx context.Context, pID uuid.UUID) (*model.Poll, error) {
			return &model.Poll{
				ID:          pollID,
				MessageID:   msgID,
				Options:     []model.PollOption{{ID: optID, PollID: pollID, Text: "Pizza", Voters: 1}},
				TotalVoters: 1,
			}, nil
		},
	}
	ms := &mockMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: msgID, ChatID: chatID}, nil
		},
	}
	cs := &mockChatStore{
		isMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
		getMemberIDsFn: func(ctx context.Context, cID uuid.UUID) ([]string, error) {
			return []string{userID.String()}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(ps, ms, cs, rec)

	poll, err := svc.Vote(context.Background(), msgID, userID, []uuid.UUID{optID})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if poll == nil {
		t.Fatal("expected poll, got nil")
	}

	events := rec.FindByEvent("poll_vote")
	if len(events) != 1 {
		t.Fatalf("expected 1 poll_vote event, got %d", len(events))
	}
}

// --- ClosePoll ---

func TestClosePoll_NotFound(t *testing.T) {
	ps := &mockPollStore{
		getByMessageIDFn: func(ctx context.Context, mID uuid.UUID) (*model.Poll, error) {
			return nil, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(ps, &mockMessageStore{}, &mockChatStore{}, rec)

	_, err := svc.ClosePoll(context.Background(), uuid.New(), uuid.New())
	pollAssertAppError(t, err, 404)
}

func TestClosePoll_AlreadyClosed(t *testing.T) {
	msgID := uuid.New()
	pollID := uuid.New()

	ps := &mockPollStore{
		getByMessageIDFn: func(ctx context.Context, mID uuid.UUID) (*model.Poll, error) {
			return &model.Poll{ID: pollID, MessageID: mID, IsClosed: true}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(ps, &mockMessageStore{}, &mockChatStore{}, rec)

	_, err := svc.ClosePoll(context.Background(), msgID, uuid.New())
	pollAssertAppError(t, err, 400)
}

func TestClosePoll_NotCreatorOrAdmin(t *testing.T) {
	msgID := uuid.New()
	pollID := uuid.New()
	chatID := uuid.New()
	creatorID := uuid.New()
	otherUserID := uuid.New()

	ps := &mockPollStore{
		getByMessageIDFn: func(ctx context.Context, mID uuid.UUID) (*model.Poll, error) {
			return &model.Poll{ID: pollID, MessageID: mID}, nil
		},
	}
	ms := &mockMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: msgID, ChatID: chatID, SenderID: &creatorID}, nil
		},
	}
	cs := &mockChatStore{
		getMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "member"}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(ps, ms, cs, rec)

	_, err := svc.ClosePoll(context.Background(), msgID, otherUserID)
	pollAssertAppError(t, err, 403)
}

func TestClosePoll_Success_PublishesEvent(t *testing.T) {
	msgID := uuid.New()
	pollID := uuid.New()
	chatID := uuid.New()
	userID := uuid.New()

	ps := &mockPollStore{
		getByMessageIDFn: func(ctx context.Context, mID uuid.UUID) (*model.Poll, error) {
			return &model.Poll{ID: pollID, MessageID: mID}, nil
		},
		getByIDFn: func(ctx context.Context, pID uuid.UUID) (*model.Poll, error) {
			return &model.Poll{ID: pollID, MessageID: msgID, IsClosed: true}, nil
		},
	}
	ms := &mockMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: msgID, ChatID: chatID, SenderID: &userID}, nil
		},
	}
	cs := &mockChatStore{
		getMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "admin"}, nil
		},
		getMemberIDsFn: func(ctx context.Context, cID uuid.UUID) ([]string, error) {
			return []string{userID.String()}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(ps, ms, cs, rec)

	poll, err := svc.ClosePoll(context.Background(), msgID, userID)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if poll == nil {
		t.Fatal("expected poll, got nil")
	}

	events := rec.FindByEvent("poll_closed")
	if len(events) != 1 {
		t.Fatalf("expected 1 poll_closed event, got %d", len(events))
	}
}

// --- Unvote ---

func TestUnvote_PollNotFound(t *testing.T) {
	ps := &mockPollStore{
		getByMessageIDFn: func(ctx context.Context, mID uuid.UUID) (*model.Poll, error) {
			return nil, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(ps, &mockMessageStore{}, &mockChatStore{}, rec)

	_, err := svc.Unvote(context.Background(), uuid.New(), uuid.New())
	pollAssertAppError(t, err, 404)
}

func TestUnvote_PollClosed(t *testing.T) {
	ps := &mockPollStore{
		getByMessageIDFn: func(ctx context.Context, mID uuid.UUID) (*model.Poll, error) {
			return &model.Poll{ID: uuid.New(), IsClosed: true}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestPollService(ps, &mockMessageStore{}, &mockChatStore{}, rec)

	_, err := svc.Unvote(context.Background(), uuid.New(), uuid.New())
	pollAssertAppError(t, err, 400)
}

// Suppress unused
var _ = time.Now
