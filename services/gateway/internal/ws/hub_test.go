// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ws

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"
)

// newTestConn creates a *Conn with no real WebSocket connection.
// The WS field is nil — safe for hub routing tests that only touch the conns map.
// Tests that call conn.Send() would panic; those are explicitly skipped below.
func newTestConn(userID string) *Conn {
	return &Conn{
		UserID: userID,
		done:   make(chan struct{}),
		// WS is intentionally nil — only hub map operations are tested here.
	}
}

// TestHub_RegisterUnregister verifies that Register makes a user online
// and Unregister removes them.
func TestHub_RegisterUnregister(t *testing.T) {
	hub := NewHub()

	conn := newTestConn("user-1")
	hub.Register(conn)

	if !hub.IsOnline("user-1") {
		t.Fatal("user-1 should be online after Register")
	}

	hub.Unregister(conn)

	if hub.IsOnline("user-1") {
		t.Fatal("user-1 should be offline after Unregister")
	}
}

// TestHub_UnregisterUnknown verifies that unregistering a connection that was
// never registered (or already removed) does not panic.
func TestHub_UnregisterUnknown(t *testing.T) {
	hub := NewHub()
	conn := newTestConn("ghost-user")
	// Must not panic
	hub.Unregister(conn)
}

// TestHub_OnlineUserIDs verifies that OnlineUserIDs returns exactly the set
// of users that are currently registered.
func TestHub_OnlineUserIDs(t *testing.T) {
	hub := NewHub()

	users := []string{"alice", "bob", "carol"}
	conns := make([]*Conn, len(users))
	for i, u := range users {
		conns[i] = newTestConn(u)
		hub.Register(conns[i])
	}

	ids := hub.OnlineUserIDs()
	if len(ids) != len(users) {
		t.Fatalf("expected %d online users, got %d", len(users), len(ids))
	}

	sort.Strings(ids)
	sort.Strings(users)
	for i := range users {
		if ids[i] != users[i] {
			t.Fatalf("OnlineUserIDs mismatch at index %d: want %q, got %q", i, users[i], ids[i])
		}
	}

	// Unregister one and verify the count drops
	hub.Unregister(conns[0])
	ids = hub.OnlineUserIDs()
	if len(ids) != len(users)-1 {
		t.Fatalf("after unregister: expected %d online users, got %d", len(users)-1, len(ids))
	}
	for _, id := range ids {
		if id == users[0] {
			t.Fatalf("unregistered user %q still appears in OnlineUserIDs", users[0])
		}
	}
}

// TestHub_MultiDevice verifies that two connections for the same user are both
// tracked and that IsOnline remains true until the last connection is removed.
func TestHub_MultiDevice(t *testing.T) {
	hub := NewHub()

	conn1 := newTestConn("multi-user")
	conn2 := newTestConn("multi-user")

	hub.Register(conn1)
	hub.Register(conn2)

	if !hub.IsOnline("multi-user") {
		t.Fatal("multi-user should be online with 2 connections")
	}

	// Verify internal count — read via OnlineUserIDs (user appears exactly once in the map key set)
	ids := hub.OnlineUserIDs()
	count := 0
	for _, id := range ids {
		if id == "multi-user" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected multi-user to appear once in OnlineUserIDs map keys, got %d", count)
	}

	// Remove one device — user must still be online
	hub.Unregister(conn1)
	if !hub.IsOnline("multi-user") {
		t.Fatal("multi-user should remain online with 1 connection left")
	}

	// Remove second device — now offline
	hub.Unregister(conn2)
	if hub.IsOnline("multi-user") {
		t.Fatal("multi-user should be offline after all connections removed")
	}
}

// TestHub_IsOnline_OfflineUser verifies that IsOnline returns false for a user
// who was never registered.
func TestHub_IsOnline_OfflineUser(t *testing.T) {
	hub := NewHub()
	if hub.IsOnline("nonexistent-user") {
		t.Fatal("expected IsOnline to return false for unknown user")
	}
}

// TestHub_ConcurrentRegisterUnregister stress-tests hub under concurrent access.
func TestHub_ConcurrentRegisterUnregister(t *testing.T) {
	hub := NewHub()
	done := make(chan struct{})

	const goroutines = 20
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			userID := "concurrent-user"
			conn := newTestConn(userID)
			hub.Register(conn)
			_ = hub.IsOnline(userID)
			_ = hub.OnlineUserIDs()
			hub.Unregister(conn)
		}(i)
	}

	for i := 0; i < goroutines; i++ {
		<-done
	}
}

