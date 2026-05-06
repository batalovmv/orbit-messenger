// Saturn SFU Stream Adapter for GroupCall UI
//
// This hook provides a bridge between the Saturn SFU client (sfu.ts)
// and the GroupCall UI components. It manages remote tracks in local
// state (NOT in global store — MediaStream cannot be serialized).
//
// Usage:
//   const { remoteStreams, isConnected, join, leave } = useSfuStreamManager(callId);
//   // remoteStreams passed to video grid components

import { useCallback, useEffect, useMemo, useRef, useState } from '../lib/teact/teact';
import { getGlobal } from '../global';

import {
  joinSfuCall,
  leaveSfuCall,
  type SfuRemoteTrack,
  type SfuSession,
  toggleSfuMute,
  toggleSfuScreenShare,
  toggleSfuVideo,
} from '../lib/secret-sauce/sfu';
import { subscribeGroupCallParticipantState } from '../lib/secret-sauce/groupCallParticipantState';
import { getAccessToken, getBaseUrl, request } from '../api/saturn/client';
import { fetchICEServers } from '../api/saturn/methods/calls';
import useLastCallback from './useLastCallback';

export interface SfuParticipantState {
  isMuted: boolean;
  isScreenSharing: boolean;
}

export interface SfuStreamManager {
  remoteStreams: Map<string, SfuRemoteTrack>;
  participantStates: Map<string, SfuParticipantState>;
  localStream?: MediaStream;
  isConnecting: boolean;
  isConnected: boolean;
  error?: Error;
  join: () => Promise<void>;
  leave: () => void;
  toggleMute: (muted: boolean) => void;
  toggleVideo: (enabled: boolean) => void;
  toggleScreenShare: (enabled: boolean) => Promise<void>;
}

// Workaround: TypeScript doesn't like instantiating Map with type args in useState
const createEmptyStreamMap = () => new Map<string, SfuRemoteTrack>();
const createEmptyStateMap = () => new Map<string, SfuParticipantState>();

