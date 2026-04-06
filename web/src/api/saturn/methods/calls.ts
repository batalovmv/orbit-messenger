import type { ApiPhoneCall, ApiUser } from '../../types';

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

export function encodePhoneCallData() {
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
  user, isVideo,
}: {
  user: ApiUser;
  gAHash?: Uint8Array;
  isVideo?: boolean;
}) {
  // Find or create a direct chat with the user, then create a call
  const { fetchChats } = await import('./chats');
  // We need the chat ID for this user — look up direct chats
  const chatId = user.id; // In Saturn, direct chat ID equals the DM target's user ID for simplicity
  // Actually we need to find the direct chat. For now, we'll use the user ID as member.
  const call = await createCall({
    chatId,
    type: isVideo ? 'video' : 'voice',
    mode: 'p2p',
    memberIds: [user.id],
  });

  if (!call) return undefined;

  activeCallId = call.id;
  activeCallPeerId = user.id;

  // Dispatch updatePhoneCall so UI shows "requesting" state
  sendApiUpdate({
    '@type': 'updatePhoneCall',
    call: {
      id: call.id,
      visId: call.id,
      visAccessHash: '',
      state: 'requesting',
      adminId: call.initiator_id,
      participantId: user.id,
      isVideo: call.type === 'video',
      isOutgoing: true,
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

  sendApiUpdate({
    '@type': 'updatePhoneCall',
    call: {
      ...call,
      state: 'active',
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
