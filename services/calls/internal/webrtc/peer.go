// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package sfu

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// SignalMessage is the JSON envelope used over the WebSocket between the
// browser client and the SFU. The shape mirrors the canonical Pion sfu-ws
// example so that future migrations can rely on the same wire format.
//
//	Server → Client:
//	  {event: "offer",     data: "<JSON SDP>"}
//	  {event: "candidate", data: "<JSON ICECandidateInit>"}
//
//	Client → Server:
//	  {event: "answer",    data: "<JSON SDP>"}
//	  {event: "candidate", data: "<JSON ICECandidateInit>"}
type SignalMessage struct {
	Event string `json:"event"`
	Data  string `json:"data"`
}

// WSConn is the minimal subset of the gofiber/contrib/websocket connection
// type that we need from inside the webrtc package. Defining it as an
// interface keeps the package decoupled from the fiber import (which would
// otherwise drag fasthttp into the SFU code) and lets handler tests inject
// a fake conn without spinning up a real WebSocket.
type WSConn interface {
	WriteJSON(v any) error
	Close() error
}

// Peer is one client's PeerConnection plus its WebSocket signaling channel.
// Each peer is owned by exactly one Room. The peer is created on WS upgrade
// and torn down when the WS closes or the PeerConnection fails.
type Peer struct {
	UserID uuid.UUID
	PC     *webrtc.PeerConnection
	WS     WSConn
	logger *slog.Logger
	room   *Room

	wsMu     sync.Mutex // serializes WS writes
	closed   bool
	closeMu  sync.Mutex
	closeFns []func()
}

// NewPeer wires up a fresh PeerConnection that is ready to receive audio +
// video from the client. The handler then calls room.AddPeer to register it
// in the SFU and start the first renegotiation pass.
//
// The peer is server-driven: the SFU creates offers and the client replies
// with answers. This matches the canonical Pion SFU pattern and lets us add
// or remove tracks at any time without coordinating with the client.
func NewPeer(userID uuid.UUID, ws WSConn, room *Room, logger *slog.Logger) (*Peer, error) {
	pc, err := room.api().NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, err
	}

	// We must add at least one transceiver per kind we plan to receive
	// before creating an offer; otherwise the SDP will not include the
	// media sections and the client cannot send back the matching tracks.
	for _, kind := range []webrtc.RTPCodecType{
		webrtc.RTPCodecTypeAudio,
		webrtc.RTPCodecTypeVideo,
	} {
		if _, err := pc.AddTransceiverFromKind(kind, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		}); err != nil {
			_ = pc.Close()
			return nil, err
		}
	}

	p := &Peer{
		UserID: userID,
		PC:     pc,
		WS:     ws,
		logger: logger.With("user_id", userID.String()),
		room:   room,
	}

	// Trickle ICE — forward server candidates to the client.
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		raw, err := json.Marshal(c.ToJSON())
		if err != nil {
			p.logger.Warn("sfu: marshal candidate", "error", err)
			return
		}
		if err := p.writeJSON(&SignalMessage{Event: "candidate", Data: string(raw)}); err != nil {
			p.logger.Warn("sfu: write candidate", "error", err)
		}
	})

	// Cleanup when the connection terminates. We never call signalPeers
	// here directly to avoid races — Room.attemptSync detects closed PCs.
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		p.logger.Info("sfu: connection state", "state", state.String())
		switch state {
		case webrtc.PeerConnectionStateFailed,
			webrtc.PeerConnectionStateClosed,
			webrtc.PeerConnectionStateDisconnected:
			// Yank the peer from the room. Room.RemovePeer is idempotent.
			go room.RemovePeer(p.UserID)
		}
	})

	// Receive media from the client and fan it out to every other peer.
	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		p.logger.Info("sfu: incoming track",
			"kind", remote.Kind().String(),
			"ssrc", remote.SSRC(),
			"track_id", remote.ID(),
			"stream_id", remote.StreamID(),
			"payload_type", remote.PayloadType())

		// Tracks are scoped per-peer with the user ID prefix so that the
		// same RTP track ID coming from a different peer never collides
		// (the room only stores one entry per ID).
		localID := p.UserID.String() + ":" + remote.ID()
		local, err := webrtc.NewTrackLocalStaticRTP(
			remote.Codec().RTPCodecCapability,
			localID,
			p.UserID.String()+":"+remote.StreamID(),
		)
		if err != nil {
			p.logger.Error("sfu: create local track", "error", err)
			return
		}
		room.AddLocalTrack(localID, local)
		// Best-effort cleanup if the publishing peer disappears.
		p.onClose(func() { room.RemoveLocalTrack(localID) })

		buf := make([]byte, 1500)
		pkt := &rtp.Packet{}
		for {
			n, _, readErr := remote.Read(buf)
			if readErr != nil {
				room.RemoveLocalTrack(localID)
				return
			}
			if err := pkt.Unmarshal(buf[:n]); err != nil {
				p.logger.Warn("sfu: unmarshal rtp", "error", err)
				continue
			}
			// Strip header extensions before re-emitting; the SFU does not
			// negotiate them per-peer and forwarding stale extension IDs
			// confuses receivers (matches Pion's reference SFU).
			pkt.Extension = false
			pkt.Extensions = nil
			if err := local.WriteRTP(pkt); err != nil {
				// io.ErrClosedPipe is the canonical "this track is gone"
				// signal from Pion when the underlying RTPSender has been
				// torn down. Treat it as a normal exit, not an error.
				if errors.Is(err, io.ErrClosedPipe) {
					return
				}
				return
			}
		}
	})

	return p, nil
}

// HandleAnswer applies an SDP answer received from the client.
func (p *Peer) HandleAnswer(sdp string) error {
	answer := webrtc.SessionDescription{}
	if err := json.Unmarshal([]byte(sdp), &answer); err != nil {
		return err
	}
	return p.PC.SetRemoteDescription(answer)
}

// AddICECandidate applies a trickle-ICE candidate received from the client.
func (p *Peer) AddICECandidate(payload string) error {
	cand := webrtc.ICECandidateInit{}
	if err := json.Unmarshal([]byte(payload), &cand); err != nil {
		return err
	}
	return p.PC.AddICECandidate(cand)
}

// WriteOffer sends an SDP offer to the client. Called by Room.attemptSync.
func (p *Peer) WriteOffer(offer webrtc.SessionDescription) error {
	raw, err := json.Marshal(offer)
	if err != nil {
		return err
	}
	return p.writeJSON(&SignalMessage{Event: "offer", Data: string(raw)})
}

// Close terminates the peer connection and runs registered cleanup hooks.
// Safe to call multiple times.
func (p *Peer) Close() {
	p.closeMu.Lock()
	if p.closed {
		p.closeMu.Unlock()
		return
	}
	p.closed = true
	fns := p.closeFns
	p.closeFns = nil
	p.closeMu.Unlock()

	_ = p.PC.Close()
	_ = p.WS.Close()
	for _, fn := range fns {
		fn()
	}
}

func (p *Peer) onClose(fn func()) {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()
	if p.closed {
		go fn()
		return
	}
	p.closeFns = append(p.closeFns, fn)
}

func (p *Peer) writeJSON(v any) error {
	p.wsMu.Lock()
	defer p.wsMu.Unlock()
	return p.WS.WriteJSON(v)
}

// api returns the Pion API the room was created with. Stored on Room (not
// passed to NewPeer separately) so the handler doesn't need to plumb it.
func (r *Room) api() *webrtc.API {
	return r.apiRef
}
