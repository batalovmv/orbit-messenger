package service

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

// Publisher is the interface for event publishing (NATS in prod, recording in tests).
type Publisher interface {
	Publish(subject string, event string, data interface{}, memberIDs []string, senderID ...string)
}

type NATSPublisher struct {
	nc *nats.Conn
}

func NewNATSPublisher(nc *nats.Conn) *NATSPublisher {
	return &NATSPublisher{nc: nc}
}

type NATSEvent struct {
	Event     string      `json:"event"`
	Data      interface{} `json:"data"`
	MemberIDs []string    `json:"member_ids"`
	SenderID  string      `json:"sender_id,omitempty"`
	Timestamp string      `json:"timestamp"`
}

// NewNoopNATSPublisher returns a NATSPublisher that discards all events.
// Use in tests to avoid requiring a real NATS connection.
func NewNoopNATSPublisher() *NATSPublisher {
	return &NATSPublisher{nc: nil}
}

func (p *NATSPublisher) Publish(subject string, event string, data interface{}, memberIDs []string, senderID ...string) {
	if p.nc == nil {
		return
	}
	evt := NATSEvent{
		Event:     event,
		Data:      data,
		MemberIDs: memberIDs,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	if len(senderID) > 0 {
		evt.SenderID = senderID[0]
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		slog.Error("nats: marshal error", "error", err, "event", event)
		return
	}
	if err := p.nc.Publish(subject, payload); err != nil {
		slog.Error("nats: publish error", "error", err, "subject", subject)
	}
}
