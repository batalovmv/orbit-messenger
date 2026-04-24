// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

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

func newTestReactionService(rs *mockReactionStore, ms *mockMessageStore, cs *mockChatStore, rec *RecordingPublisher) *ReactionService {
	return NewReactionService(rs, ms, cs, rec, slog.Default())
}

func reactionAssertAppError(t *testing.T, err error, wantStatus int) {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError with status %d, got %T: %v", wantStatus, err, err)
	}
	if appErr.Status != wantStatus {
		t.Fatalf("expected status %d, got %d: %s", wantStatus, appErr.Status, appErr.Message)
	}
}

// --- AddReaction ---

func TestAddReaction_NotMember(t *testing.T) {
	chatID := uuid.New()
	msgID := uuid.New()
	userID := uuid.New()

	ms := &mockMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: msgID, ChatID: chatID}, nil
		},
	}
	cs := &mockChatStore{
		isMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (bool, string, error) {
			return false, "", nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestReactionService(&mockReactionStore{}, ms, cs, rec)

	err := svc.AddReaction(context.Background(), msgID, userID, "👍")
	reactionAssertAppError(t, err, 403)
}

func TestAddReaction_MessageNotFound(t *testing.T) {
	ms := &mockMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Message, error) {
			return nil, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestReactionService(&mockReactionStore{}, ms, &mockChatStore{}, rec)

	err := svc.AddReaction(context.Background(), uuid.New(), uuid.New(), "👍")
	reactionAssertAppError(t, err, 404)
}

func TestAddReaction_ReactionsDisabled(t *testing.T) {
	chatID := uuid.New()
	msgID := uuid.New()
	userID := uuid.New()

	ms := &mockMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: msgID, ChatID: chatID}, nil
		},
	}
	cs := &mockChatStore{
		isMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	}
	rs := &mockReactionStore{
		getAvailableReactionsFn: func(ctx context.Context, cID uuid.UUID) (*model.ChatAvailableReactions, error) {
			return &model.ChatAvailableReactions{ChatID: cID, Mode: "none"}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestReactionService(rs, ms, cs, rec)

	err := svc.AddReaction(context.Background(), msgID, userID, "👍")
	reactionAssertAppError(t, err, 403)
}

func TestAddReaction_EmojiNotInAllowedList(t *testing.T) {
	chatID := uuid.New()
	msgID := uuid.New()
	userID := uuid.New()

	ms := &mockMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: msgID, ChatID: chatID}, nil
		},
	}
	cs := &mockChatStore{
		isMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	}
	rs := &mockReactionStore{
		getAvailableReactionsFn: func(ctx context.Context, cID uuid.UUID) (*model.ChatAvailableReactions, error) {
			return &model.ChatAvailableReactions{ChatID: cID, Mode: "selected", AllowedEmojis: []string{"❤️", "🎉"}}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestReactionService(rs, ms, cs, rec)

	err := svc.AddReaction(context.Background(), msgID, userID, "👍")
	reactionAssertAppError(t, err, 400)
}

func TestAddReaction_Success_PublishesEvent(t *testing.T) {
	chatID := uuid.New()
	msgID := uuid.New()
	userID := uuid.New()

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
	rs := &mockReactionStore{
		getAvailableReactionsFn: func(ctx context.Context, cID uuid.UUID) (*model.ChatAvailableReactions, error) {
			return &model.ChatAvailableReactions{ChatID: cID, Mode: "all"}, nil
		},
		replaceUserReactionFn: func(ctx context.Context, messageID, reactingUserID uuid.UUID, emoji string) error {
			if messageID != msgID {
				t.Fatalf("unexpected message id: %s", messageID)
			}
			if reactingUserID != userID {
				t.Fatalf("unexpected user id: %s", reactingUserID)
			}
			if emoji != "👍" {
				t.Fatalf("unexpected emoji: %s", emoji)
			}
			return nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestReactionService(rs, ms, cs, rec)

	err := svc.AddReaction(context.Background(), msgID, userID, "👍")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	events := rec.FindByEvent("reaction_added")
	if len(events) != 1 {
		t.Fatalf("expected 1 reaction_added event, got %d", len(events))
	}
}

// --- RemoveReaction ---

func TestRemoveReaction_NotMember(t *testing.T) {
	chatID := uuid.New()
	msgID := uuid.New()
	userID := uuid.New()

	ms := &mockMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: msgID, ChatID: chatID}, nil
		},
	}
	cs := &mockChatStore{
		isMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (bool, string, error) {
			return false, "", nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestReactionService(&mockReactionStore{}, ms, cs, rec)

	err := svc.RemoveReaction(context.Background(), msgID, userID, "👍")
	reactionAssertAppError(t, err, 403)
}

func TestRemoveReaction_Success_PublishesEvent(t *testing.T) {
	chatID := uuid.New()
	msgID := uuid.New()
	userID := uuid.New()

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
	svc := newTestReactionService(&mockReactionStore{}, ms, cs, rec)

	err := svc.RemoveReaction(context.Background(), msgID, userID, "👍")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	events := rec.FindByEvent("reaction_removed")
	if len(events) != 1 {
		t.Fatalf("expected 1 reaction_removed event, got %d", len(events))
	}
}

// --- SetAvailableReactions ---

func TestSetAvailableReactions_NotAdmin(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
		getMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "member"}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestReactionService(&mockReactionStore{}, &mockMessageStore{}, cs, rec)

	err := svc.SetAvailableReactions(context.Background(), chatID, userID, "none", nil)
	reactionAssertAppError(t, err, 403)
}

func TestSetAvailableReactions_InvalidMode(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (bool, string, error) {
			return true, "admin", nil
		},
		getMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "admin"}, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestReactionService(&mockReactionStore{}, &mockMessageStore{}, cs, rec)

	err := svc.SetAvailableReactions(context.Background(), chatID, userID, "invalid-mode", nil)
	reactionAssertAppError(t, err, 400)
}

