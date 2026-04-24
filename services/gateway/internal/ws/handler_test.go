// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ws

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func startTestNATS(t *testing.T) *nats.Conn {
	t.Helper()
	opts := &server.Options{Host: "127.0.0.1", Port: -1}
	ns, err := server.NewServer(opts)
	if err != nil {
		t.Fatalf("failed to create NATS server: %v", err)
	}
	ns.Start()
	t.Cleanup(ns.Shutdown)
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS server not ready")
	}
	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatalf("failed to connect to NATS: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc
}

func newTestHandler(t *testing.T) (*Handler, *nats.Conn) {
	t.Helper()
	nc := startTestNATS(t)
	hub := NewHub()
	h := &Handler{
		Hub:            hub,
		NATS:           nc,
		typingDebounce: make(map[string]time.Time),
		typingTimers:   make(map[string]*time.Timer),
		done:           make(chan struct{}),
	}
	t.Cleanup(func() {
		select {
		case <-h.done:
		default:
			close(h.done)
		}
	})
	return h, nc
}

func TestHandleClientMessage_SetOnline_True(t *testing.T) {
	h, nc := newTestHandler(t)
	userID := uuid.New().String()
	conn := &Conn{UserID: userID, done: make(chan struct{})}

	var received NATSEvent
	var wg sync.WaitGroup
	wg.Add(1)
	sub, err := nc.Subscribe("orbit.user."+userID+".status", func(msg *nats.Msg) {
		json.Unmarshal(msg.Data, &received)
		wg.Done()
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Unsubscribe()
	nc.Flush()

	data, _ := json.Marshal(map[string]bool{"is_online": true})
	msg, _ := json.Marshal(ClientMessage{Type: "set_online", Data: data})
	h.handleClientMessage(conn, msg)

	waitCh := make(chan struct{})
	go func() { wg.Wait(); close(waitCh) }()
	select {
	case <-waitCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for NATS message")
	}

	var sd StatusData
	json.Unmarshal(received.Data, &sd)
	if sd.Status != "online" {
		t.Errorf("expected status 'online', got %q", sd.Status)
	}
	if sd.UserID != userID {
		t.Errorf("expected userID %q, got %q", userID, sd.UserID)
	}
	if received.Event != EventUserStatus {
		t.Errorf("expected event %q, got %q", EventUserStatus, received.Event)
	}
}

func TestHandleClientMessage_SetOnline_False(t *testing.T) {
	h, nc := newTestHandler(t)
	userID := uuid.New().String()
	conn := &Conn{UserID: userID, done: make(chan struct{})}

	var received NATSEvent
	var wg sync.WaitGroup
	wg.Add(1)
	sub, err := nc.Subscribe("orbit.user."+userID+".status", func(msg *nats.Msg) {
		json.Unmarshal(msg.Data, &received)
		wg.Done()
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Unsubscribe()
	nc.Flush()

	data, _ := json.Marshal(map[string]bool{"is_online": false})
	msg, _ := json.Marshal(ClientMessage{Type: "set_online", Data: data})
	h.handleClientMessage(conn, msg)

	waitCh := make(chan struct{})
	go func() { wg.Wait(); close(waitCh) }()
	select {
	case <-waitCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for NATS message")
	}

	var sd StatusData
	json.Unmarshal(received.Data, &sd)
	if sd.Status != "recently" {
		t.Errorf("expected status 'recently', got %q", sd.Status)
	}
	if sd.UserID != userID {
		t.Errorf("expected userID %q, got %q", userID, sd.UserID)
	}
	if sd.LastSeen != "" {
		t.Errorf("expected empty last_seen for 'recently', got %q", sd.LastSeen)
	}
}

func TestHandleSetOnline_MalformedPayload(t *testing.T) {
	h, nc := newTestHandler(t)
	userID := uuid.New().String()
	conn := &Conn{UserID: userID, done: make(chan struct{})}

	gotMsg := make(chan struct{}, 1)
	sub, _ := nc.Subscribe("orbit.user."+userID+".status", func(msg *nats.Msg) {
		gotMsg <- struct{}{}
	})
	defer sub.Unsubscribe()
	nc.Flush()

	malformed := [][]byte{
		[]byte(`{"type":"set_online","data":"not json"}`),
		[]byte(`{"type":"set_online","data":123}`),
		[]byte(`not json at all`),
		[]byte(`{"type":"set_online"}`),
	}

	for i, raw := range malformed {
		h.handleClientMessage(conn, raw)
		select {
		case <-gotMsg:
			t.Errorf("malformed payload %d should not produce NATS message", i)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func TestPublishStatusChange_InvalidUserID(t *testing.T) {
	h, nc := newTestHandler(t)

	gotMsg := make(chan struct{}, 1)
	sub, _ := nc.Subscribe("orbit.user.>", func(msg *nats.Msg) {
		gotMsg <- struct{}{}
	})
	defer sub.Unsubscribe()
	nc.Flush()

	invalidIDs := []string{"not-a-uuid", "", "../injection", "user.*.status"}
	for _, id := range invalidIDs {
		h.publishStatusChange(id, "online")
	}

	select {
	case <-gotMsg:
		t.Error("publishStatusChange should not publish for invalid userIDs")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestPublishStatusChange_OfflineHasLastSeen(t *testing.T) {
	h, nc := newTestHandler(t)
	userID := uuid.New().String()

	var received NATSEvent
	var wg sync.WaitGroup
	wg.Add(1)
	sub, _ := nc.Subscribe("orbit.user."+userID+".status", func(msg *nats.Msg) {
		json.Unmarshal(msg.Data, &received)
		wg.Done()
	})
	defer sub.Unsubscribe()
	nc.Flush()

	before := time.Now()
	h.publishStatusChange(userID, "offline")

	waitCh := make(chan struct{})
	go func() { wg.Wait(); close(waitCh) }()
	select {
	case <-waitCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	var sd StatusData
	json.Unmarshal(received.Data, &sd)
	if sd.Status != "offline" {
		t.Errorf("expected 'offline', got %q", sd.Status)
	}
	if sd.LastSeen == "" {
		t.Error("offline status should include last_seen")
	}
	ts, err := time.Parse(time.RFC3339, sd.LastSeen)
	if err != nil {
		t.Fatalf("last_seen not RFC3339: %v", err)
	}
	if ts.Before(before.Add(-2*time.Second)) || ts.After(time.Now().Add(2*time.Second)) {
		t.Errorf("last_seen %v out of expected range", ts)
	}
}

func TestHandleClientMessage_UnknownType_NoPublish(t *testing.T) {
	h, nc := newTestHandler(t)
	userID := uuid.New().String()
	conn := &Conn{UserID: userID, done: make(chan struct{})}

	gotMsg := make(chan struct{}, 1)
	sub, _ := nc.Subscribe("orbit.user.>", func(msg *nats.Msg) {
		gotMsg <- struct{}{}
	})
	defer sub.Unsubscribe()
	nc.Flush()

	msg, _ := json.Marshal(ClientMessage{Type: "unknown_type", Data: json.RawMessage(`{}`)})
	h.handleClientMessage(conn, msg)

	select {
	case <-gotMsg:
		t.Error("unknown message type should not produce NATS messages")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestHandleSetOnline_DefaultFalse_WhenMissing(t *testing.T) {
	h, nc := newTestHandler(t)
	userID := uuid.New().String()
	conn := &Conn{UserID: userID, done: make(chan struct{})}

	var received NATSEvent
	var wg sync.WaitGroup
	wg.Add(1)
	sub, _ := nc.Subscribe("orbit.user."+userID+".status", func(msg *nats.Msg) {
		json.Unmarshal(msg.Data, &received)
		wg.Done()
	})
	defer sub.Unsubscribe()
	nc.Flush()

	msg, _ := json.Marshal(ClientMessage{Type: "set_online", Data: json.RawMessage(`{}`)})
	h.handleClientMessage(conn, msg)

	waitCh := make(chan struct{})
	go func() { wg.Wait(); close(waitCh) }()
	select {
	case <-waitCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	var sd StatusData
	json.Unmarshal(received.Data, &sd)
	if sd.Status != "recently" {
		t.Errorf("expected 'recently' for missing is_online, got %q", sd.Status)
	}
}

func TestPublishStatusChange_NATSSubject(t *testing.T) {
	h, nc := newTestHandler(t)
	userID := uuid.New().String()
	expectedSubject := fmt.Sprintf("orbit.user.%s.status", userID)

	var receivedSubject string
	var wg sync.WaitGroup
	wg.Add(1)
	sub, _ := nc.Subscribe(expectedSubject, func(msg *nats.Msg) {
		receivedSubject = msg.Subject
		wg.Done()
	})
	defer sub.Unsubscribe()
	nc.Flush()

	h.publishStatusChange(userID, "online")

	waitCh := make(chan struct{})
	go func() { wg.Wait(); close(waitCh) }()
	select {
	case <-waitCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	if receivedSubject != expectedSubject {
		t.Errorf("expected subject %q, got %q", expectedSubject, receivedSubject)
	}
}

func TestHandleSetOnline_InvalidJSON_Data(t *testing.T) {
	h, nc := newTestHandler(t)
	userID := uuid.New().String()
	conn := &Conn{UserID: userID, done: make(chan struct{})}

	gotMsg := make(chan struct{}, 1)
	sub, _ := nc.Subscribe("orbit.user."+userID+".status", func(msg *nats.Msg) {
		gotMsg <- struct{}{}
	})
	defer sub.Unsubscribe()
	nc.Flush()

	data := json.RawMessage(`{"is_online": "yes_please"}`)
	msg, _ := json.Marshal(ClientMessage{Type: "set_online", Data: data})
	h.handleClientMessage(conn, msg)

	select {
	case <-gotMsg:
		t.Error("invalid is_online type should not produce NATS message")
	case <-time.After(200 * time.Millisecond):
	}
}