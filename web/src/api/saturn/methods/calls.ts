// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import type { ApiPhoneCall, ApiUser } from '../../types';
import type { ApiPhoneCallConnection, ApiCallProtocol } from '../../../lib/secret-sauce';
import {
  leaveSfuCall,
} from '../../../lib/secret-sauce/sfu';

import { sendApiUpdate } from '../updates/apiUpdateEmitter';
import { request, sendWsMessage, getAccessToken, getBaseUrl } from '../client';

// Saturn call types
interface SaturnCall {
  id: string;
  type: string;
  mode: string;
  chat_id: string;
  initiator_id: string;
  status: string;
  started_at?: string;
  ended_at?: string;
  duration_seconds?: number;
  created_at: string;
  participants?: SaturnCallParticipant[];
  // Stage 3 — group calls return the SFU WebSocket URL the client should
  // open to join the media plane. Empty for p2p calls.
  sfu_ws_url?: string;
}

interface SaturnCallParticipant {
  call_id: string;
  user_id: string;
  joined_at?: string;
  left_at?: string;
  is_muted: boolean;
  is_camera_off: boolean;
  is_screen_sharing: boolean;
  display_name?: string;
  avatar_url?: string;
}

interface ICEServer {
  urls: string[];
  username?: string;
  credential?: string;
}

const DEFAULT_PROTOCOL: ApiCallProtocol = {
  libraryVersions: ['4.0.0'],
  minLayer: 92,
  maxLayer: 92,
  isUdpP2p: true,
  isUdpReflector: true,
};

function parseIceServerUrl(url: string): { host: string; port: number; isTurn: boolean; isStun: boolean } {
  // Handles: stun:host:port, turn:host:port, stun:host, turn:host?transport=udp
  const isTurn = url.startsWith('turn:');
  const isStun = url.startsWith('stun:');
  const withoutScheme = url.replace(/^(stun|turn|turns):/, '');
  const withoutParams = withoutScheme.split('?')[0];
  const parts = withoutParams.split(':');
  const host = parts[0];
  const port = parts[1] ? parseInt(parts[1], 10) : (isTurn ? 3478 : 19302);
  return { host, port, isTurn, isStun };
}

export function iceServersToConnections(servers: ICEServer[]): ApiPhoneCallConnection[] {
  const connections: ApiPhoneCallConnection[] = [];
  for (const server of servers) {
    for (const url of server.urls) {
      const { host, port, isTurn, isStun } = parseIceServerUrl(url);
      connections.push({
        ip: host,
        ipv6: '',
        port,
        username: server.username || '',
        password: server.credential || '',
        isTurn,
        isStun,
      });
    }
  }
  return connections;
}

// Store the current active call ID for signaling
let activeCallId: string | undefined;
let activeCallPeerId: string | undefined;
let activeCallMode: 'p2p' | 'group' | undefined;
// Timestamp (ms epoch) when the current call's media session started — used
// by discardCall to decide whether the call lasted long enough to prompt the
// user for a rating (>10s threshold, matching Telegram's behaviour).
let activeCallStartedAt: number | undefined;

// Stage 5 — calls shorter than this are not worth rating.
const RATING_MIN_DURATION_MS = 10_000;

// Dedupe concurrent requestCall invocations. The TG action layer dispatches
// connectToActivePhoneCall both from the requestCall UI handler and from the
// PhoneCall.tsx mount-effect, and the two invocations race past any
// async-aware guard (both pass the check before the first await sets a flag).
// Sharing the in-flight Promise here means the second caller gets the same
// result as the first instead of issuing a duplicate POST /calls.
let pendingRequestCall: Promise<SaturnCall | undefined> | undefined;

export function markCallStarted() {
  activeCallStartedAt = Date.now();
}

export function getActiveCallId() {
  return activeCallId;
}

export function setActiveCallId(id: string | undefined) {
  activeCallId = id;
}

export function setActiveCallPeerId(id: string | undefined) {
  activeCallPeerId = id;
}

