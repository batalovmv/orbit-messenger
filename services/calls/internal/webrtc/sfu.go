// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package sfu (directory: internal/webrtc) implements a Pion-based Selective
// Forwarding Unit for group
// voice/video calls. Each call_id maps to a Room. Each authenticated user that
// joins the room becomes a Peer with one PeerConnection to the SFU. The SFU
// receives RTP from each peer and forwards it to all other peers in the room.
//
// Routing rules:
//   - 1-on-1 calls (mode='p2p') do NOT use the SFU; they continue to flow
//     directly between the two browsers (Stage 1+2 P2P pipeline).
//   - Group calls (mode='group') route every participant through this SFU.
//
// The implementation is intentionally close to the canonical Pion sfu-ws
// example so that future maintainers can cross-reference upstream patterns.
package sfu

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// SFU is the top-level container that owns all active rooms.
type SFU struct {
	api    *webrtc.API
	logger *slog.Logger

	mu    sync.RWMutex
	rooms map[uuid.UUID]*Room
}

// NewSFU constructs an SFU with a MediaEngine that supports the codecs we use
// in production (Opus + VP8). Other codecs are intentionally omitted to keep
// SDP small and simplify cross-browser interop until we have a real need.
func NewSFU(logger *slog.Logger) (*SFU, error) {
	api, err := newAPI()
	if err != nil {
		return nil, err
	}
	return &SFU{
		api:    api,
		logger: logger,
		rooms:  make(map[uuid.UUID]*Room),
	}, nil
}

// GetOrCreateRoom returns the Room for the given call ID, creating one if
// none exists yet. Rooms are created lazily on the first peer's join.
func (s *SFU) GetOrCreateRoom(callID uuid.UUID) *Room {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.rooms[callID]; ok {
		return r
	}
	r := newRoom(callID, s.api, s.logger.With("room", callID.String()))
	s.rooms[callID] = r
	return r
}

// GetRoom returns the room for the given call ID, or nil if it does not exist.
func (s *SFU) GetRoom(callID uuid.UUID) *Room {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rooms[callID]
}

// CloseRoom evicts all peers and removes the room from the registry. Safe to
// call concurrently and on rooms that have already been closed.
func (s *SFU) CloseRoom(callID uuid.UUID) {
	s.mu.Lock()
	r, ok := s.rooms[callID]
	if !ok {
		s.mu.Unlock()
		return
	}
	delete(s.rooms, callID)
	s.mu.Unlock()
	r.closeAll()
}

// API exposes the underlying Pion API so the handler can construct
// PeerConnections that share the SFU's MediaEngine + InterceptorRegistry.
func (s *SFU) API() *webrtc.API {
	return s.api
}

// RoomCount returns the number of active rooms (used for /metrics).
func (s *SFU) RoomCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.rooms)
}

// StartCleanupLoop runs a goroutine that periodically removes rooms whose
// peers have all disconnected. Without this, a room with a stale entry could
// linger forever if every peer dropped its connection without sending an
// explicit leave (e.g. browser crash). Stops when ctx is cancelled.
func (s *SFU) StartCleanupLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupEmptyRooms()
		}
	}
}

func (s *SFU) cleanupEmptyRooms() {
	s.mu.Lock()
	var emptyIDs []uuid.UUID
	for id, r := range s.rooms {
		if r.PeerCount() == 0 {
			emptyIDs = append(emptyIDs, id)
			delete(s.rooms, id)
		}
	}
	s.mu.Unlock()
	for _, id := range emptyIDs {
		s.logger.Info("sfu: removed empty room", "room", id)
	}
}
