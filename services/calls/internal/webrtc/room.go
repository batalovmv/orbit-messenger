package sfu

import (
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// Room is a single SFU session for one call_id. It owns the set of peers and
// the set of "local" tracks (one per published remote track) that are fanned
// out to every peer's PeerConnection.
//
// Concurrency rules:
//   - mu guards peers, localTracks and the signal in-flight flag.
//   - All exported methods take mu briefly. Long operations (renegotiation,
//     RTCP write loops) happen outside the lock.
//   - Renegotiation must be serialized: we acquire signalRunning to avoid
//     two concurrent passes interfering with each other.
type Room struct {
	ID     uuid.UUID
	logger *slog.Logger
	apiRef *webrtc.API

	mu            sync.RWMutex
	peers         map[uuid.UUID]*Peer
	localTracks   map[string]*webrtc.TrackLocalStaticRTP
	signalRunning bool
}

func newRoom(id uuid.UUID, api *webrtc.API, logger *slog.Logger) *Room {
	return &Room{
		ID:          id,
		logger:      logger,
		apiRef:      api,
		peers:       make(map[uuid.UUID]*Peer),
		localTracks: make(map[string]*webrtc.TrackLocalStaticRTP),
	}
}

// AddPeer registers the peer in the room and triggers a renegotiation pass so
// that any tracks already published in the room start flowing to the new peer.
func (r *Room) AddPeer(p *Peer) {
	r.mu.Lock()
	r.peers[p.UserID] = p
	count := len(r.peers)
	r.mu.Unlock()
	r.logger.Info("sfu: peer joined", "user_id", p.UserID, "peer_count", count)
	r.SignalAllPeers()
}

// RemovePeer evicts the peer and renegotiates so that the remaining peers
// stop receiving the departed peer's tracks.
func (r *Room) RemovePeer(userID uuid.UUID) {
	r.mu.Lock()
	p, ok := r.peers[userID]
	if !ok {
		r.mu.Unlock()
		return
	}
	delete(r.peers, userID)
	count := len(r.peers)
	r.mu.Unlock()

	if p != nil {
		p.Close()
	}
	r.logger.Info("sfu: peer left", "user_id", userID, "peer_count", count)
	r.SignalAllPeers()
}

// PeerCount returns the number of peers currently in the room (used by the
// SFU cleanup loop and by handlers that need to detect "last leaver" events).
func (r *Room) PeerCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.peers)
}

// AddLocalTrack registers a TrackLocalStaticRTP that should be forwarded to
// every other peer. Called from Peer.OnTrack when a new RTP stream arrives.
// Triggers a renegotiation pass so existing peers receive the new media.
func (r *Room) AddLocalTrack(trackID string, track *webrtc.TrackLocalStaticRTP) {
	r.mu.Lock()
	r.localTracks[trackID] = track
	r.mu.Unlock()
	r.logger.Info("sfu: local track added", "track_id", trackID, "kind", track.Kind())
	r.SignalAllPeers()
}

// RemoveLocalTrack tears down a forwarded track when its source peer drops it.
func (r *Room) RemoveLocalTrack(trackID string) {
	r.mu.Lock()
	if _, ok := r.localTracks[trackID]; !ok {
		r.mu.Unlock()
		return
	}
	delete(r.localTracks, trackID)
	r.mu.Unlock()
	r.logger.Info("sfu: local track removed", "track_id", trackID)
	r.SignalAllPeers()
}

// SignalAllPeers walks every peer's PeerConnection, syncs its senders to
// match the current set of localTracks (adding new ones, removing stale
// ones), creates a fresh offer and pushes it to the client over WS. The
// pattern is lifted from Pion's canonical sfu-ws example.
//
// Renegotiation is bounded to one in-flight pass at a time. If a second
// pass is requested while one is running, the running pass will pick up
// the new state at the end (or the second caller will retry shortly).
func (r *Room) SignalAllPeers() {
	r.mu.Lock()
	if r.signalRunning {
		r.mu.Unlock()
		return
	}
	r.signalRunning = true
	r.mu.Unlock()

	go func() {
		defer func() {
			r.mu.Lock()
			r.signalRunning = false
			r.mu.Unlock()
			// Send a keyframe so newly attached receivers render immediately
			// instead of waiting for the next periodic IDR. Critical for
			// "join the call and see existing video right away" UX.
			r.dispatchKeyFrame()
		}()

		const maxAttempts = 25
		for attempt := 0; attempt < maxAttempts; attempt++ {
			if !r.attemptSync() {
				return
			}
		}
		// Could not converge — schedule a retry without holding the lock.
		// This matches Pion's reference implementation behaviour.
		go func() {
			time.Sleep(3 * time.Second)
			r.SignalAllPeers()
		}()
	}()
}