export function getActiveCallPeerId() {
  return activeCallPeerId;
}

export function setActiveCallMode(mode: 'p2p' | 'group' | undefined) {
  activeCallMode = mode;
}

export function getActiveCallMode() {
  return activeCallMode;
}

// REST methods

export async function createCall({
  chatId, type, mode, memberIds,
}: {
  chatId: string;
  type: 'voice' | 'video';
  mode: 'p2p' | 'group';
  memberIds?: string[];
}): Promise<SaturnCall | undefined> {
  try {
    const result = await request<SaturnCall>('POST', '/calls', {
      chat_id: chatId,
      type,
      mode,
      member_ids: memberIds || [],
    });
    return result;
  } catch (e) {
    return undefined;
  }
}

export async function acceptCallApi({ callId }: { callId: string }): Promise<SaturnCall | undefined> {
  try {
    return await request<SaturnCall>('PUT', `/calls/${callId}/accept`);
  } catch (e) {
    return undefined;
  }
}

export async function declineCallApi({ callId }: { callId: string }): Promise<void> {
  try {
    await request('PUT', `/calls/${callId}/decline`);
  } catch {
    // ignore
  }
}

export async function endCallApi({ callId }: { callId: string }): Promise<void> {
  try {
    await request('PUT', `/calls/${callId}/end`);
  } catch {
    // ignore
  }
}

export async function fetchCall({ callId }: { callId: string }): Promise<SaturnCall | undefined> {
  try {
    return await request<SaturnCall>('GET', `/calls/${callId}`);
  } catch {
    return undefined;
  }
}

export async function fetchCallHistory({
  cursor, limit,
}: {
  cursor?: string;
  limit?: number;
} = {}): Promise<{ data: SaturnCall[]; cursor: string; has_more: boolean } | undefined> {
  try {
    const params = new URLSearchParams();
    if (limit) params.set('limit', String(limit));
    if (cursor) params.set('cursor', cursor);
    const qs = params.toString();
    return await request('GET', `/calls/history${qs ? `?${qs}` : ''}`);
  } catch {
    return undefined;
  }
}

export async function toggleCallMute({ callId, muted }: { callId: string; muted: boolean }): Promise<void> {
  try {
    await request('PUT', `/calls/${callId}/mute`, { muted });
  } catch {
    // ignore
  }
}

export async function startScreenShareApi({ callId }: { callId: string }): Promise<void> {
  try {
    await request('PUT', `/calls/${callId}/screen-share/start`);
  } catch {
    // ignore
  }
}

export async function stopScreenShareApi({ callId }: { callId: string }): Promise<void> {
  try {
    await request('PUT', `/calls/${callId}/screen-share/stop`);
  } catch {
    // ignore
  }
}

export async function fetchICEServers({ callId }: { callId: string }): Promise<ICEServer[] | undefined> {
  try {
    const result = await request<{ ice_servers: ICEServer[] }>('GET', `/calls/${callId}/ice-servers`);
    return result?.ice_servers;
  } catch {
    return undefined;
  }
}

// WebSocket signaling methods

export function sendWebRTCOffer(callId: string, targetUserId: string, sdp: string) {
  sendWsMessage('webrtc_offer', {
    call_id: callId,
    target_user_id: targetUserId,
    sdp,
  });
}

export function sendWebRTCAnswer(callId: string, targetUserId: string, sdp: string) {
  sendWsMessage('webrtc_answer', {
    call_id: callId,
    target_user_id: targetUserId,
    sdp,
  });
}

export function sendICECandidate(callId: string, targetUserId: string, candidate: string) {
  sendWsMessage('webrtc_ice_candidate', {
    call_id: callId,
    target_user_id: targetUserId,
    candidate,
  });
}

// Bridge methods — called by existing TG Web A action handlers via callApi()
// These map TG's DH-based call flow to Saturn's simpler REST+WebRTC flow

