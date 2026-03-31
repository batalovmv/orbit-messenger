package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

func invAssertAppError(t *testing.T, err error, wantStatus int) {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError with status %d, got %T: %v", wantStatus, err, err)
	}
	if appErr.Status != wantStatus {
		t.Fatalf("expected status %d, got %d: %s", wantStatus, appErr.Status, appErr.Message)
	}
}

// ---------------------------------------------------------------------------
// CreateInviteLink — hash format
// ---------------------------------------------------------------------------

func TestCreateInviteLink_HashFormat(t *testing.T) {
	chatID := uuid.New()
	callerID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "owner", Permissions: permissions.AllPermissions}, nil
		},
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			name := "Group"
			return &model.Chat{ID: chatID, Type: "group", Name: &name, DefaultPermissions: 255}, nil
		},
	}
	is := &mockInviteStore{
		createFn: func(_ context.Context, link *model.InviteLink) error {
			link.ID = uuid.New()
			return nil
		},
	}

	svc := NewInviteService(is, cs, rec)
	link, err := svc.CreateInviteLink(context.Background(), chatID, callerID, nil, nil, 0, false)
	if err != nil {
		t.Fatalf("CreateInviteLink: %v", err)
	}
	if len(link.Hash) != 32 {
		t.Fatalf("hash should be 32 hex chars (16 bytes), got %d: %q", len(link.Hash), link.Hash)
	}
}

// ---------------------------------------------------------------------------
// JoinByInvite — expired link
// ---------------------------------------------------------------------------

func TestJoinByInvite_ExpiredLink(t *testing.T) {
	rec := &RecordingPublisher{}
	cs := &mockChatStore{}
	past := time.Now().Add(-1 * time.Hour)
	is := &mockInviteStore{
		getByHashFn: func(_ context.Context, _ string) (*model.InviteLink, error) {
			return &model.InviteLink{
				ID:        uuid.New(),
				ChatID:    uuid.New(),
				Hash:      "abc123def456",
				ExpireAt:  &past,
				IsRevoked: false,
			}, nil
		},
	}

	svc := NewInviteService(is, cs, rec)
	_, err := svc.JoinByInvite(context.Background(), "abc123def456", uuid.New())
	invAssertAppError(t, err, 404)
}

// ---------------------------------------------------------------------------
// JoinByInvite — requires approval → creates JoinRequest
// ---------------------------------------------------------------------------

func TestJoinByInvite_RequiresApproval_CreatesPendingRequest(t *testing.T) {
	rec := &RecordingPublisher{}
	chatID := uuid.New()
	userID := uuid.New()
	joinRequestCreated := false

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return false, "", nil
		},
	}
	is := &mockInviteStore{
		getByHashFn: func(_ context.Context, _ string) (*model.InviteLink, error) {
			return &model.InviteLink{
				ID:               uuid.New(),
				ChatID:           chatID,
				Hash:             "abc123def456",
				RequiresApproval: true,
				IsRevoked:        false,
			}, nil
		},
		createJoinRequestFn: func(_ context.Context, req *model.JoinRequest) error {
			joinRequestCreated = true
			if req.Status != "pending" {
				t.Errorf("join request status should be 'pending', got %q", req.Status)
			}
			if req.UserID != userID {
				t.Errorf("join request user_id mismatch")
			}
			return nil
		},
	}

	svc := NewInviteService(is, cs, rec)
	result, err := svc.JoinByInvite(context.Background(), "abc123def456", userID)
	if err != nil {
		t.Fatalf("JoinByInvite: %v", err)
	}
	if result["status"] != "pending" {
		t.Fatalf("expected status=pending, got %v", result["status"])
	}
	if !joinRequestCreated {
		t.Fatal("join request should have been created")
	}
}

// ---------------------------------------------------------------------------
// JoinByInvite — usage limit reached
// ---------------------------------------------------------------------------

func TestJoinByInvite_UsageLimitReached(t *testing.T) {
	rec := &RecordingPublisher{}
	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _ uuid.UUID, _ uuid.UUID) (bool, string, error) {
			return false, "", nil
		},
	}
	is := &mockInviteStore{
		getByHashFn: func(_ context.Context, _ string) (*model.InviteLink, error) {
			return &model.InviteLink{
				ID:         uuid.New(),
				ChatID:     uuid.New(),
				Hash:       "abc123def456",
				UsageLimit: 5,
				UsageCount: 5,
				IsRevoked:  false,
			}, nil
		},
		// Simulate SQL atomic rejection: WHERE usage_count < usage_limit fails
		incrementUsageFn: func(_ context.Context, _ uuid.UUID) error {
			return fmt.Errorf("no rows affected")
		},
	}

	svc := NewInviteService(is, cs, rec)
	_, err := svc.JoinByInvite(context.Background(), "abc123def456", uuid.New())
	if err == nil {
		t.Fatal("expected error when usage limit reached, got nil")
	}
}

