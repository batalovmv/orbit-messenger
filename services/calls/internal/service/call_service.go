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

	if err := s.calls.Create(ctx, call); err != nil {
		return nil, apperror.Internal("create call")
	}

	// Add initiator as first participant
	if err := s.participants.Add(ctx, &model.CallParticipant{
		CallID: call.ID,
		UserID: initiatorID,
	}); err != nil {
		s.logger.Error("failed to add initiator as participant", "error", err, "call_id", call.ID)
	}

	// Publish call_incoming to other members
	subject := fmt.Sprintf("orbit.call.%s.lifecycle", call.ID)
	s.nats.Publish(subject, "call_incoming", call, req.MemberIDs, initiatorID.String())

	s.logger.Info("call created", "call_id", call.ID, "chat_id", req.ChatID, "type", req.Type, "mode", req.Mode)
	return call, nil
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

	now := time.Now()
	if err := s.calls.UpdateStatus(ctx, callID, model.CallStatusActive, &now, nil, nil); err != nil {
		return nil, apperror.Internal("accept call")
	}

	// Add accepter as participant
	if err := s.participants.Add(ctx, &model.CallParticipant{
		CallID: callID,
		UserID: userID,
	}); err != nil {
		s.logger.Error("failed to add participant on accept", "error", err)
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

// AddParticipant adds a user to a group call.
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

	if err := s.participants.Add(ctx, &model.CallParticipant{
		CallID: callID,
		UserID: userID,
	}); err != nil {
		return apperror.Internal("add participant")
	}

	participants, _ := s.participants.ListByCall(ctx, callID)
	memberIDs := participantUserIDs(participants)

	subject := fmt.Sprintf("orbit.call.%s.participant", callID)
	s.nats.Publish(subject, "call_participant_joined", map[string]interface{}{
		"call_id": callID,
		"user_id": userID,
	}, memberIDs)

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
