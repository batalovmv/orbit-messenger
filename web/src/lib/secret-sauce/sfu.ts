// Saturn SFU client (Phase 6 Stage 3 — group voice/video calls).
//
// Connects the browser to the Pion SFU running inside the calls service via
// the gateway WebSocket proxy at /api/v1/calls/:id/sfu-ws. The protocol is
// server-driven: the SFU creates SDP offers and the client replies with
// answers. This mirrors the canonical Pion sfu-ws example so future
// maintainers can cross-reference upstream patterns.
//
// Lifecycle:
//   1. joinSfuCall — open WS, send auth frame, wait for auth_ok, accept first
//      offer, attach local media, send answer, start receiving remote tracks.
//   2. toggleSfuMute / toggleSfuVideo / toggleSfuScreenShare — flip the
//      enabled flag on the corresponding local track. The SFU keeps forwarding
//      the (silent / black) RTP so peer state stays consistent.
//   3. leaveSfuCall — close PC + WS. Server-side cleanup is handled by
//      LeaveGroupCall in the calls service when the WS drops.
//
// This module intentionally does NOT depend on the legacy TG Web A
// secretsauce.ts (Colibri) signaling — Saturn calls only use Pion.

export interface SfuRemoteTrack {
  userId: string; // SFU prefixes track IDs with the publishing user's UUID
  trackId: string;
  stream: MediaStream;
  kind: 'audio' | 'video';
}

export interface SfuJoinOptions {
  callId: string;
  wsBaseUrl: string; // e.g. ws://localhost:8080/api/v1
  accessToken: string;
  iceServers?: RTCIceServer[];
  isVideo: boolean;
  onRemoteTrack: (track: SfuRemoteTrack) => void;
  onRemoteTrackRemoved: (trackId: string) => void;
  onConnectionStateChange?: (state: RTCPeerConnectionState) => void;
  onError?: (error: Error) => void;
}

export interface SfuSession {
  pc: RTCPeerConnection;
  ws: WebSocket;
  localStream: MediaStream;
  callId: string;
}

interface SignalMessage {
  event: 'offer' | 'answer' | 'candidate';
  data: string;
}

let activeSession: SfuSession | undefined;

export function getActiveSfuSession(): SfuSession | undefined {
  return activeSession;
}

/**
 * Open a WebSocket to the SFU, perform the auth handshake and the first
 * server-driven SDP exchange. Returns once the PeerConnection has reached
 * the `connected` state OR `failed` (the latter rejects).
 */
export async function joinSfuCall(options: SfuJoinOptions): Promise<SfuSession> {
  if (activeSession) {
    throw new Error('SFU session already in progress');
  }

  // Acquire local media BEFORE the WS handshake so the SDP answer we send
  // back to the SFU contains the right tracks.
  const localStream = await navigator.mediaDevices.getUserMedia({
    audio: true,
    video: options.isVideo ? { width: 1280, height: 720 } : false,
  });

  const pc = new RTCPeerConnection({
    iceServers: options.iceServers || [{ urls: 'stun:stun.l.google.com:19302' }],
  });

  // Add local tracks immediately. The SFU's first offer is recvonly so we
  // need at least one track per kind on our side to negotiate sendrecv on
  // the answer.
  for (const track of localStream.getTracks()) {
    pc.addTrack(track, localStream);
  }

  const wsUrl = `${options.wsBaseUrl.replace(/\/$/, '')}/calls/${options.callId}/sfu-ws`;
  const ws = new WebSocket(wsUrl);

  const session: SfuSession = {
    pc,
    ws,
    localStream,
    callId: options.callId,
  };

  // Forward server candidates upstream and trickle our own back.
  pc.onicecandidate = (event) => {
    if (!event.candidate) return;
    sendSignal(ws, {
      event: 'candidate',
      data: JSON.stringify(event.candidate.toJSON()),
    });
  };

  pc.ontrack = (event) => {
    const remoteStream = event.streams[0] || new MediaStream([event.track]);
    const stream = remoteStream;
    const trackId = event.track.id;
    // SFU prefixes track IDs as "<userUUID>:<remoteTrackID>" so we can
    // de-multiplex incoming streams by publisher.
    const [prefix] = trackId.split(':', 1);
    const userId = prefix && prefix.length === 36 ? prefix : '';
    options.onRemoteTrack({
      userId,
      trackId,
      stream,
      kind: event.track.kind === 'audio' ? 'audio' : 'video',
    });
    event.track.onended = () => {
      options.onRemoteTrackRemoved(trackId);
    };
    stream.onremovetrack = (e) => {
      if (e.track.id === trackId) {
        options.onRemoteTrackRemoved(trackId);
      }
    };
  };

  pc.onconnectionstatechange = () => {
    options.onConnectionStateChange?.(pc.connectionState);
    if (pc.connectionState === 'failed' || pc.connectionState === 'closed') {
      cleanup(session);
    }
  };

  // Drive the handshake from inside a Promise so callers can await readiness.
  return new Promise<SfuSession>((resolve, reject) => {
    let resolved = false;
    const finishOk = () => {
      if (resolved) return;
      resolved = true;
      activeSession = session;
      resolve(session);
    };
    const finishErr = (err: Error) => {
      if (resolved) return;
      resolved = true;
      cleanup(session);
      reject(err);
      options.onError?.(err);
    };

    ws.onopen = () => {
      // Auth frame matches /api/v1/ws — token in payload, never in URL.
      ws.send(JSON.stringify({ type: 'auth', data: { token: options.accessToken } }));
    };

    ws.onerror = () => {
      finishErr(new Error('SFU WebSocket error'));
    };

    ws.onclose = () => {
      cleanup(session);
    };

    ws.onmessage = async (event) => {
      let parsed: { type?: string; event?: string; data?: any };
      try {
        parsed = JSON.parse(event.data as string);
      } catch {
        return;
      }

      // Auth handshake (gateway-side messages use {type, data})
      if (parsed.type === 'auth_ok') {
        // Wait for the SFU's first offer — no action needed here.
        return;
      }
      if (parsed.type === 'error') {
        finishErr(new Error(typeof parsed.data?.message === 'string' ? parsed.data.message : 'sfu error'));
        return;
      }

      // SFU signal frames use {event, data}
      if (parsed.event === 'offer' && typeof parsed.data === 'string') {
        try {
          const offer: RTCSessionDescriptionInit = JSON.parse(parsed.data);
          await pc.setRemoteDescription(offer);
          const answer = await pc.createAnswer();
          await pc.setLocalDescription(answer);
          sendSignal(ws, {
            event: 'answer',
            data: JSON.stringify(pc.localDescription),
          });
          // First successful offer/answer = session is established. The
          // connectionState callback will refine this with `connected` or
          // `failed` later.
          finishOk();
        } catch (err) {
          finishErr(err instanceof Error ? err : new Error(String(err)));
        }
        return;
      }

      if (parsed.event === 'candidate' && typeof parsed.data === 'string') {
        try {
          const cand: RTCIceCandidateInit = JSON.parse(parsed.data);
          await pc.addIceCandidate(cand);
        } catch (err) {
          // eslint-disable-next-line no-console
          console.warn('[SFU] addIceCandidate failed', err);
        }
      }
    };
  });
}