// attemptSync makes one pass over every peer; returns true if any peer needs
// the loop to start over (because the slice was modified or a tx call failed).
func (r *Room) attemptSync() bool {
	r.mu.Lock()
	// Snapshot peers + tracks under lock so we can release it during slow ops.
	peerSnapshot := make([]*Peer, 0, len(r.peers))
	for _, p := range r.peers {
		peerSnapshot = append(peerSnapshot, p)
	}
	trackSnapshot := make(map[string]*webrtc.TrackLocalStaticRTP, len(r.localTracks))
	for k, v := range r.localTracks {
		trackSnapshot[k] = v
	}
	r.mu.Unlock()

	for _, p := range peerSnapshot {
		if p.PC.ConnectionState() == webrtc.PeerConnectionStateClosed {
			r.RemovePeer(p.UserID)
			return true
		}

		existing := map[string]bool{}

		// Drop senders whose track is no longer in the room.
		for _, sender := range p.PC.GetSenders() {
			if sender.Track() == nil {
				continue
			}
			id := sender.Track().ID()
			existing[id] = true
			if _, ok := trackSnapshot[id]; !ok {
				if err := p.PC.RemoveTrack(sender); err != nil {
					r.logger.Warn("sfu: remove sender failed", "user_id", p.UserID, "error", err)
					return true
				}
			}
		}

		// Avoid sending a peer its own published tracks (loopback).
		for _, receiver := range p.PC.GetReceivers() {
			if receiver.Track() == nil {
				continue
			}
			existing[receiver.Track().ID()] = true
		}

		// Add missing tracks to this peer.
		for id, t := range trackSnapshot {
			if existing[id] {
				continue
			}
			if _, err := p.PC.AddTrack(t); err != nil {
				r.logger.Warn("sfu: add track failed", "user_id", p.UserID, "error", err)
				return true
			}
		}

		// Renegotiate.
		offer, err := p.PC.CreateOffer(nil)
		if err != nil {
			r.logger.Warn("sfu: create offer failed", "user_id", p.UserID, "error", err)
			return true
		}
		if err := p.PC.SetLocalDescription(offer); err != nil {
			r.logger.Warn("sfu: set local desc failed", "user_id", p.UserID, "error", err)
			return true
		}
		if err := p.WriteOffer(offer); err != nil {
			r.logger.Warn("sfu: write offer failed", "user_id", p.UserID, "error", err)
			return true
		}
	}

	return false
}

// dispatchKeyFrame sends a PLI on every receiver of every peer so that the
// publishing client emits a fresh IDR frame. We do this on join + every
// renegotiation so new receivers render the existing video stream immediately.
func (r *Room) dispatchKeyFrame() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.peers {
		for _, recv := range p.PC.GetReceivers() {
			if recv.Track() == nil {
				continue
			}
			_ = p.PC.WriteRTCP([]rtcp.Packet{
				&rtcp.PictureLossIndication{MediaSSRC: uint32(recv.Track().SSRC())},
			})
		}
	}
}

// closeAll terminates every peer in the room (used by SFU.CloseRoom).
func (r *Room) closeAll() {
	r.mu.Lock()
	peers := make([]*Peer, 0, len(r.peers))
	for _, p := range r.peers {
		peers = append(peers, p)
	}
	r.peers = make(map[uuid.UUID]*Peer)
	r.localTracks = make(map[string]*webrtc.TrackLocalStaticRTP)
	r.mu.Unlock()
	for _, p := range peers {
		p.Close()
	}
}
