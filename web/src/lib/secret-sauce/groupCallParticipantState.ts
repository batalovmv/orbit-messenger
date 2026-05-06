// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Tiny pub/sub for SFU group-call per-participant indicators (mute,
// screen-share). Lives outside the global store on purpose: the tile
// indicator is local to the GroupCall panel and Teact's withGlobal
// memoization gets noisy when high-frequency mute toggles fan into the
// global state. This keeps the signal scoped to the SFU stream manager
// hook that already lives in the call panel's lifetime.
//
// wsHandler emits here on `call_muted` / `call_unmuted` / `screen_share_*`
// in addition to the legacy `updatePhoneCallPeerState` it dispatches for
// the (pre-SFU) P2P UI. The two consumers are now decoupled — the broken
// P2P shape that wsHandler.ts:737 inherits no longer pollutes group tiles.

export type GroupCallParticipantStateEvent =
  | { kind: 'mute'; userId: string; muted: boolean }
  | { kind: 'screenshare'; userId: string; sharing: boolean };

type Listener = (event: GroupCallParticipantStateEvent) => void;

const listeners = new Set<Listener>();

export function subscribeGroupCallParticipantState(listener: Listener) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function emitGroupCallParticipantState(event: GroupCallParticipantStateEvent) {
  for (const listener of listeners) {
    try {
      listener(event);
    } catch {
      // Listener errors must never bubble — one bad subscriber would
      // suppress notifications to everyone else in the same loop.
    }
  }
}
