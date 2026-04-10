package service

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

// Publisher publishes events to NATS.
type Publisher interface {
	Publish(subject string, event string, data interface{}, memberIDs []string, senderID ...string)
}

// NATSPublisher implements Publisher using NATS.
type NATSPublisher struct {
	nc *nats.Conn
}

// NewNATSPublisher creates a new NATSPublisher.
func NewNATSPublisher(nc *nats.Conn) *NATSPublisher {
	return &NATSPublisher{nc: nc}
}

// NATSEvent is the standard NATS event envelope.
type NATSEvent struct {
	Event     string      `json:"event"`
	Data      interface{} `json:"data"`
	MemberIDs []string    `json:"member_ids"`
	SenderID  string      `json:"sender_id,omitempty"`
	Timestamp string      `json:"timestamp"`
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
		slog.Error("failed to marshal NATS event", "error", err, "event", event)
		return
	}

	msg := &nats.Msg{
		Subject: subject,
		Data:    payload,
		Header:  nats.Header{},
	}
	msg.Header.Set("Nats-Msg-Id", uuid.New().String())
	if err := p.nc.PublishMsg(msg); err != nil {
		slog.Error("failed to publish NATS event", "error", err, "subject", subject, "event", event)
	}
}

// NewNoopNATSPublisher returns a publisher that does nothing (for tests).
func NewNoopNATSPublisher() Publisher {
	return &NATSPublisher{nc: nil}
}
