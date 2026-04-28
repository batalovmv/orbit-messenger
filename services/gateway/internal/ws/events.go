// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ws

import "encoding/json"

// Event types
const (
	EventNewMessage      = "new_message"
	EventMessageUpdated  = "message_updated"
	EventMessageDeleted  = "message_deleted"
	EventMessagesRead    = "messages_read"
	EventMessagePinned   = "message_pinned"
	EventMessageUnpinned = "message_unpinned"
	EventReactionAdded   = "reaction_added"
	EventReactionRemoved = "reaction_removed"
	EventPollVote        = "poll_vote"
	EventPollClosed      = "poll_closed"
	EventTyping          = "typing"
	EventStopTyping      = "stop_typing"
	EventUserStatus      = "user_status"
	EventUserDeactivated = "user_deactivated"
	EventReadSync        = "read_sync"
	// EventSessionRevoked is published by messaging on
	// orbit.session.<sessionID>.revoked after a successful admin revoke.
	// Gateway closes any WS connection whose JWT jti matches sessionID.
	EventSessionRevoked = "session_revoked"
	EventPong            = "pong"

	EventChatCreated       = "chat_created"
	EventChatUpdated       = "chat_updated"
	EventChatDeleted       = "chat_deleted"
	EventChatMemberAdded   = "chat_member_added"
	EventChatMemberRemoved = "chat_member_removed"
	EventChatMemberUpdated = "chat_member_updated"

	EventMediaUploadProgress = "media_upload_progress"
	EventMediaReady          = "media_ready"

	// Bot events
	EventBotInstalled   = "bot_installed"
	EventBotUninstalled = "bot_uninstalled"
	EventCallbackQuery  = "callback_query"

	// Call events
	EventCallIncoming          = "call_incoming"
	EventCallAccepted          = "call_accepted"
	EventCallDeclined          = "call_declined"
	EventCallEnded             = "call_ended"
	EventCallParticipantJoined = "call_participant_joined"
	EventCallParticipantLeft   = "call_participant_left"
	EventCallMuted             = "call_muted"
	EventCallUnmuted           = "call_unmuted"
	EventScreenShareStarted    = "screen_share_started"
	EventScreenShareStopped    = "screen_share_stopped"

	// WebRTC signaling (client-to-client relay via gateway)
	EventWebRTCOffer        = "webrtc_offer"
	EventWebRTCAnswer       = "webrtc_answer"
	EventWebRTCICECandidate = "webrtc_ice_candidate"
)

// SignalingData is the payload for WebRTC signaling events relayed through the gateway.
type SignalingData struct {
	CallID       string `json:"call_id"`
	TargetUserID string `json:"target_user_id"`
	SenderID     string `json:"sender_id,omitempty"`
}

// Envelope is the standard WebSocket message format.
type Envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// NATSEvent is the event envelope received from NATS.
type NATSEvent struct {
	Event     string          `json:"event"`
	Data      json.RawMessage `json:"data"`
	MemberIDs []string        `json:"member_ids"`
	SenderID  string          `json:"sender_id,omitempty"`
	Timestamp string          `json:"timestamp"`
}

// ClientMessage is a message received from the WebSocket client.
type ClientMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// TypingData is the payload for typing events.
type TypingData struct {
	ChatID string `json:"chat_id"`
}

// StatusData is the payload for user_status events.
type StatusData struct {
	UserID   string `json:"user_id"`
	Status   string `json:"status"`
	LastSeen string `json:"last_seen,omitempty"`
}

// ReadSyncData is the payload for read_sync events. Published by messaging on
// the user-scoped subject orbit.user.<userID>.read_sync after a successful
// MarkRead. The gateway forwards it to all of the user's WS connections except
// the one whose SessionID matches OriginSessionID — that's the device that
// just performed the action and already has its UI up-to-date.
type ReadSyncData struct {
	ChatID            string `json:"chat_id"`
	LastReadMessageID string `json:"last_read_message_id"`
	LastReadSeqNum    int64  `json:"last_read_seq_num"`
	UnreadCount       int64  `json:"unread_count"`
	ReadAt            string `json:"read_at"`
	OriginSessionID   string `json:"origin_session_id"`
}