// --- ListReactions ---

func TestListReactions_NotMember(t *testing.T) {
	chatID := uuid.New()
	msgID := uuid.New()
	userID := uuid.New()

	ms := &mockMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: msgID, ChatID: chatID}, nil
		},
	}
	cs := &mockChatStore{
		isMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (bool, string, error) {
			return false, "", nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestReactionService(&mockReactionStore{}, ms, cs, rec)

	_, err := svc.ListReactions(context.Background(), msgID, userID)
	reactionAssertAppError(t, err, 403)
}

func TestListReactionUsers_AllReactions_AllowsEmptyEmoji(t *testing.T) {
	chatID := uuid.New()
	msgID := uuid.New()
	userID := uuid.New()
	expectedCreatedAt := time.Now().UTC()

	ms := &mockMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: msgID, ChatID: chatID}, nil
		},
	}
	cs := &mockChatStore{
		isMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	}
	rs := &mockReactionStore{
		listUsersByEmojiFn: func(ctx context.Context, messageID uuid.UUID, emoji string, cursor string, limit int) ([]model.Reaction, string, bool, error) {
			if messageID != msgID {
				t.Fatalf("unexpected message id: %s", messageID)
			}
			if emoji != "" {
				t.Fatalf("expected empty emoji filter, got %q", emoji)
			}
			if cursor != "cursor-1" {
				t.Fatalf("expected cursor-1, got %q", cursor)
			}
			if limit != 25 {
				t.Fatalf("expected limit 25, got %d", limit)
			}

			return []model.Reaction{{
				MessageID:   msgID,
				UserID:      userID,
				Emoji:       "👍",
				CreatedAt:   expectedCreatedAt,
				DisplayName: "Orbit QA",
			}}, "cursor-2", true, nil
		},
	}
	rec := &RecordingPublisher{}
	svc := newTestReactionService(rs, ms, cs, rec)

	reactions, nextCursor, hasMore, err := svc.ListReactionUsers(context.Background(), msgID, userID, "", "cursor-1", 25)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(reactions) != 1 {
		t.Fatalf("expected 1 reaction, got %d", len(reactions))
	}
	if reactions[0].Emoji != "👍" {
		t.Fatalf("expected 👍 emoji, got %q", reactions[0].Emoji)
	}
	if nextCursor != "cursor-2" {
		t.Fatalf("expected next cursor cursor-2, got %q", nextCursor)
	}
	if !hasMore {
		t.Fatal("expected hasMore to be true")
	}
}
