package ws

import "encoding/json"

// Event types
const (
	EventNewMessage     = "new_message"
	EventMessageUpdated = "message_updated"
	EventMessageDeleted = "message_deleted"
	EventMessagesRead     = "messages_read"
	EventMessagePinned   = "message_pinned"
	EventMessageUnpinned = "message_unpinned"
	EventTyping          = "typing"
	EventStopTyping     = "stop_typing"
	EventUserStatus     = "user_status"
	EventPong           = "pong"
)

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
