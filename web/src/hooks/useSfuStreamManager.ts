// Saturn SFU Stream Adapter for GroupCall UI
//
// This hook provides a bridge between the Saturn SFU client (sfu.ts)
// and the GroupCall UI components. It manages remote tracks in local
// state (NOT in global store — MediaStream cannot be serialized).
//
// Usage:
//   const { remoteStreams, isConnected, join, leave } = useSfuStreamManager(callId);
//   // remoteStreams passed to video grid components

import { useCallback, useEffect, useRef, useState } from '../lib/teact/teact';
import { getAccessToken, getBaseUrl } from '../api/saturn/client';
import {
  type SfuRemoteTrack,
  type SfuSession,
  joinSfuCall,
  leaveSfuCall,
  toggleSfuMute,
  toggleSfuVideo,
  toggleSfuScreenShare,
} from '../lib/secret-sauce/sfu';
import { getGlobal } from '../global';
import { fetchICEServers } from '../api/saturn/methods/calls';

export interface SfuStreamManager {
  remoteStreams: Map<string, SfuRemoteTrack>;
  localStream: MediaStream | null;
  isConnecting: boolean;
  isConnected: boolean;
  error: Error | null;
  join: () => Promise<void>;
  leave: () => void;
  toggleMute: (muted: boolean) => void;
  toggleVideo: (enabled: boolean) => void;
  toggleScreenShare: (enabled: boolean) => Promise<void>;
}

// Workaround: TypeScript doesn't like instantiating Map with type args in useState
const createEmptyStreamMap = () => new Map<string, SfuRemoteTrack>();

export function useSfuStreamManager(callId: string | undefined): SfuStreamManager {
  const [remoteStreams, setRemoteStreams] = useState<Map<string, SfuRemoteTrack>>(createEmptyStreamMap);
  const [localStream, setLocalStream] = useState<MediaStream | null>(null);
  const [isConnecting, setIsConnecting] = useState(false);
  const [isConnected, setIsConnected] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const sessionRef = useRef<SfuSession | null>(null);

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

  const join = useCallback(async () => {
    if (!callId || isConnecting || isConnected) return;

    setIsConnecting(true);
    setError(null);

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
  }, [callId, isConnecting, isConnected, handleRemoteTrack, handleRemoteTrackRemoved]);

  const leave = useCallback(() => {
    if (sessionRef.current) {
      leaveSfuCall();
      sessionRef.current = null;
    }
    setLocalStream(null);
    setRemoteStreams(new Map());
    setIsConnected(false);
  }, []);

  const toggleMute = useCallback((muted: boolean) => {
    toggleSfuMute(muted);
  }, []);

  const toggleVideo = useCallback((enabled: boolean) => {
    toggleSfuVideo(enabled);
  }, []);

  const toggleScreenShare = useCallback(async (enabled: boolean) => {
    await toggleSfuScreenShare(enabled);
  }, []);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (sessionRef.current) {
        leaveSfuCall();
      }
    };
  }, []);

  return {
    remoteStreams,
    localStream,
    isConnecting,
    isConnected,
    error,
    join,
    leave,
    toggleMute,
    toggleVideo,
    toggleScreenShare,
  };
}