export function getDhConfig() {
  // Saturn doesn't use Diffie-Hellman for call setup — return a dummy config
  return Promise.resolve({ g: 0, p: new Uint8Array(0), random: new Uint8Array(0) });
}

export function requestPhoneCall() {
  // No-op: Saturn creates calls via REST, not DH exchange
  return Promise.resolve(new Uint8Array(0));
}

export function createPhoneCallState() {
  return Promise.resolve(undefined);
}

export function destroyPhoneCallState() {
  return Promise.resolve(undefined);
}

export function acceptPhoneCall() {
  // No-op: acceptance happens via REST acceptCallApi
  return Promise.resolve(new Uint8Array(0));
}

export function encodePhoneCallData(args: [string]) {
  // Saturn doesn't encrypt signaling — pass through as-is
  return Promise.resolve(args[0]);
}

export function decodePhoneCallData(args: [number[]]) {
  // Saturn signaling arrives as JSON string, not encrypted bytes
  // The data comes as-is from WS, already a string
  return Promise.resolve(undefined);
}

export function sendSignalingData() {
  // No-op: signaling goes through WS directly
  return Promise.resolve(undefined);
}

export async function setCallRating(
  args: { call: ApiPhoneCall; rating: number; comment: string },
): Promise<undefined> {
  const callId = args?.call?.id;
  if (!callId) return undefined;
  try {
    await request('POST', `/calls/${callId}/rating`, {
      rating: args.rating,
      comment: args.comment || '',
    });
  } catch {
    // Rating is non-critical — swallow so we don't surface errors to the user
    // for something they already dismissed.
  }
  return undefined;
}

export function requestCall(args: {
  user: ApiUser;
  gAHash?: Uint8Array;
  isVideo?: boolean;
  chatId?: string;
  chatMemberCount?: number; // For auto-routing: if > 2, use group mode
}): Promise<SaturnCall | undefined> {
  // Concurrent-call dedupe: TG action layer dispatches connectToActivePhoneCall
  // twice (once from the requestCall UI handler, once from PhoneCall.tsx mount
  // useEffect). Both reach this function before the first await resolves, so
  // any async-aware lock is racy. By caching the in-flight Promise, both
  // callers get the same SaturnCall back instead of issuing duplicate POSTs.
  if (pendingRequestCall) return pendingRequestCall;
  pendingRequestCall = doRequestCall(args).finally(() => {
    pendingRequestCall = undefined;
  });
  return pendingRequestCall;
}

async function doRequestCall({
  user, isVideo, chatId: providedChatId, chatMemberCount,
}: {
  user: ApiUser;
  gAHash?: Uint8Array;
  isVideo?: boolean;
  chatId?: string;
  chatMemberCount?: number;
}): Promise<SaturnCall | undefined> {
  // In Saturn, DM chatId !== userId. Callers MUST pass the correct chatId —
  // falling back to user.id silently used to corrupt the backend request and
  // cause "Call failed to start" with no clue why.
  if (!providedChatId) {
    // eslint-disable-next-line no-console
    console.error('[Saturn:calls] requestCall called without chatId — DM chatId is required in Saturn');
    sendApiUpdate({
      '@type': 'updatePhoneCall',
      call: {
        id: '',
        accessHash: '',
        state: 'discarded',
        reason: 'disconnect',
      } as ApiPhoneCall,
    });
    return undefined;
  }
  const chatId = providedChatId;

  // Auto-routing: group calls for > 2 participants, p2p otherwise.
  // This logic lives here (action layer) rather than in the UI button
  // to ensure consistent behavior across all call initiation paths.
  const memberCount = chatMemberCount ?? 2; // default: assume DM (2 = just the two participants)
  const callMode = memberCount > 2 ? 'group' : 'p2p';

  const call = await createCall({
    chatId,
    type: isVideo ? 'video' : 'voice',
    mode: callMode,
    memberIds: [user.id],
  });

  if (!call) return undefined;

  activeCallId = call.id;
  activeCallPeerId = user.id;
  activeCallMode = 'p2p';
  activeCallStartedAt = Date.now();

  // Fetch ICE servers for WebRTC connection
  const iceServers = await fetchICEServers({ callId: call.id });
  const connections = iceServers ? iceServersToConnections(iceServers) : [];

  // Ensure at least a public STUN server is available
  if (!connections.length) {
    connections.push({
      ip: 'stun.l.google.com', ipv6: '', port: 19302,
      username: '', password: '', isStun: true,
    });
  }

  sendApiUpdate({
    '@type': 'updatePhoneCall',
    call: {
      id: call.id,
      accessHash: '',
      state: 'requesting',
      adminId: call.initiator_id,
      participantId: user.id,
      isVideo: call.type === 'video',
      isOutgoing: true,
      connections,
      protocol: DEFAULT_PROTOCOL,
      isP2pAllowed: true,
    } as ApiPhoneCall,
  });

  return call;
}