/** Toggle the local audio track on/off without renegotiating. */
export function toggleSfuMute(muted: boolean) {
  if (!activeSession) return;
  for (const track of activeSession.localStream.getAudioTracks()) {
    track.enabled = !muted;
  }
}

/** Toggle the local video track on/off without renegotiating. */
export function toggleSfuVideo(enabled: boolean) {
  if (!activeSession) return;
  for (const track of activeSession.localStream.getVideoTracks()) {
    track.enabled = enabled;
  }
}

/**
 * Replace the camera track with a screen-share track. Pass `false` to
 * restore the camera. The SFU automatically forwards the new RTP because
 * we keep the same RTPSender — no renegotiation required.
 */
export async function toggleSfuScreenShare(enable: boolean): Promise<void> {
  if (!activeSession) return;
  const sender = activeSession.pc.getSenders().find((s) => s.track?.kind === 'video');
  if (!sender) return;

  if (enable) {
    const display = await (navigator.mediaDevices as MediaDevices & {
      getDisplayMedia: (c: { video: true; audio: false }) => Promise<MediaStream>;
    }).getDisplayMedia({ video: true, audio: false });
    const screenTrack = display.getVideoTracks()[0];
    if (!screenTrack) return;
    await sender.replaceTrack(screenTrack);
    // Auto-restore camera when the user clicks the browser "Stop sharing" UI.
    screenTrack.onended = () => {
      void toggleSfuScreenShare(false);
    };
    return;
  }

  // Restore the original camera track from the local stream snapshot.
  const cameraTrack = activeSession.localStream.getVideoTracks()[0];
  if (cameraTrack) {
    await sender.replaceTrack(cameraTrack);
  }
}

/** Tear down the SFU session and release media. */
export function leaveSfuCall(): void {
  if (!activeSession) return;
  cleanup(activeSession);
}

function cleanup(session: SfuSession): void {
  try {
    for (const track of session.localStream.getTracks()) {
      track.stop();
    }
  } catch {
    // ignore
  }
  try {
    session.pc.close();
  } catch {
    // ignore
  }
  try {
    if (session.ws.readyState === WebSocket.OPEN || session.ws.readyState === WebSocket.CONNECTING) {
      session.ws.close();
    }
  } catch {
    // ignore
  }
  if (activeSession === session) {
    activeSession = undefined;
  }
}

function sendSignal(ws: WebSocket, msg: SignalMessage) {
  if (ws.readyState !== WebSocket.OPEN) return;
  ws.send(JSON.stringify(msg));
}
