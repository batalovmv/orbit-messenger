// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import "sync"

// RecordedEvent captures a single NATS publish call.
type RecordedEvent struct {
	Subject   string
	Event     string
	Data      interface{}
	MemberIDs []string
	SenderID  string
	Hints     ClassifierHints
}

// RecordingPublisher replaces NATSPublisher in tests, capturing all published events.
type RecordingPublisher struct {
	mu     sync.Mutex
	Events []RecordedEvent
}

// Publish records the event instead of sending it to NATS.
func (r *RecordingPublisher) Publish(subject, event string, data interface{}, memberIDs []string, senderID ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := RecordedEvent{
		Subject:   subject,
		Event:     event,
		Data:      data,
		MemberIDs: memberIDs,
	}
	if len(senderID) > 0 {
		e.SenderID = senderID[0]
	}
	r.Events = append(r.Events, e)
}

// PublishMessage records the event + classifier hints.
func (r *RecordingPublisher) PublishMessage(subject, event string, data interface{}, memberIDs []string, senderID string, hints ClassifierHints) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Events = append(r.Events, RecordedEvent{
		Subject:   subject,
		Event:     event,
		Data:      data,
		MemberIDs: memberIDs,
		SenderID:  senderID,
		Hints:     hints,
	})
}

// Reset clears all recorded events.
func (r *RecordingPublisher) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Events = nil
}

// FindByEvent returns all events matching the given event name.
func (r *RecordingPublisher) FindByEvent(name string) []RecordedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []RecordedEvent
	for _, e := range r.Events {
		if e.Event == name {
			out = append(out, e)
		}
	}
	return out
}