export async function acceptCall({
  call,
}: {
  call: ApiPhoneCall;
  gB?: Uint8Array;
}) {
  const result = await acceptCallApi({ callId: call.id });
  if (!result) return undefined;

  activeCallId = call.id;
  activeCallPeerId = call.adminId;
  activeCallStartedAt = Date.now();

  // Fetch ICE servers for WebRTC connection
  const iceServers = await fetchICEServers({ callId: call.id });
  const connections = iceServers ? iceServersToConnections(iceServers) : [];

  if (!connections.length) {
    connections.push({
      ip: 'stun.l.google.com', ipv6: '', port: 19302,
      username: '', password: '', isStun: true,
    });
  }

  sendApiUpdate({
    '@type': 'updatePhoneCall',
    call: {
      ...call,
      state: 'active',
      connections,
      protocol: DEFAULT_PROTOCOL,
      isP2pAllowed: true,
    },
  });

  return result;
}

export async function discardCall({
  call, isPageUnload,
}: {
  call: ApiPhoneCall;
  isPageUnload?: boolean;
}) {
  if (activeCallId) {
    await endCallApi({ callId: activeCallId });
  }

  // Only prompt for rating when the call actually connected for more than
  // the minimum threshold — missed/instant-declined calls should just close.
  const durationMs = activeCallStartedAt ? Date.now() - activeCallStartedAt : 0;
  const needRating = !isPageUnload && durationMs >= RATING_MIN_DURATION_MS;

  activeCallId = undefined;
  activeCallPeerId = undefined;
  activeCallStartedAt = undefined;

  if (!isPageUnload) {
    sendApiUpdate({
      '@type': 'updatePhoneCall',
      call: {
        ...call,
        state: 'discarded',
        reason: 'hangup',
        needRating,
      },
    });
  }
}

export function receivedCall() {
  return Promise.resolve(undefined);
}

export function confirmCall() {
  return Promise.resolve(undefined);
}

// ─────────────────────────────────────────────────────────────────────────────
// Group calls (Phase 6 Stage 3 — Pion SFU)
//
// The TG Web A action layer (createGroupCall / joinGroupCall / etc.) drives
// these methods. We implement the Saturn-flavoured versions: REST roundtrip
// to the calls service for participant bookkeeping, plus a single Pion SFU
// WebSocket via lib/secret-sauce/sfu.ts for media.
// ─────────────────────────────────────────────────────────────────────────────

const remoteSfuStreams = new Map<string, MediaStream>();
// Maps trackId → userId so removal callbacks can carry the correct userId.
const remoteSfuTrackOwners = new Map<string, string>();

export function getSfuRemoteStreams(): ReadonlyMap<string, MediaStream> {
  return remoteSfuStreams;
}

function buildSfuWsBase(): string {
  const baseUrl = getBaseUrl();
  if (!baseUrl) return '';
  return baseUrl.replace(/^http/, 'ws');
}

/**
 * Saturn createGroupCall — POST /calls with mode='group'. Returns the call
 * row (including sfu_ws_url) so the action layer can hand it off to
 * connectToActiveGroupCall via callApi('joinGroupCall').
 */
