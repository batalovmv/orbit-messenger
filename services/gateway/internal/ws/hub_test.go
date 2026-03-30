package ws

import (
	"sort"
	"testing"
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