func TestHub_SendToUser_DisconnectsSlowSubscriberWithoutBlockingOthers(t *testing.T) {
	hub := NewHub()

	releaseSlowWriter := make(chan struct{})
	slowCloses := make(chan string, 1)
	slowDone := make(chan struct{})
	slowConn := &Conn{
		UserID: "slow-user",
		done:   slowDone,
		send:   make(chan interface{}, sendQueueCapacity),
		sendFn: func(interface{}) error {
			<-releaseSlowWriter
			return nil
		},
		closeFn: func(code int, text string) error {
			slowCloses <- fmt.Sprintf("%d:%s", code, text)
			close(slowDone)
			return nil
		},
	}

	fastDeliveries := make(chan Envelope, 1)
	fastConn := newCapturingConn("fast-user", fastDeliveries)

	hub.Register(slowConn)
	hub.Register(fastConn)

	msg := Envelope{Type: EventPong, Data: json.RawMessage(`{}`)}

	start := time.Now()
	for i := 0; i < sendQueueCapacity+8; i++ {
		hub.SendToUser("slow-user", msg)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("slow fanout blocked for %s", elapsed)
	}

	select {
	case got := <-slowCloses:
		want := fmt.Sprintf("%d:%s", closeCodePolicyViolation, "slow consumer")
		if got != want {
			t.Fatalf("unexpected close payload: want %q, got %q", want, got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for slow consumer disconnect")
	}

	hub.SendToUser("fast-user", msg)

	select {
	case got := <-fastDeliveries:
		if got.Type != EventPong {
			t.Fatalf("unexpected fast delivery type: %q", got.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for fast delivery")
	}

	close(releaseSlowWriter)
}

// newSessionConn is like newCapturingConn but also tags the connection with a
// SessionID so we can verify SendToUserExceptSession's exclusion logic.
func newSessionConn(userID, sessionID string, deliveries chan Envelope) *Conn {
	return &Conn{
		UserID:    userID,
		SessionID: sessionID,
		done:      make(chan struct{}),
		sendFn: func(msg interface{}) error {
			if env, ok := msg.(Envelope); ok {
				deliveries <- env
			}
			return nil
		},
	}
}

// TestHub_SendToUserExceptSession_ExcludesOriginatingTab verifies the core
// cross-device fanout primitive: when a user has multiple connections and one
// of them performed an action (SessionID == excludeSessionID), only the OTHER
// connections receive the event.
func TestHub_SendToUserExceptSession_ExcludesOriginatingTab(t *testing.T) {
	hub := NewHub()

	originDeliveries := make(chan Envelope, 1)
	otherDeliveries := make(chan Envelope, 1)
	thirdDeliveries := make(chan Envelope, 1)

	const userID = "u-1"
	const originSession = "tab-A"
	const otherSession = "tab-B"
	const thirdSession = "tab-C"

	origin := newSessionConn(userID, originSession, originDeliveries)
	other := newSessionConn(userID, otherSession, otherDeliveries)
	third := newSessionConn(userID, thirdSession, thirdDeliveries)

	hub.Register(origin)
	hub.Register(other)
	hub.Register(third)

	msg := Envelope{Type: EventReadSync, Data: json.RawMessage(`{}`)}
	hub.SendToUserExceptSession(userID, originSession, msg)

	// origin must NOT receive (its SessionID matches the exclusion)
	select {
	case got := <-originDeliveries:
		t.Fatalf("origin tab received echo it should have been excluded from: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}

	// the two other tabs must receive
	for i, ch := range []chan Envelope{otherDeliveries, thirdDeliveries} {
		select {
		case got := <-ch:
			if got.Type != EventReadSync {
				t.Fatalf("tab %d got unexpected type %q", i, got.Type)
			}
		case <-time.After(time.Second):
			t.Fatalf("tab %d did not receive expected event", i)
		}
	}
}

// TestHub_SendToUserExceptSession_EmptyExcludeFansToAll guards the backwards-
// compat path: a request without X-Session-ID reaches messaging with
// originSessionID="" and the fanout must include every connection — the
// originating tab gets its own echo, which is harmless because the SW
// closeMessageNotifications handler is idempotent.
func TestHub_SendToUserExceptSession_EmptyExcludeFansToAll(t *testing.T) {
	hub := NewHub()

	dA := make(chan Envelope, 1)
	dB := make(chan Envelope, 1)

	hub.Register(newSessionConn("u-1", "tab-A", dA))
	hub.Register(newSessionConn("u-1", "tab-B", dB))

	msg := Envelope{Type: EventReadSync, Data: json.RawMessage(`{}`)}
	hub.SendToUserExceptSession("u-1", "", msg)

	for i, ch := range []chan Envelope{dA, dB} {
		select {
		case got := <-ch:
			if got.Type != EventReadSync {
				t.Fatalf("tab %d unexpected type %q", i, got.Type)
			}
		case <-time.After(time.Second):
			t.Fatalf("tab %d did not receive (legacy clients must still get the event)", i)
		}
	}
}

// newCloseTrackingConn returns a Conn whose Close() calls record the (code,
// text) pair into the provided channel. Used by CloseSessionByJTI tests.
func newCloseTrackingConn(userID, jti string, closes chan<- string) *Conn {
	return &Conn{
		UserID: userID,
		JTI:    jti,
		done:   make(chan struct{}),
		closeFn: func(code int, text string) error {
			closes <- fmt.Sprintf("%d:%s:%s", code, jti, text)
			return nil
		},
	}
}

// TestHub_CloseSessionByJTI_ClosesOnlyMatchingConn verifies the per-jti close
// primitive used by the admin session-revoke path (Day 5.2).
//
// Verify-by-revert: setting the wrong JTI on a connection should mean it does
// NOT get closed. Reverting the JTI match check would make BOTH connections
// close, failing TestHub_CloseSessionByJTI_DoesNotCloseOtherJTI below.
func TestHub_CloseSessionByJTI_ClosesOnlyMatchingConn(t *testing.T) {
	hub := NewHub()

	closes := make(chan string, 4)
	target := newCloseTrackingConn("u-1", "jti-target", closes)
	other := newCloseTrackingConn("u-1", "jti-other", closes)
	stranger := newCloseTrackingConn("u-2", "jti-stranger", closes)

	hub.Register(target)
	hub.Register(other)
	hub.Register(stranger)

	closed := hub.CloseSessionByJTI("jti-target")
	if closed != 1 {
		t.Fatalf("expected 1 connection closed, got %d", closed)
	}

	select {
	case got := <-closes:
		want := fmt.Sprintf("%d:%s:%s", closeCodePolicyViolation, "jti-target", "session revoked")
		if got != want {
			t.Fatalf("unexpected close payload: want %q, got %q", want, got)
		}
	case <-time.After(time.Second):
		t.Fatal("target connection was not closed")
	}

	// other and stranger must not have been closed
	select {
	case got := <-closes:
		t.Fatalf("non-matching connection was closed: %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestHub_CloseSessionByJTI_EmptyJTI_NoOp guards against accidentally closing
// every connection that happens to carry an empty JTI (which is legal — the
// cache/auth path leaves JTI empty when the JWT lacks a jti claim).
func TestHub_CloseSessionByJTI_EmptyJTI_NoOp(t *testing.T) {
	hub := NewHub()

	closes := make(chan string, 4)
	a := newCloseTrackingConn("u-1", "", closes)
	b := newCloseTrackingConn("u-2", "", closes)
	hub.Register(a)
	hub.Register(b)

	closed := hub.CloseSessionByJTI("")
	if closed != 0 {
		t.Fatalf("empty jti must close 0 connections, got %d", closed)
	}
	select {
	case got := <-closes:
		t.Fatalf("empty jti closed a connection: %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestHub_CloseSessionByJTI_NoMatch_ReturnsZero is the cheap-path test: the
// admin revokes a session whose owner currently has no active WS — the hub
// must not panic and must report zero closes.
func TestHub_CloseSessionByJTI_NoMatch_ReturnsZero(t *testing.T) {
	hub := NewHub()
	hub.Register(newCloseTrackingConn("u-1", "jti-A", make(chan string, 1)))

	closed := hub.CloseSessionByJTI("jti-NOT-PRESENT")
	if closed != 0 {
		t.Fatalf("expected 0 closes, got %d", closed)
	}
}

// TestHub_SendToUserExceptSession_DoesNotCrossUsers asserts the userID lookup
// scopes the fanout to a single user, even if a different user happens to
// share a SessionID (collision is theoretically possible since SessionID is
// client-generated).
func TestHub_SendToUserExceptSession_DoesNotCrossUsers(t *testing.T) {
	hub := NewHub()

	mine := make(chan Envelope, 1)
	stranger := make(chan Envelope, 1)

	hub.Register(newSessionConn("u-1", "tab-A", mine))
	hub.Register(newSessionConn("u-2", "tab-A", stranger)) // same SessionID, different user

	msg := Envelope{Type: EventReadSync, Data: json.RawMessage(`{}`)}
	hub.SendToUserExceptSession("u-1", "tab-A", msg) // exclude my origin

	// Mine: excluded by SessionID match
	select {
	case got := <-mine:
		t.Fatalf("u-1 origin tab received echo: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}

	// Stranger: must not receive at all — different user
	select {
	case got := <-stranger:
		t.Fatalf("u-2 received an event scoped to u-1: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
}
