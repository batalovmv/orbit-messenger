package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/calls/internal/model"
	"github.com/mst-corp/orbit/services/calls/internal/store"
)

// CallService implements call business logic.
type CallService struct {
	calls        store.CallStore
	participants store.ParticipantStore
	nats         Publisher
	logger       *slog.Logger
}

// NewCallService creates a new CallService.
func NewCallService(calls store.CallStore, participants store.ParticipantStore, nats Publisher, logger *slog.Logger) *CallService {
	return &CallService{
		calls:        calls,
		participants: participants,
		nats:         nats,
		logger:       logger,
	}
}

// CreateCallRequest is the input for creating a call.
type CreateCallRequest struct {
	ChatID    uuid.UUID
	Type      string // voice/video
	Mode      string // p2p/group
	MemberIDs []string
}

// CreateCall initiates a new call.
func (s *CallService) CreateCall(ctx context.Context, initiatorID uuid.UUID, req CreateCallRequest) (*model.Call, error) {
	// Check no active call exists for this chat
	existing, err := s.calls.GetActiveForChat(ctx, req.ChatID)
	if err != nil {
		return nil, apperror.Internal("check active call")
	}
	if existing != nil {
		return nil, apperror.Conflict("Active call already exists for this chat")
	}

	call := &model.Call{
		Type:        req.Type,
		Mode:        req.Mode,
		ChatID:      req.ChatID,
		InitiatorID: initiatorID,
		Status:      model.CallStatusRinging,
	}

	// Verify caller is a member of the chat (prevents IDOR — can't start a call in
	// someone else's chat just by knowing the chat ID).
	inChat, err := s.calls.IsUserInChat(ctx, req.ChatID, initiatorID)
	if err != nil {
		s.logger.Error("check chat membership", "error", err, "chat_id", req.ChatID, "user_id", initiatorID)
		return nil, apperror.Internal("check chat membership")
	}
	if !inChat {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	if err := s.calls.Create(ctx, call); err != nil {
		return nil, apperror.Internal("create call")
	}

	// Add initiator as first participant. If this fails the call is orphaned
	// (no participants), so roll back the call creation.
	if err := s.participants.Add(ctx, &model.CallParticipant{
		CallID: call.ID,
		UserID: initiatorID,
	}); err != nil {
		s.logger.Error("failed to add initiator as participant, rolling back call", "error", err, "call_id", call.ID)
		if delErr := s.calls.Delete(ctx, call.ID); delErr != nil {
			s.logger.Error("failed to rollback call creation", "error", delErr, "call_id", call.ID)
		}
		return nil, apperror.Internal("add initiator")
	}

	// For group calls, hand the client the SFU WebSocket URL it should
	// open after acceptance. P2P calls keep the empty value (Stage 1 path).
	if call.Mode == model.CallModeGroup {
		call.SfuWsURL = fmt.Sprintf("/api/v1/calls/%s/sfu-ws", call.ID)
	}

	// Publish call_incoming to other members
	subject := fmt.Sprintf("orbit.call.%s.lifecycle", call.ID)
	s.nats.Publish(subject, "call_incoming", call, req.MemberIDs, initiatorID.String())

	s.logger.Info("call created", "call_id", call.ID, "chat_id", req.ChatID, "type", req.Type, "mode", req.Mode)
	return call, nil
}

// JoinGroupCall is called by the SFU WebSocket handler when a user opens
// the signaling connection. It validates that the user is allowed in the
// chat the call belongs to, marks them as a participant in the database,
// and publishes call_participant_joined so other peers update their UI.
//
// Group calls in p2p mode are rejected — the SFU signaling endpoint is
// only meaningful for mode='group'.
func (s *CallService) JoinGroupCall(ctx context.Context, callID, userID uuid.UUID) error {
	call, err := s.calls.GetByID(ctx, callID)
	if err != nil {
		return apperror.Internal("get call")
	}
	if call == nil {
		return apperror.NotFound("Call not found")
	}
	if call.Mode != model.CallModeGroup {
		return apperror.BadRequest("Not a group call")
	}
	if call.Status != model.CallStatusActive && call.Status != model.CallStatusRinging {
		return apperror.BadRequest("Call is not active")
	}

	inChat, err := s.calls.IsUserInChat(ctx, call.ChatID, userID)
	if err != nil {
		return apperror.Internal("check chat membership")
	}
	if !inChat {
		return apperror.Forbidden("Not a member of this chat")
	}

	// Promote ringing → active on first non-initiator join, mirroring the
	// AcceptCall flow for p2p. Without this, group calls would never leave
	// "ringing" if the initiator is the only one to dial in via SFU.
	if call.Status == model.CallStatusRinging && userID != call.InitiatorID {
		now := time.Now()
		if err := s.calls.UpdateStatus(ctx, callID, model.CallStatusActive, &now, nil, nil); err != nil {
			return apperror.Internal("activate call")
		}
		call.Status = model.CallStatusActive
		call.StartedAt = &now
	}

	if err := s.participants.Add(ctx, &model.CallParticipant{
		CallID: callID,
		UserID: userID,
	}); err != nil {
		s.logger.Error("join group call: add participant", "error", err, "call_id", callID, "user_id", userID)
		return apperror.Internal("add participant")
	}

	participants, err := s.participants.ListByCall(ctx, callID)
	if err != nil {
		s.logger.Error("join group call: list participants", "error", err, "call_id", callID)
	}
	memberIDs := participantUserIDs(participants)
	memberIDs = appendUnique(memberIDs, call.InitiatorID.String())

	subject := fmt.Sprintf("orbit.call.%s.participant", callID)
	s.nats.Publish(subject, "call_participant_joined", map[string]interface{}{
		"call_id": callID,
		"user_id": userID,
		"call":    call,
	}, memberIDs, userID.String())

	s.logger.Info("group call joined", "call_id", callID, "user_id", userID)
	return nil
}

// LeaveGroupCall is the inverse of JoinGroupCall: marks the participant as
// left, publishes call_participant_left so other peers see the tile drop, and
// — if endIfEmpty is true and no participants remain — finishes the call.
//
// endIfEmpty exists so the SFU disconnect path can auto-terminate the call,
// while an explicit REST DELETE /calls/:id/leave can choose whether to.
func (s *CallService) LeaveGroupCall(ctx context.Context, callID, userID uuid.UUID, endIfEmpty bool) error {
	call, err := s.calls.GetByID(ctx, callID)
	if err != nil {
		return apperror.Internal("get call")
	}
	if call == nil {
		return apperror.NotFound("Call not found")
	}
	if call.Mode != model.CallModeGroup {
		return apperror.BadRequest("Not a group call")
	}

	// Remove tolerates "already left" — a peer that crashed and rejoined
	// might never have re-marked itself as in the DB row.
	if err := s.participants.Remove(ctx, callID, userID); err != nil {
		s.logger.Warn("leave group call: remove participant", "error", err, "call_id", callID, "user_id", userID)
	}

	remaining, err := s.participants.ListByCall(ctx, callID)
	if err != nil {
		s.logger.Error("leave group call: list participants", "error", err, "call_id", callID)
	}
	memberIDs := participantUserIDs(remaining)
	memberIDs = appendUnique(memberIDs, call.InitiatorID.String())

	subject := fmt.Sprintf("orbit.call.%s.participant", callID)
	s.nats.Publish(subject, "call_participant_left", map[string]interface{}{
		"call_id": callID,
		"user_id": userID,
	}, memberIDs, userID.String())

	s.logger.Info("group call left", "call_id", callID, "user_id", userID, "remaining", len(remaining))

	// Auto-end the call if this was the last participant. Active call rows
	// without any participants are dead state — UI sees "ringing" forever.
	if endIfEmpty && len(remaining) == 0 && (call.Status == model.CallStatusActive || call.Status == model.CallStatusRinging) {
		now := time.Now()
		var durationSeconds *int
		newStatus := model.CallStatusEnded
		if call.Status == model.CallStatusRinging {
			newStatus = model.CallStatusMissed
		}
		if call.StartedAt != nil {
			d := int(math.Round(now.Sub(*call.StartedAt).Seconds()))
			durationSeconds = &d
		}
		if err := s.calls.UpdateStatus(ctx, callID, newStatus, nil, &now, durationSeconds); err != nil {
			s.logger.Error("auto-end empty call", "error", err, "call_id", callID)
		} else {
			lifecycleSubject := fmt.Sprintf("orbit.call.%s.lifecycle", callID)
			s.nats.Publish(lifecycleSubject, "call_ended", map[string]interface{}{
				"call_id":          callID,
				"user_id":          userID,
				"reason":           "hangup",
				"duration_seconds": durationSeconds,
			}, memberIDs, userID.String())
			s.logger.Info("group call auto-ended (last leaver)", "call_id", callID, "status", newStatus)
		}
	}

	return nil
}

// AcceptCall accepts a ringing call.
func (s *CallService) AcceptCall(ctx context.Context, callID, userID uuid.UUID) (*model.Call, error) {
	call, err := s.calls.GetByID(ctx, callID)
	if err != nil {
		return nil, apperror.Internal("get call")
	}
	if call == nil {
		return nil, apperror.NotFound("Call not found")
	}
	if call.Status != model.CallStatusRinging {
		return nil, apperror.BadRequest("Call is not in ringing state")
	}

	// Verify the accepter is a member of the chat BEFORE mutating state —
	// prevents strangers from accepting calls they weren't invited to.
	inChat, err := s.calls.IsUserInChat(ctx, call.ChatID, userID)
	if err != nil {
		s.logger.Error("check chat membership on accept", "error", err, "chat_id", call.ChatID, "user_id", userID)
		return nil, apperror.Internal("check chat membership")
	}
	if !inChat {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	now := time.Now()
	if err := s.calls.UpdateStatus(ctx, callID, model.CallStatusActive, &now, nil, nil); err != nil {
		return nil, apperror.Internal("accept call")
	}

	// Add accepter as participant. Propagate errors — an accepted call with no
	// active participant is a broken state.
	if err := s.participants.Add(ctx, &model.CallParticipant{
		CallID: callID,
		UserID: userID,
	}); err != nil {
		s.logger.Error("failed to add participant on accept", "error", err, "call_id", callID, "user_id", userID)
		return nil, apperror.Internal("add participant")
	}

	call.Status = model.CallStatusActive
	call.StartedAt = &now

	// Fetch member list for notification
	participants, _ := s.participants.ListByCall(ctx, callID)
	memberIDs := participantUserIDs(participants)

	subject := fmt.Sprintf("orbit.call.%s.lifecycle", callID)
	s.nats.Publish(subject, "call_accepted", map[string]interface{}{
		"call_id": callID,
		"user_id": userID,
		"call":    call,
	}, memberIDs, userID.String())

	s.logger.Info("call accepted", "call_id", callID, "user_id", userID)
	return call, nil
}

// DeclineCall declines a ringing call.
func (s *CallService) DeclineCall(ctx context.Context, callID, userID uuid.UUID) error {
	call, err := s.calls.GetByID(ctx, callID)
	if err != nil {
		return apperror.Internal("get call")
	}
	if call == nil {
		return apperror.NotFound("Call not found")
	}
	if call.Status != model.CallStatusRinging {
		return apperror.BadRequest("Call is not in ringing state")
	}

	now := time.Now()
	if err := s.calls.UpdateStatus(ctx, callID, model.CallStatusDeclined, nil, &now, nil); err != nil {
		return apperror.Internal("decline call")
	}

	participants, _ := s.participants.ListByCall(ctx, callID)
	memberIDs := participantUserIDs(participants)
	// Include initiator who may not yet be "in" the call
	memberIDs = appendUnique(memberIDs, call.InitiatorID.String())

	subject := fmt.Sprintf("orbit.call.%s.lifecycle", callID)
	s.nats.Publish(subject, "call_declined", map[string]interface{}{
		"call_id": callID,
		"user_id": userID,
	}, memberIDs, userID.String())

	s.logger.Info("call declined", "call_id", callID, "user_id", userID)
	return nil
}

// EndCall ends an active or ringing call.
func (s *CallService) EndCall(ctx context.Context, callID, userID uuid.UUID) error {
	call, err := s.calls.GetByID(ctx, callID)
	if err != nil {
		return apperror.Internal("get call")
	}
	if call == nil {
		return apperror.NotFound("Call not found")
	}
	if call.Status != model.CallStatusActive && call.Status != model.CallStatusRinging {
		return apperror.BadRequest("Call is already ended")
	}

	now := time.Now()
	var durationSeconds *int
	newStatus := model.CallStatusEnded

	// If call was ringing and never accepted, mark as missed
	if call.Status == model.CallStatusRinging {
		newStatus = model.CallStatusMissed
	}

	if call.StartedAt != nil {
		d := int(math.Round(now.Sub(*call.StartedAt).Seconds()))
		durationSeconds = &d
	}

	if err := s.calls.UpdateStatus(ctx, callID, newStatus, nil, &now, durationSeconds); err != nil {
		return apperror.Internal("end call")
	}

	participants, _ := s.participants.ListByCall(ctx, callID)
	memberIDs := participantUserIDs(participants)
	memberIDs = appendUnique(memberIDs, call.InitiatorID.String())

	subject := fmt.Sprintf("orbit.call.%s.lifecycle", callID)
	s.nats.Publish(subject, "call_ended", map[string]interface{}{
		"call_id":          callID,
		"user_id":          userID,
		"reason":           "hangup",
		"duration_seconds": durationSeconds,
	}, memberIDs, userID.String())

	s.logger.Info("call ended", "call_id", callID, "user_id", userID, "status", newStatus)
	return nil
}

// GetCall returns call details with participants.
func (s *CallService) GetCall(ctx context.Context, callID uuid.UUID) (*model.Call, error) {
	call, err := s.calls.GetByID(ctx, callID)
	if err != nil {
		return nil, apperror.Internal("get call")
	}
	if call == nil {
		return nil, apperror.NotFound("Call not found")
	}

	participants, err := s.participants.ListByCall(ctx, callID)
	if err != nil {
		s.logger.Error("failed to fetch participants", "error", err, "call_id", callID)
	}
	call.Participants = participants
	return call, nil
}

// ListCallHistory returns paginated call history for a user.
func (s *CallService) ListCallHistory(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]model.Call, string, bool, error) {
	calls, nextCursor, hasMore, err := s.calls.ListByUser(ctx, userID, cursor, limit)
	if err != nil {
		return nil, "", false, apperror.Internal("list call history")
	}
	return calls, nextCursor, hasMore, nil
}

// AddParticipant adds a user to a group call. Both the caller (addedByID) and
// the target user must be members of the underlying chat — without this check
// any authenticated user could pull strangers into calls they discover the ID of.
func (s *CallService) AddParticipant(ctx context.Context, callID, userID, addedByID uuid.UUID) error {
	call, err := s.calls.GetByID(ctx, callID)
	if err != nil {
		return apperror.Internal("get call")
	}
	if call == nil {
		return apperror.NotFound("Call not found")
	}
	if call.Status != model.CallStatusActive && call.Status != model.CallStatusRinging {
		return apperror.BadRequest("Call is not active")
	}

	// Caller must be a member of the chat.
	callerInChat, err := s.calls.IsUserInChat(ctx, call.ChatID, addedByID)
	if err != nil {
		return apperror.Internal("check caller membership")
	}
	if !callerInChat {
		return apperror.Forbidden("Not a member of this chat")
	}
	// Target user must also be a member of the chat.
	targetInChat, err := s.calls.IsUserInChat(ctx, call.ChatID, userID)
	if err != nil {
		return apperror.Internal("check target membership")
	}
	if !targetInChat {
		return apperror.Forbidden("Target user is not a member of this chat")
	}

	if err := s.participants.Add(ctx, &model.CallParticipant{
		CallID: callID,
		UserID: userID,
	}); err != nil {
		return apperror.Internal("add participant")
	}

	participants, err := s.participants.ListByCall(ctx, callID)
	if err != nil {
		s.logger.Error("list participants after add", "error", err, "call_id", callID)
	}
	memberIDs := participantUserIDs(participants)

	subject := fmt.Sprintf("orbit.call.%s.participant", callID)
	s.nats.Publish(subject, "call_participant_joined", map[string]interface{}{
		"call_id": callID,
		"user_id": userID,
	}, memberIDs)

	return nil
}

// ExpireRingingCalls marks ringing calls older than threshold as missed and
// publishes call_ended events. Called periodically from a background worker
// in main.go — without this, ringing calls hang forever if the callee's
// client crashes before accepting or declining.
func (s *CallService) ExpireRingingCalls(ctx context.Context, threshold time.Duration) error {
	expired, err := s.calls.ExpireRinging(ctx, threshold)
	if err != nil {
		return fmt.Errorf("expire ringing: %w", err)
	}
	for i := range expired {
		call := &expired[i]
		// Notify all members of the chat — the initiator needs to see "no answer"
		// and any callees that were still ringing should dismiss their incoming UI.
		participants, pErr := s.participants.ListByCall(ctx, call.ID)
		if pErr != nil {
			s.logger.Error("list participants for expired call", "error", pErr, "call_id", call.ID)
		}
		memberIDs := participantUserIDs(participants)
		memberIDs = appendUnique(memberIDs, call.InitiatorID.String())

		subject := fmt.Sprintf("orbit.call.%s.lifecycle", call.ID)
		s.nats.Publish(subject, "call_ended", map[string]interface{}{
			"call_id":          call.ID,
			"user_id":          call.InitiatorID,
			"reason":           "missed",
			"duration_seconds": nil,
		}, memberIDs)
		s.logger.Info("call expired as missed", "call_id", call.ID, "age", time.Since(call.CreatedAt))
	}
	return nil
}

// RemoveParticipant removes a user from a call.
func (s *CallService) RemoveParticipant(ctx context.Context, callID, userID uuid.UUID) error {
	if err := s.participants.Remove(ctx, callID, userID); err != nil {
		return apperror.Internal("remove participant")
	}

	participants, _ := s.participants.ListByCall(ctx, callID)
	memberIDs := participantUserIDs(participants)

	subject := fmt.Sprintf("orbit.call.%s.participant", callID)
	s.nats.Publish(subject, "call_participant_left", map[string]interface{}{
		"call_id": callID,
		"user_id": userID,
	}, memberIDs)

	return nil
}

// ToggleMute updates the mute status of a participant.
func (s *CallService) ToggleMute(ctx context.Context, callID, userID uuid.UUID, isMuted bool) error {
	if err := s.participants.UpdateMute(ctx, callID, userID, isMuted); err != nil {
		return apperror.Internal("toggle mute")
	}

	participants, _ := s.participants.ListByCall(ctx, callID)
	memberIDs := participantUserIDs(participants)

	eventName := "call_muted"
	if !isMuted {
		eventName = "call_unmuted"
	}

	subject := fmt.Sprintf("orbit.call.%s.media", callID)
	s.nats.Publish(subject, eventName, map[string]interface{}{
		"call_id": callID,
		"user_id": userID,
		"muted":   isMuted,
	}, memberIDs, userID.String())

	return nil
}

// StartScreenShare starts screen sharing for a participant.
func (s *CallService) StartScreenShare(ctx context.Context, callID, userID uuid.UUID) error {
	if err := s.participants.UpdateScreenShare(ctx, callID, userID, true); err != nil {
		return apperror.Internal("start screen share")
	}

	participants, _ := s.participants.ListByCall(ctx, callID)
	memberIDs := participantUserIDs(participants)

	subject := fmt.Sprintf("orbit.call.%s.media", callID)
	s.nats.Publish(subject, "screen_share_started", map[string]interface{}{
		"call_id": callID,
		"user_id": userID,
	}, memberIDs, userID.String())

	return nil
}

// StopScreenShare stops screen sharing for a participant.
func (s *CallService) StopScreenShare(ctx context.Context, callID, userID uuid.UUID) error {
	if err := s.participants.UpdateScreenShare(ctx, callID, userID, false); err != nil {
		return apperror.Internal("stop screen share")
	}

	participants, _ := s.participants.ListByCall(ctx, callID)
	memberIDs := participantUserIDs(participants)

	subject := fmt.Sprintf("orbit.call.%s.media", callID)
	s.nats.Publish(subject, "screen_share_stopped", map[string]interface{}{
		"call_id": callID,
		"user_id": userID,
	}, memberIDs, userID.String())

	return nil
}

// ICEServer represents a STUN/TURN server configuration.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// GetICEServers returns STUN and TURN server configurations.
func (s *CallService) GetICEServers(turnURL, turnUser, turnPassword string) []ICEServer {
	servers := []ICEServer{
		{URLs: []string{"stun:stun.l.google.com:19302"}},
	}

	if turnURL != "" && turnUser != "" && turnPassword != "" {
		servers = append(servers, ICEServer{
			URLs:       []string{turnURL},
			Username:   turnUser,
			Credential: turnPassword,
		})
	}

	return servers
}

// participantUserIDs extracts user ID strings from a slice of participants.
func participantUserIDs(participants []model.CallParticipant) []string {
	ids := make([]string, 0, len(participants))
	for _, p := range participants {
		ids = append(ids, p.UserID.String())
	}
	return ids
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}
