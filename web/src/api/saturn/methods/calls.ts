import type { ApiPhoneCall, ApiUser } from '../../types';
import type { ApiPhoneCallConnection, ApiCallProtocol } from '../../../lib/secret-sauce';

import { sendApiUpdate } from '../updates/apiUpdateEmitter';
import { request, sendWsMessage } from '../client';

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

export function setCallRating() {
  // Not yet implemented
  return Promise.resolve(undefined);
}

export async function requestCall({
  user, isVideo, chatId: providedChatId,
}: {
  user: ApiUser;
  gAHash?: Uint8Array;
  isVideo?: boolean;
  chatId?: string;
}) {
  // In Saturn, DM chatId !== userId. Use provided chatId, fall back to userId for TG compat.
  const chatId = providedChatId || user.id;
  const call = await createCall({
    chatId,
    type: isVideo ? 'video' : 'voice',
    mode: 'p2p',
    memberIds: [user.id],
  });

  if (!call) return undefined;

  activeCallId = call.id;
  activeCallPeerId = user.id;

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

  activeCallId = undefined;
  activeCallPeerId = undefined;

  if (!isPageUnload) {
    sendApiUpdate({
      '@type': 'updatePhoneCall',
      call: {
        ...call,
        state: 'discarded',
        reason: 'hangup',
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

// Group call stubs (Phase 6 future: full group call implementation)
export function createGroupCall() { return Promise.resolve(undefined); }
export function joinGroupCall() { return Promise.resolve(undefined); }
export function leaveGroupCall() { return Promise.resolve(undefined); }
export function discardGroupCall() { return Promise.resolve(undefined); }
export function getGroupCall() { return Promise.resolve(undefined); }
export function fetchGroupCallParticipants() { return Promise.resolve(undefined); }
export function editGroupCallParticipant() { return Promise.resolve(undefined); }
export function editGroupCallTitle() { return Promise.resolve(undefined); }
export function exportGroupCallInvite() { return Promise.resolve(undefined); }
export function joinGroupCallPresentation() { return Promise.resolve(undefined); }
export function leaveGroupCallPresentation() { return Promise.resolve(undefined); }
export function toggleGroupCallStartSubscription() { return Promise.resolve(undefined); }
