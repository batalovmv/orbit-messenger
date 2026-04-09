package model

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Call type constants
const (
	CallTypeVoice = "voice"
	CallTypeVideo = "video"
)

// Call mode constants
const (
	CallModeP2P   = "p2p"
	CallModeGroup = "group"
)

// Call status constants
const (
	CallStatusRinging  = "ringing"
	CallStatusActive   = "active"
	CallStatusEnded    = "ended"
	CallStatusMissed   = "missed"
	CallStatusDeclined = "declined"
)

// Sentinel errors
var (
	ErrCallNotFound      = errors.New("call not found")
	ErrAlreadyInCall     = errors.New("active call already exists for this chat")
	ErrNotParticipant    = errors.New("user is not a participant of this call")
	ErrInvalidCallStatus = errors.New("invalid call status for this operation")
	ErrAlreadyRated      = errors.New("call has already been rated")
)

// ValidCallTypes are the allowed call types.
var ValidCallTypes = map[string]bool{
	CallTypeVoice: true,
	CallTypeVideo: true,
}

// ValidCallModes are the allowed call modes.
var ValidCallModes = map[string]bool{
	CallModeP2P:   true,
	CallModeGroup: true,
}

// Call represents a voice/video call.
type Call struct {
	ID              uuid.UUID  `json:"id"`
	Type            string     `json:"type"`
	Mode            string     `json:"mode"`
	ChatID          uuid.UUID  `json:"chat_id"`
	InitiatorID     uuid.UUID  `json:"initiator_id"`
	Status          string     `json:"status"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	DurationSeconds *int       `json:"duration_seconds,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	// Rating (Phase 6 Stage 5) — set once by any participant after the call ends.
	Rating         *int       `json:"rating,omitempty"`
	RatingComment  *string    `json:"rating_comment,omitempty"`
	RatedBy        *uuid.UUID `json:"rated_by,omitempty"`
	RatedAt        *time.Time `json:"rated_at,omitempty"`
	// Joined data
	Participants []CallParticipant `json:"participants,omitempty"`
	// SfuWsURL is the gateway-relative WebSocket URL the client should
	// open to join the SFU for group calls. Empty for p2p calls.
	SfuWsURL string `json:"sfu_ws_url,omitempty"`
}

// CallParticipant represents a user in a call.
type CallParticipant struct {
	CallID         uuid.UUID  `json:"call_id"`
	UserID         uuid.UUID  `json:"user_id"`
	JoinedAt       *time.Time `json:"joined_at,omitempty"`
	LeftAt         *time.Time `json:"left_at,omitempty"`
	IsMuted        bool       `json:"is_muted"`
	IsCameraOff    bool       `json:"is_camera_off"`
	IsScreenSharing bool      `json:"is_screen_sharing"`
	// Joined user data
	DisplayName string  `json:"display_name,omitempty"`
	AvatarURL   *string `json:"avatar_url,omitempty"`
}
