package sfu

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeWSConn is a no-op WSConn for tests — we only need NewPeer to construct
// successfully and the cleanup hooks to fire. Real Pion PeerConnections do
// not require the conn to be valid until they actually try to write.
type fakeWSConn struct {
	closed bool
	writes int
}

func (f *fakeWSConn) WriteJSON(any) error { f.writes++; return nil }
func (f *fakeWSConn) Close() error        { f.closed = true; return nil }

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func TestSFU_RoomLifecycle(t *testing.T) {
	s, err := NewSFU(newTestLogger())
	if err != nil {
		t.Fatalf("NewSFU: %v", err)
	}
	if s.RoomCount() != 0 {
		t.Fatalf("expected 0 rooms, got %d", s.RoomCount())
	}

	callID := uuid.New()
	r1 := s.GetOrCreateRoom(callID)
	r2 := s.GetOrCreateRoom(callID)
	if r1 != r2 {
		t.Fatalf("GetOrCreateRoom should be idempotent")
	}
	if s.RoomCount() != 1 {
		t.Fatalf("expected 1 room, got %d", s.RoomCount())
	}

	s.CloseRoom(callID)
	if s.RoomCount() != 0 {
		t.Fatalf("expected 0 rooms after close, got %d", s.RoomCount())
	}

	// CloseRoom on a non-existent room should be a no-op (idempotent).
	s.CloseRoom(callID)
}

func TestSFU_CleanupEmptyRooms(t *testing.T) {
	s, err := NewSFU(newTestLogger())
	if err != nil {
		t.Fatalf("NewSFU: %v", err)
	}
	// Two rooms, both empty.
	id1, id2 := uuid.New(), uuid.New()
	s.GetOrCreateRoom(id1)
	s.GetOrCreateRoom(id2)
	if s.RoomCount() != 2 {
		t.Fatalf("expected 2 rooms, got %d", s.RoomCount())
	}

	// Run one tick of the cleanup loop. We use a context cancelled after a
	// short delay so the loop exits cleanly.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	go s.StartCleanupLoop(ctx, 5*time.Millisecond)
	<-ctx.Done()

	if got := s.RoomCount(); got != 0 {
		t.Fatalf("expected 0 rooms after cleanup, got %d", got)
	}
}

func TestPeer_NewAndClose(t *testing.T) {
	s, err := NewSFU(newTestLogger())
	if err != nil {
		t.Fatalf("NewSFU: %v", err)
	}
	room := s.GetOrCreateRoom(uuid.New())
	ws := &fakeWSConn{}
	peer, err := NewPeer(uuid.New(), ws, room, newTestLogger())
	if err != nil {
		t.Fatalf("NewPeer: %v", err)
	}
	if peer.PC == nil {
		t.Fatalf("expected non-nil PeerConnection")
	}
	// Two transceivers (audio + video) must be present so that SignalAllPeers
	// can produce an offer with both media sections.
	if got := len(peer.PC.GetTransceivers()); got != 2 {
		t.Fatalf("expected 2 transceivers, got %d", got)
	}

	// Close should be idempotent and tear down the WS conn.
	peer.Close()
	peer.Close()
	if !ws.closed {
		t.Fatalf("expected ws.closed=true after Close")
	}
}

func TestRoom_PeerCountAfterRemove(t *testing.T) {
	s, err := NewSFU(newTestLogger())
	if err != nil {
		t.Fatalf("NewSFU: %v", err)
	}
	room := s.GetOrCreateRoom(uuid.New())

	// Construct a peer, register, then remove. PeerCount must drop to 0.
	uid := uuid.New()
	peer, err := NewPeer(uid, &fakeWSConn{}, room, newTestLogger())
	if err != nil {
		t.Fatalf("NewPeer: %v", err)
	}
	room.AddPeer(peer)
	// signalAllPeers runs async — give it a moment to spin up before we
	// remove the peer. We're not asserting on its outcome here; the test
	// only verifies that AddPeer/RemovePeer bookkeeping is consistent.
	time.Sleep(20 * time.Millisecond)
	room.RemovePeer(uid)
	if got := room.PeerCount(); got != 0 {
		t.Fatalf("expected 0 peers after remove, got %d", got)
	}
}
