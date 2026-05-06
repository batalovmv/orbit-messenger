// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

// Publisher is the interface for event publishing (NATS in prod, recording in tests).
type Publisher interface {
	Publish(subject string, event string, data interface{}, memberIDs []string, senderID ...string)
	// PublishMessage is the variant used for `new_message` events that carries
	// the extra signals the gateway notification classifier consumes per
	// recipient. Falling back to Publish loses the signal and the classifier
	// runs blind. Implementations must accept nil/empty hints.
	PublishMessage(subject string, event string, data interface{}, memberIDs []string, senderID string, hints ClassifierHints)
}

// ClassifierHints carries the per-message signals the gateway uses to
// decide push priority without round-tripping back to messaging or the
// users table. All fields are optional — empty means "no signal", which
// downgrades the classifier to its rule-default.
type ClassifierHints struct {
	// SenderRole — admin / member / bot. Empty when not resolvable here.
	SenderRole string `json:"sender_role,omitempty"`
	// MentionUserIDs — user IDs explicitly @mentioned in the message.
	// Gateway does set-membership against the recipient ID to derive
	// per-recipient HasMention.
	MentionUserIDs []string `json:"mention_user_ids,omitempty"`
	// ReplyToUserID — sender of the message being replied to. Gateway
	// compares to the recipient ID to derive per-recipient ReplyToMe.
	ReplyToUserID string `json:"reply_to_user_id,omitempty"`
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
	// Classifier hints — only populated for `new_message` events; everything
	// else leaves them zero. Inlined into the same envelope to avoid a new
	// subject + double-publish cost.
	SenderRole     string   `json:"sender_role,omitempty"`
	MentionUserIDs []string `json:"mention_user_ids,omitempty"`
	ReplyToUserID  string   `json:"reply_to_user_id,omitempty"`
}

// NewNoopNATSPublisher returns a NATSPublisher that discards all events.
// Use in tests to avoid requiring a real NATS connection.
func NewNoopNATSPublisher() *NATSPublisher {
	return &NATSPublisher{nc: nil}
}

func (p *NATSPublisher) Publish(subject string, event string, data interface{}, memberIDs []string, senderID ...string) {
	sid := ""
	if len(senderID) > 0 {
		sid = senderID[0]
	}
	p.publishEnvelope(subject, NATSEvent{
		Event:     event,
		Data:      data,
		MemberIDs: memberIDs,
		SenderID:  sid,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

// PublishMessage is Publish + classifier hints for `new_message` events.
func (p *NATSPublisher) PublishMessage(subject string, event string, data interface{}, memberIDs []string, senderID string, hints ClassifierHints) {
	p.publishEnvelope(subject, NATSEvent{
		Event:          event,
		Data:           data,
		MemberIDs:      memberIDs,
		SenderID:       senderID,
		Timestamp:      time.Now().Format(time.RFC3339),
		SenderRole:     hints.SenderRole,
		MentionUserIDs: hints.MentionUserIDs,
		ReplyToUserID:  hints.ReplyToUserID,
	})
}

func (p *NATSPublisher) publishEnvelope(subject string, evt NATSEvent) {
	if p.nc == nil {
		return
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		slog.Error("nats: marshal error", "error", err, "event", evt.Event)
		return
	}
	msg := &nats.Msg{
		Subject: subject,
		Data:    payload,
		Header:  nats.Header{},
	}
	// Nats-Msg-Id enables JetStream server-side dedup within the 2-minute window
	// and is also used by the gateway dedup cache to skip duplicate redeliveries.
	msg.Header.Set("Nats-Msg-Id", uuid.New().String())
	if err := p.nc.PublishMsg(msg); err != nil {
		slog.Error("nats: publish error", "error", err, "subject", subject)
	}
}