export async function createGroupCall({ chatId, type, memberIds }: {
  chatId: string;
  type?: 'voice' | 'video';
  memberIds?: string[];
}): Promise<SaturnCall | undefined> {
  return createCall({
    chatId,
    type: type || 'voice',
    mode: 'group',
    memberIds,
  });
}

/**
 * Saturn joinGroupCall — opens the SFU WebSocket and returns once the first
 * SDP exchange is complete. Called by ui/calls.ts action handlers after the
 * group call panel has been opened. The signature accepts a loose object so
 * existing TG callers (which pass `{call, params, inviteHash}`) keep working.
 */
export async function joinGroupCall(args?: { call?: { id?: string; type?: string }; isVideo?: boolean }): Promise<SaturnCall | undefined> {
  const callId = args?.call?.id;
  if (!callId) {
    // eslint-disable-next-line no-console
    console.error('[Saturn:calls] joinGroupCall called without call.id');
    return undefined;
  }

  // Mark this user as a participant in the DB. The REST hop fires
  // NATS call_participant_joined so other peers see the new tile immediately.
  // The actual SFU WebSocket connection is managed by useSfuStreamManager in GroupCall.tsx.
  try {
    await request<{ status: string }>('POST', `/calls/${callId}/join`);
  } catch (e) {
    // eslint-disable-next-line no-console
    console.error('[Saturn:calls] join REST failed', e);
    return undefined;
  }

  activeCallId = callId;
  activeCallMode = 'group';
  return undefined;
}

/**
 * Saturn leaveGroupCall — closes the SFU session and tells the calls
 * service to drop the participant. Backend auto-ends the call if this was
 * the last peer.
 */
export async function leaveGroupCall(args?: { call?: { id?: string }; isPageUnload?: boolean }): Promise<undefined> {
  const callId = args?.call?.id || activeCallId;
  leaveSfuCall();
  remoteSfuStreams.clear();
  remoteSfuTrackOwners.clear();
  if (callId) {
    try {
      await request('DELETE', `/calls/${callId}/leave`);
    } catch {
      // ignore — backend cleanup will fire on WS drop too
    }
  }
  if (activeCallId === callId) {
    activeCallId = undefined;
    activeCallMode = undefined;
  }
  return undefined;
}

export function discardGroupCall(args?: { call?: { id?: string } }) {
  return leaveGroupCall(args);
}

/**
 * GET /calls/:id — returns the raw Saturn call row (used internally; the
 * TG-style action layer hits a different shape so we keep getGroupCall as a
 * no-op for compatibility with the legacy fetchGroupCall path).
 */
export async function fetchSaturnCall(callId: string): Promise<SaturnCall | undefined> {
  return fetchCall({ callId });
}

// Legacy TG-style API surface — Saturn does not use these in the SFU group
// call flow (joinGroupCall fetches everything it needs itself), but the TG
// Web A action layer still imports them. Returning undefined keeps the
// existing fetchGroupCall path inert without breaking the type contract.
export function getGroupCall() { return Promise.resolve(undefined); }
export async function fetchGroupCallParticipants(args?: { call?: { id?: string } }): Promise<SaturnCallParticipant[] | undefined> {
  const callId = args?.call?.id;
  if (!callId) return undefined;
  const call = await fetchCall({ callId });
  return call?.participants;
}

// The remaining methods are not used by the Saturn group call path but the
// TG Web A action layer still imports them. They stay as no-ops to keep the
// type surface compatible until those code paths are removed.
export function editGroupCallParticipant() { return Promise.resolve(undefined); }
export function editGroupCallTitle() { return Promise.resolve(undefined); }
export function exportGroupCallInvite() { return Promise.resolve(undefined); }
export function joinGroupCallPresentation() { return Promise.resolve(undefined); }
export function leaveGroupCallPresentation() { return Promise.resolve(undefined); }
export function toggleGroupCallStartSubscription() { return Promise.resolve(undefined); }