// ---------------------------------------------------------------------------
// ApproveJoinRequest — member added + NATS event
// ---------------------------------------------------------------------------

func TestApproveJoinRequest_MemberAdded_NATSEvent(t *testing.T) {
	rec := &RecordingPublisher{}
	chatID := uuid.New()
	adminID := uuid.New()
	targetID := uuid.New()
	memberAdded := false

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "admin", nil
		},
		addMemberFn: func(_ context.Context, _, userID uuid.UUID, role string) error {
			memberAdded = true
			if userID != targetID {
				t.Errorf("wrong user added: %s", userID)
			}
			if role != "member" {
				t.Errorf("should be added as member, got %q", role)
			}
			return nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{adminID.String(), targetID.String()}, nil
		},
	}
	is := &mockInviteStore{
		updateJoinRequestStatusFn: func(_ context.Context, _, _ uuid.UUID, status string, reviewedBy uuid.UUID) error {
			if status != "approved" {
				t.Errorf("status should be 'approved', got %q", status)
			}
			return nil
		},
	}

	svc := NewInviteService(is, cs, rec)
	err := svc.ApproveJoinRequest(context.Background(), chatID, adminID, targetID)
	if err != nil {
		t.Fatalf("ApproveJoinRequest: %v", err)
	}
	if !memberAdded {
		t.Fatal("member should have been added to chat")
	}

	events := rec.FindByEvent("chat_member_added")
	if len(events) != 1 {
		t.Fatalf("expected 1 chat_member_added event, got %d", len(events))
	}
}

// ---------------------------------------------------------------------------
// RejectJoinRequest — member NOT added
// ---------------------------------------------------------------------------

func TestRejectJoinRequest_MemberNotAdded(t *testing.T) {
	rec := &RecordingPublisher{}
	chatID := uuid.New()
	adminID := uuid.New()
	targetID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "admin", nil
		},
		addMemberFn: func(_ context.Context, _, _ uuid.UUID, _ string) error {
			t.Fatal("AddMember should NOT be called on rejection")
			return nil
		},
	}
	is := &mockInviteStore{
		updateJoinRequestStatusFn: func(_ context.Context, _, _ uuid.UUID, status string, _ uuid.UUID) error {
			if status != "rejected" {
				t.Errorf("status should be 'rejected', got %q", status)
			}
			return nil
		},
	}

	svc := NewInviteService(is, cs, rec)
	err := svc.RejectJoinRequest(context.Background(), chatID, adminID, targetID)
	if err != nil {
		t.Fatalf("RejectJoinRequest: %v", err)
	}
}

// ---------------------------------------------------------------------------
// JoinByInvite — successful join increments usage
// ---------------------------------------------------------------------------

func TestJoinByInvite_Success_IncrementsUsage(t *testing.T) {
	rec := &RecordingPublisher{}
	chatID := uuid.New()
	userID := uuid.New()
	usageIncremented := false

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return false, "", nil
		},
		addMemberFn: func(_ context.Context, _, _ uuid.UUID, _ string) error { return nil },
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{userID.String()}, nil
		},
	}
	is := &mockInviteStore{
		getByHashFn: func(_ context.Context, _ string) (*model.InviteLink, error) {
			return &model.InviteLink{
				ID:         uuid.New(),
				ChatID:     chatID,
				Hash:       "abc123def456",
				UsageLimit: 10,
				UsageCount: 3,
				IsRevoked:  false,
			}, nil
		},
		incrementUsageFn: func(_ context.Context, _ uuid.UUID) error {
			usageIncremented = true
			return nil
		},
	}

	svc := NewInviteService(is, cs, rec)
	result, err := svc.JoinByInvite(context.Background(), "abc123def456", userID)
	if err != nil {
		t.Fatalf("JoinByInvite: %v", err)
	}
	if result["status"] != "joined" {
		t.Fatalf("expected status=joined, got %v", result["status"])
	}
	if !usageIncremented {
		t.Fatal("usage counter should have been incremented")
	}
}

// ---------------------------------------------------------------------------
// Permission: member without CanInviteViaLink cannot create links
// ---------------------------------------------------------------------------

func TestCreateInviteLink_MemberWithoutPermission_Forbidden(t *testing.T) {
	chatID := uuid.New()
	memberID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "member", Permissions: 0}, nil
		},
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			name := "Group"
			// Group default perms do NOT include CanInviteViaLink
			return &model.Chat{ID: chatID, Type: "group", Name: &name, DefaultPermissions: permissions.AllPermissions &^ permissions.CanInviteViaLink}, nil
		},
	}
	is := &mockInviteStore{}

	svc := NewInviteService(is, cs, rec)
	_, err := svc.CreateInviteLink(context.Background(), chatID, memberID, nil, nil, 0, false)
	invAssertAppError(t, err, 403)
}