export function useSfuStreamManager(callId: string | undefined): SfuStreamManager {
  const [remoteStreams, setRemoteStreams] = useState<Map<string, SfuRemoteTrack>>(createEmptyStreamMap);
  const [participantStates, setParticipantStates] = useState<Map<string, SfuParticipantState>>(createEmptyStateMap);
  const [localStream, setLocalStream] = useState<MediaStream | undefined>();
  const [isConnecting, setIsConnecting] = useState(false);
  const [isConnected, setIsConnected] = useState(false);
  const [error, setError] = useState<Error | undefined>();
  const sessionRef = useRef<SfuSession | undefined>();

  // Listen for SFU participant state changes (mute, screen-share). Bound
  // to callId so a panel-remount on call switch starts with a fresh map
  // — stale state from the previous call would otherwise paint wrong
  // indicators for the first frame.
  useEffect(() => {
    if (!callId) return undefined;
    setParticipantStates(createEmptyStateMap());
    const unsubscribe = subscribeGroupCallParticipantState((event) => {
      setParticipantStates((prev) => {
        const next = new Map(prev);
        const current = next.get(event.userId) ?? { isMuted: false, isScreenSharing: false };
        if (event.kind === 'mute') {
          next.set(event.userId, { ...current, isMuted: event.muted });
        } else {
          next.set(event.userId, { ...current, isScreenSharing: event.sharing });
        }
        return next;
      });
    });
    return unsubscribe;
  }, [callId]);

  const handleRemoteTrack = useCallback((track: SfuRemoteTrack) => {
    setRemoteStreams((prev) => {
      const next = new Map(prev);
      const key = `${track.userId}:${track.trackId}`;
      next.set(key, track);
      return next;
    });
  }, []);

  const handleRemoteTrackRemoved = useCallback((trackId: string) => {
    setRemoteStreams((prev) => {
      const next = new Map(prev);
      // Find and remove by trackId
      for (const [key, track] of next) {
        if (track.trackId === trackId) {
          next.delete(key);
          break;
        }
      }
      return next;
    });
  }, []);

  // Refs let join() see fresh isConnecting/isConnected values without
  // listing them as deps — so join's identity stays stable across the
  // ~50 renders that fire while the call is mounting (otherwise
  // GroupCall.tsx's effect re-fires per render and tears down the SFU
  // session within a few ms of joining — the 2026-05-05 thrash bug).
  const isConnectingRef = useRef(isConnecting);
  const isConnectedRef = useRef(isConnected);
  isConnectingRef.current = isConnecting;
  isConnectedRef.current = isConnected;

  const join = useLastCallback(async () => {
    if (!callId || isConnectingRef.current || isConnectedRef.current) return;

    setIsConnecting(true);
    setError(undefined);

    try {
      const global = getGlobal();
      const currentUserId = global.currentUserId;
      if (!currentUserId) {
        throw new Error('Not authenticated');
      }

      // Fetch chat type to determine video mode
      // For now, default to voice (isVideo: false)
      const isVideo = false;
      const accessToken = getAccessToken();
      if (!accessToken) {
        throw new Error('Not authenticated: missing access token');
      }
      const wsBaseUrl = getBaseUrl().replace(/^http/, 'ws');

      // Register this user as a call participant BEFORE opening the SFU
      // WebSocket. The gateway sfu_proxy enforces a `checkCallMembership`
      // gate (sfu_proxy.go:170) and rejects WS upgrades for users who
      // aren't in the participants table. The initiator is auto-added by
      // CreateCall, so this is a no-op for them; for joiners (bob/carol
      // in the e2e fixture) it's load-bearing — without it the WS is
      // closed before the auth_ok frame can fly.
      // 409 / "already a participant" is fine and expected on rejoins.
      try {
        await request<{ status: string }>('POST', `/calls/${callId}/join`);
      } catch (e) {
        // eslint-disable-next-line no-console
        console.warn('[useSfuStreamManager] join REST failed', e);
      }

      // Fetch ICE servers from the calls service so coturn is used for NAT traversal.
      const iceServersRaw = await fetchICEServers({ callId });
      const iceServers: RTCIceServer[] = (iceServersRaw || []).map((s) => ({
        urls: s.urls,
        username: s.username,
        credential: s.credential,
      }));

      const session = await joinSfuCall({
        callId,
        wsBaseUrl,
        accessToken,
        iceServers,
        isVideo,
        onRemoteTrack: handleRemoteTrack,
        onRemoteTrackRemoved: handleRemoteTrackRemoved,
        onConnectionStateChange: (state) => {
          if (state === 'connected') {
            setIsConnecting(false);
            setIsConnected(true);
          } else if (state === 'failed' || state === 'closed') {
            setIsConnecting(false);
            setIsConnected(false);
          }
        },
        onError: (err) => {
          setError(err);
          setIsConnecting(false);
        },
      });

      sessionRef.current = session;
      setLocalStream(session.localStream);
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
      setIsConnecting(false);
    }
  });

  const leave = useLastCallback(() => {
    if (sessionRef.current) {
      leaveSfuCall();
      sessionRef.current = undefined;
    }
    setLocalStream(undefined);
    setRemoteStreams(new Map());
    setIsConnected(false);
  });

  const toggleMute = useLastCallback((muted: boolean) => {
    toggleSfuMute(muted);
  });

  const toggleVideo = useLastCallback((enabled: boolean) => {
    toggleSfuVideo(enabled);
  });

  const toggleScreenShare = useLastCallback(async (enabled: boolean) => {
    await toggleSfuScreenShare(enabled);
  });

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (sessionRef.current) {
        leaveSfuCall();
      }
    };
  }, []);

  // Memoize the returned object so it changes identity only when the
  // exposed STATE actually changes — not on every render. Callbacks
  // are stable via useLastCallback, so only the state values feed deps.
  return useMemo(() => ({
    remoteStreams,
    participantStates,
    localStream,
    isConnecting,
    isConnected,
    error,
    join,
    leave,
    toggleMute,
    toggleVideo,
    toggleScreenShare,
  }), [
    remoteStreams,
    participantStates,
    localStream,
    isConnecting,
    isConnected,
    error,
    join,
    leave,
    toggleMute,
    toggleVideo,
    toggleScreenShare,
  ]);
}
