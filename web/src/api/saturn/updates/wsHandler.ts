// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import type { ApiGroupCall, ApiMessage } from '../../types';
import type { SaturnChat, SaturnMessage, SaturnMessageEntity, SaturnWsMessage } from '../types';

import { buildApiChat } from '../apiBuilders/chats';
import {
  buildApiMessage, buildApiPoll, getMessageSeqNum, parseSaturnReplyMarkup,
} from '../apiBuilders/messages';
import { setWsMessageHandler } from '../client';
import { getActiveCallMode, setActiveCallId, setActiveCallMode, setActiveCallPeerId } from '../methods/calls';
import { sendApiUpdate } from './apiUpdateEmitter';

let currentUserId: string | undefined;

// Track UUIDs of messages we sent locally (pending confirmation).
// When the server broadcasts our own message back via WS, we skip it
// because `updateMessageSendSucceeded` already handled the swap.
const pendingSendUuids = new Set<string>();
const PENDING_UUID_TTL_MS = 30_000;

// Buffer for own messages that arrive via WS before the HTTP response.
// We hold them briefly and recheck after a short delay.
const deferredOwnMessages = new Map<string, SaturnMessage>();
const DEFERRED_CHECK_MS = 500;

export function trackPendingSend(uuid: string) {
  pendingSendUuids.add(uuid);
  // If this UUID was already received via WS while we waited, drop it now
  if (deferredOwnMessages.has(uuid)) {
    deferredOwnMessages.delete(uuid);
  }
  setTimeout(() => pendingSendUuids.delete(uuid), PENDING_UUID_TTL_MS);
}

export function initWsHandler() {
  setWsMessageHandler(handleWsMessage);
}

export function setWsCurrentUserId(userId: string) {
  currentUserId = userId;
}

async function handleWsMessage(msg: SaturnWsMessage) {
  switch (msg.type) {
    case 'new_message':
      handleNewMessage(msg.data as unknown as SaturnMessage);
      break;
    case 'message_updated':
      handleMessageUpdated(msg.data as unknown as SaturnMessage);
      break;
    case 'message_deleted':
      handleMessageDeleted(msg.data);
      break;
    case 'messages_read':
      handleMessagesRead(msg.data);
      break;
    case 'read_sync':
      handleReadSync(msg.data);
      break;
    case 'typing':
      handleTyping(msg.data);
      break;
    case 'stop_typing':
      handleStopTyping(msg.data);
      break;
    case 'message_pinned':
    case 'message_unpinned':
      handleMessagePinChanged(msg.data);
      break;
    case 'reaction_added':
    case 'reaction_removed':
      await handleReactionChanged(msg.data);
      break;
    case 'poll_vote':
      handlePollUpdated(msg.data);
      break;
    case 'poll_closed':
      handlePollUpdated(msg.data, true);
      break;
    case 'user_status':
      handleUserStatus(msg.data);
      break;
    case 'chat_created': {
      const chatData = msg.data as unknown as SaturnChat;
      sendApiUpdate({
        '@type': 'updateChat',
        id: chatData.id,
        chat: buildApiChat(chatData),
      });
      break;
    }
    case 'chat_updated': {
      const payload = msg.data;
      const { fetchFullChat } = await import('../methods/chats');
      fetchFullChat({ id: (payload.chat_id || payload.id) as string });
      break;
    }
    case 'chat_deleted': {
      const payload = msg.data;
      const chatId = payload.chat_id as string;
      sendApiUpdate({
        '@type': 'updateChat',
        id: chatId,
        chat: {
          isRestricted: true,
          isNotJoined: true,
        } as any,
      });
      sendApiUpdate({
        '@type': 'updateChatLeave',
        id: chatId,
      } as any);
      break;
    }
    case 'chat_member_added': {
      const payload = msg.data;
      const { fetchFullChat } = await import('../methods/chats');
      fetchFullChat({ id: payload.chat_id as string });
      break;
    }
    case 'chat_member_removed': {
      const payload = msg.data;
      const chatId = payload.chat_id as string;
      if (payload.user_id === currentUserId) {
        sendApiUpdate({
          '@type': 'updateChat',
          id: chatId,
          chat: {
            isRestricted: true,
            isNotJoined: true,
            isForbidden: true,
          } as any,
        });
        sendApiUpdate({
          '@type': 'updateChatLeave',
          id: chatId,
        } as any);
      } else {
        const { fetchFullChat } = await import('../methods/chats');
        fetchFullChat({ id: chatId });
      }
      break;
    }
    case 'chat_member_updated': {
      const payload = msg.data;
      const { fetchFullChat } = await import('../methods/chats');
      fetchFullChat({ id: payload.chat_id as string });
      break;
    }
    case 'mention': {
      const payload = msg.data;
      const { fetchFullChat } = await import('../methods/chats');
      fetchFullChat({ id: payload.chat_id as string });
      break;
    }
    case 'media_ready': {
      handleMediaReady(msg.data);
      break;
    }
    case 'media_upload_progress': {
      break;
    }
    // Phase 6: Call events
    case 'call_incoming':
      handleCallIncoming(msg.data);
      break;
    case 'call_accepted':
      handleCallAccepted(msg.data);
      break;
    case 'call_declined':
      handleCallDeclined(msg.data);
      break;
    case 'call_ended':
      handleCallEnded(msg.data);
      break;
    case 'call_participant_joined':
      handleCallParticipantJoined(msg.data);
      break;
    case 'call_participant_left':
      handleCallParticipantLeft(msg.data);
      break;
    case 'call_muted':
    case 'call_unmuted':
      handleCallMuteChanged(msg.data);
      break;
    case 'screen_share_started':
      handleScreenShareChanged(msg.data, true);
      break;
    case 'screen_share_stopped':
      handleScreenShareChanged(msg.data, false);
      break;
    case 'webrtc_offer':
    case 'webrtc_answer':
    case 'webrtc_ice_candidate':
      handleWebRTCSignaling(msg.type, msg.data);
      break;
    default:
      break;
  }
}

function handleNewMessage(data: SaturnMessage) {
  // Dedup: skip our own messages that we already handled optimistically
  if (data.sender_id === currentUserId && pendingSendUuids.has(data.id)) {
    pendingSendUuids.delete(data.id);
    return;
  }

  // Race condition guard: WS echo may arrive before the HTTP response
  // registers the UUID via trackPendingSend. Defer own messages briefly.
  if (data.sender_id === currentUserId && !pendingSendUuids.has(data.id)) {
    deferredOwnMessages.set(data.id, data);
    setTimeout(() => {
      // If trackPendingSend was called in the meantime, it already removed us
      if (!deferredOwnMessages.has(data.id)) return;
      deferredOwnMessages.delete(data.id);
      // Still not in pending set — this is a genuine message (e.g. from another tab)
      if (pendingSendUuids.has(data.id)) {
        pendingSendUuids.delete(data.id);
        return;
      }
      dispatchNewMessage(data);
    }, DEFERRED_CHECK_MS);
    return;
  }

  dispatchNewMessage(data);
}

function dispatchNewMessage(data: SaturnMessage) {
  const apiMsg = buildApiMessage(data);
  if (currentUserId) {
    apiMsg.isOutgoing = data.sender_id === currentUserId;
  }

  // ForceReply lives on the update, not the message — the reducer uses it to
  // open a reply prompt in the composer for the recipient (and only them when
  // selective is set). We compute it here from reply_markup since buildApiMessage
  // doesn't have the outgoing/userId context.
  const parsedMarkup = parseSaturnReplyMarkup(data.reply_markup);
  const shouldForceReply = !apiMsg.isOutgoing && parsedMarkup?.force_reply
    ? !parsedMarkup.selective || apiMsg.replyInfo !== undefined
    : false;

  sendApiUpdate({
    '@type': 'newMessage',
    chatId: data.chat_id,
    id: apiMsg.id,
    message: apiMsg,
    poll: buildApiPoll(data.poll),
    shouldForceReply: shouldForceReply || undefined,
  });
}

function handleMessageUpdated(data: SaturnMessage) {
  const apiMsg = buildApiMessage(data);
  if (currentUserId) {
    apiMsg.isOutgoing = data.sender_id === currentUserId;
  }

  sendApiUpdate({
    '@type': 'updateMessage',
    chatId: data.chat_id,
    id: apiMsg.id,
    isFull: true,
    message: apiMsg,
    poll: buildApiPoll(data.poll),
  });
}

function handleMessageDeleted(data: Record<string, unknown>) {
  const chatId = data.chat_id as string;
  const seqNum = data.sequence_number as number | undefined;

  if (!seqNum || !chatId) return;

  sendApiUpdate({
    '@type': 'deleteMessages',
    ids: [seqNum],
    chatId,
  });
}

function handleMessagePinChanged(data: Record<string, unknown>) {
  const chatId = data.chat_id as string;
  const seqNum = data.sequence_number as number | undefined;
  const isPinned = data.is_pinned as boolean;

  if (!seqNum || !chatId) return;

  sendApiUpdate({
    '@type': 'updateMessage',
    chatId,
    id: seqNum,
    isFull: false,
    message: { isPinned },
  });

  // Also update pinned message IDs list
  sendApiUpdate({
    '@type': 'updatePinnedIds',
    chatId,
    isPinned,
    messageIds: [seqNum],
  });
}

async function handleMessagesRead(data: Record<string, unknown>) {
  const chatId = data.chat_id as string;
  const userId = data.user_id as string;
  const lastReadUuid = data.last_read_message_id as string | undefined;

  if (!lastReadUuid) return;

  const lastReadSeqNum = getMessageSeqNum(lastReadUuid);
  if (!lastReadSeqNum) {
    // Fallback: UUID not in local map (e.g. after reconnect/reload) — refresh chat to get current read state
    const { fetchFullChat } = await import('../methods/chats');
    fetchFullChat({ id: chatId });
    return;
  }

  // If someone else read our messages → update outbox (shows ✓✓)
  // If we read someone's messages → update inbox
  if (userId !== currentUserId) {
    sendApiUpdate({
      '@type': 'updateChat',
      id: chatId,
      chat: {},
      readState: {
        lastReadOutboxMessageId: lastReadSeqNum,
      },
      noTopChatsRequest: true,
    });
  } else {
    sendApiUpdate({
      '@type': 'updateChat',
      id: chatId,
      chat: {},
      readState: {
        lastReadInboxMessageId: lastReadSeqNum,
        unreadCount: 0,
      },
      noTopChatsRequest: true,
    });
  }
}

// handleReadSync processes orbit.user.<userID>.read_sync — a self-only event
// emitted by messaging after MarkRead so other tabs/devices of the same user
// can prune their notifications and refresh chat read state without an extra
// fetch. Gateway already excluded our originating connection by SessionID
// before fanning this out, so receiving it here means the action came from a
// different device.
function handleReadSync(data: Record<string, unknown>) {
  const chatId = data.chat_id as string;
  const lastReadSeqNum = data.last_read_seq_num as number | undefined;
  const unreadCount = data.unread_count as number | undefined;

  if (!chatId || typeof lastReadSeqNum !== 'number') return;

  // Update the chat's inbox read state so the unread badge clears immediately
  // on this device. Mirrors the inbox branch of handleMessagesRead, but the
  // count comes from the backend (post-MarkRead snapshot) instead of being
  // assumed zero.
  sendApiUpdate({
    '@type': 'updateChat',
    id: chatId,
    chat: {},
    readState: {
      lastReadInboxMessageId: lastReadSeqNum,
      unreadCount: typeof unreadCount === 'number' ? unreadCount : 0,
    },
    noTopChatsRequest: true,
  });

  // Tell the service worker to close any push notifications for this chat up
  // to and including the just-read message. The SW handler is keyed off
  // sequence_number, so we must pass last_read_seq_num — the UUID-shaped
  // last_read_message_id from the API would not match notification.data.messageId.
  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.controller?.postMessage({
      type: 'closeMessageNotifications',
      payload: { chatId, lastReadInboxMessageId: lastReadSeqNum },
    });
  }
}

function handleTyping(data: Record<string, unknown>) {
  const chatId = data.chat_id as string;
  const userId = data.user_id as string | undefined;

  // Don't show our own typing status
  if (userId === currentUserId) return;

  sendApiUpdate({
    '@type': 'updateChatTypingStatus',
    id: chatId,
    typingStatus: {
      userId,
      action: 'typing',
      timestamp: Date.now(),
    },
  });
}

function handleStopTyping(data: Record<string, unknown>) {
  const chatId = data.chat_id as string;
  const userId = data.user_id as string | undefined;

  if (userId === currentUserId) return;

  sendApiUpdate({
    '@type': 'updateChatTypingStatus',
    id: chatId,
    typingStatus: undefined,
    userId,
  });
}

function handleUserStatus(data: Record<string, unknown>) {
  const userId = data.user_id as string;
  const status = data.status as string;
  const lastSeen = data.last_seen as string | undefined;
  const customStatus = data.custom_status as string | undefined;
  const customStatusEmoji = data.custom_status_emoji as string | undefined;

  sendApiUpdate({
    '@type': 'updateUserStatus',
    userId,
    status: {
      type: status === 'online' ? 'userStatusOnline'
        : status === 'recently' ? 'userStatusRecently'
          : 'userStatusOffline',
      wasOnline: lastSeen ? Math.floor(new Date(lastSeen).getTime() / 1000) : undefined,
      expires: status === 'online' ? Math.floor(Date.now() / 1000) + 300 : undefined,
    },
  });

  // Propagate custom status changes to user object in global state
  if (customStatus !== undefined || customStatusEmoji !== undefined) {
    sendApiUpdate({
      '@type': 'updateUser',
      id: userId,
      user: {
        customStatus: customStatus || undefined,
        customStatusEmoji: customStatusEmoji || undefined,
      },
    });
  }
}

async function handleReactionChanged(data: Record<string, unknown>) {
  const chatId = data.chat_id as string | undefined;
  const seqNum = data.sequence_number as number | undefined;

  if (!chatId || !seqNum) {
    return;
  }

  const { fetchMessageReactions } = await import('../methods/reactions');
  await fetchMessageReactions({
    ids: [seqNum],
    chat: { id: chatId } as any,
  });
}

function handleMediaReady(data: Record<string, unknown>) {
  const mediaId = data.media_id as string | undefined;
  if (!mediaId) return;

  sendApiUpdate({
    '@type': 'updateMessageMediaReady',
    mediaId,
    width: typeof data.width === 'number' ? data.width : undefined,
    height: typeof data.height === 'number' ? data.height : undefined,
    duration: typeof data.duration_seconds === 'number' ? data.duration_seconds : undefined,
    hasThumbnail: Boolean(data.has_thumbnail),
  } as any);
}

function handlePollUpdated(data: Record<string, unknown>, isClosed = false) {
  const poll = data.poll as Record<string, unknown> | undefined;
  const pollId = (poll?.id || data.poll_id) as string | undefined;
  const peerId = data.user_id as string | undefined;
  if (!pollId) {
    return;
  }

  sendApiUpdate({
      '@type': 'updateMessagePoll',
      pollId,
      pollUpdate: {
        id: pollId,
        mediaType: 'poll',
        summary: isClosed ? { closed: true } : undefined,
        ...buildApiPollFromWs(poll, isClosed, peerId === currentUserId, peerId),
      } as any,
  });
}

function buildApiPollFromWs(
  poll?: Record<string, unknown>,
  isClosed = false,
  shouldKeepChoiceState = false,
  voterId?: string,
) {
  if (!poll) {
    return isClosed ? { summary: { closed: true } } : undefined;
  }

  const options = Array.isArray(poll.options)
    ? poll.options.filter((option): option is Record<string, unknown> => Boolean(option))
    : [];
  const createdAt = typeof poll.created_at === 'string' ? poll.created_at : undefined;
  const closeAt = typeof poll.close_at === 'string' ? poll.close_at : undefined;
  const createdAtTs = createdAt ? Math.floor(new Date(createdAt).getTime() / 1000) : undefined;
  const closeAtTs = closeAt ? Math.floor(new Date(closeAt).getTime() / 1000) : undefined;
  const solutionEntities = Array.isArray(poll.solution_entities)
    ? poll.solution_entities
      .filter((entity): entity is SaturnMessageEntity => Boolean(entity))
      .map(buildWsPollEntity)
    : undefined;

  return {
    id: poll.id as string,
    mediaType: 'poll',
    summary: {
      closed: Boolean(poll.is_closed) || isClosed || undefined,
      isPublic: poll.is_anonymous === false || undefined,
      multipleChoice: Boolean(poll.is_multiple) || undefined,
      quiz: Boolean(poll.is_quiz) || undefined,
      question: {
        text: (poll.question as string) || '',
        entities: [],
      },
      answers: options
        .slice()
        .sort((left, right) => Number(left.position || 0) - Number(right.position || 0))
        .map((option) => ({
          option: option.id as string,
          text: {
            text: (option.text as string) || '',
            entities: [],
          },
        })),
      closeDate: closeAtTs,
      closePeriod: createdAtTs && closeAtTs && closeAtTs > createdAtTs ? closeAtTs - createdAtTs : undefined,
    },
    results: {
      results: options
        .slice()
        .sort((left, right) => Number(left.position || 0) - Number(right.position || 0))
        .map((option) => ({
          option: option.id as string,
          votersCount: Number(option.voters || 0),
          isChosen: shouldKeepChoiceState && Boolean(option.is_chosen) || undefined,
          isCorrect: shouldKeepChoiceState && Boolean(option.is_correct) || undefined,
        })),
      totalVoters: Number(poll.total_voters || 0),
      recentVoterIds: !poll.is_anonymous && voterId ? [voterId] : undefined,
      solution: typeof poll.solution === 'string' ? poll.solution : undefined,
      solutionEntities,
    },
  };
}

// Phase 6: Call event handlers

function handleCallIncoming(data: Record<string, unknown>) {
  const call = data as Record<string, unknown>;
  const callId = (call.id || data.call_id) as string;
  const initiatorId = (call.initiator_id) as string;
  const isVideo = call.type === 'video';
  const mode = (call.mode as string) || 'p2p';
  const chatId = call.chat_id as string | undefined;

  if (!callId || !initiatorId) return;

  // Don't show incoming call if we initiated it
  if (initiatorId === currentUserId) return;

  setActiveCallId(callId);
  setActiveCallMode(mode === 'group' ? 'group' : 'p2p');

  // Vibrate on incoming — works on mobile PWA; desktop browsers may ignore it
  // silently if the tab isn't focused. Wrapped in try to guard against browsers
  // that reject the call (some Safari versions throw rather than no-op).
  if (typeof navigator !== 'undefined' && 'vibrate' in navigator) {
    try {
      navigator.vibrate([300, 200, 300, 200, 300]);
    } catch {
      // ignore — vibration is best-effort
    }
  }

  if (mode === 'group') {
    // Register the group call in global state so GroupCallTopPane shows a "Join" banner.
    sendApiUpdate({
      '@type': 'updateGroupCall',
      call: {
        id: callId,
        accessHash: '',
        participantsCount: 1,
        participants: {} as ApiGroupCall['participants'],
        connectionState: 'disconnected',
        version: 0,
        chatId: chatId || '',
        isLoaded: true,
      },
    });
    if (chatId) {
      sendApiUpdate({
        '@type': 'updateGroupCallChatId',
        call: { id: callId, accessHash: '' },
        chatId,
      });
    }
    return;
  }

  // p2p incoming call path
  setActiveCallPeerId(initiatorId);
  sendApiUpdate({
    '@type': 'updatePhoneCall',
    call: {
      id: callId,
      accessHash: '',
      state: 'requested',
      adminId: initiatorId,
      participantId: currentUserId || '',
      isVideo,
      isP2pAllowed: true,
    },
  });
}

function handleCallAccepted(data: Record<string, unknown>) {
  const callId = data.call_id as string;
  const acceptorId = data.user_id as string;

  if (!callId) return;

  // Skip echo for the callee — already handled by acceptCall() Saturn method
  if (acceptorId === currentUserId) return;

  // For the caller: set peer to acceptor
  setActiveCallPeerId(acceptorId);

  // Only set state to active; do NOT include adminId/participantId
  // to avoid overwriting correct values already in global.phoneCall
  sendApiUpdate({
    '@type': 'updatePhoneCall',
    call: {
      id: callId,
      accessHash: '',
      state: 'active',
    },
  });
}

function handleCallDeclined(data: Record<string, unknown>) {
  const callId = data.call_id as string;
  const userId = data.user_id as string;

  // eslint-disable-next-line no-console
  console.info('[Calls] call_declined', { callId, userId, self: currentUserId });

  if (!callId) return;

  // Echo guard — if we are the one who declined, the local acceptCall/declineCall
  // path already marked the call as discarded. Applying the event twice can race
  // with hangUp() and leave phoneCall cleared mid-transition.
  if (userId === currentUserId) return;

  sendApiUpdate({
    '@type': 'updatePhoneCall',
    call: {
      id: callId,
      accessHash: '',
      state: 'discarded',
      reason: 'busy',
    },
  });
}

function handleCallEnded(data: Record<string, unknown>) {
  const callId = data.call_id as string;
  const durationSeconds = data.duration_seconds as number | undefined;
  const rawReason = (data.reason || 'hangup') as string;
  const reason = (['missed', 'disconnect', 'hangup', 'busy'].includes(rawReason)
    ? rawReason
    : 'hangup') as 'missed' | 'disconnect' | 'hangup' | 'busy';

  if (!callId) return;

  // For group calls, tear down via the group call state machine.
  // getActiveCallMode() reflects what handleCallIncoming (or joinGroupCall) set.
  const callMode = (data.mode as string) || getActiveCallMode();
  if (callMode === 'group') {
    setActiveCallMode(undefined);
    setActiveCallId(undefined);
    sendApiUpdate({
      '@type': 'updateGroupCall',
      call: {
        id: callId,
        accessHash: '',
        participantsCount: 0,
        participants: {} as ApiGroupCall['participants'],
        connectionState: 'discarded',
        version: 0,
      },
    });
    return;
  }

  sendApiUpdate({
    '@type': 'updatePhoneCall',
    call: {
      id: callId,
      accessHash: '',
      state: 'discarded',
      reason,
      duration: durationSeconds,
    },
  });
}

function handleCallMuteChanged(data: Record<string, unknown>) {
  const callId = data.call_id as string;
  const userId = data.user_id as string;
  const isMuted = Boolean(data.muted);

  if (!callId || !userId) return;

  // Echo guard — our own mute is handled locally by the toggleStreamP2p path.
  if (userId === currentUserId) return;

  sendApiUpdate({
    '@type': 'updatePhoneCallPeerState',
    peerIsMuted: isMuted,
  } as any);
}

// Group call (Phase 6 Stage 3) — peer entered the SFU room.
// We do not need to mutate phoneCall state here; the SFU client itself
// installs new RTCPeerConnection.ontrack callbacks that surface remote
// streams via the apiUpdateEmitter. The event is mostly used to refresh
// the participant grid in the group call panel via a generic update.
function handleCallParticipantJoined(data: Record<string, unknown>) {
  const callId = data.call_id as string;
  const userId = data.user_id as string;
  if (!callId || !userId) return;
  if (userId === currentUserId) return;
  sendApiUpdate({
    '@type': 'updateGroupCallParticipants',
    groupCallId: callId,
    // GroupCallParticipant.id is the user ID string
    participants: [{ id: userId, hasJustJoined: true, source: 0, date: new Date() }],
    nextOffset: undefined,
  } as any);
}

// Group call (Phase 6 Stage 3) — peer left the SFU room (or auto-ended).
function handleCallParticipantLeft(data: Record<string, unknown>) {
  const callId = data.call_id as string;
  const userId = data.user_id as string;
  if (!callId || !userId) return;
  if (userId === currentUserId) return;
  sendApiUpdate({
    '@type': 'updateGroupCallParticipants',
    groupCallId: callId,
    participants: [{ id: userId, isLeft: true, source: 0, date: new Date() }],
    nextOffset: undefined,
  } as any);
}

function handleScreenShareChanged(data: Record<string, unknown>, isActive: boolean) {
  const callId = data.call_id as string;
  const userId = data.user_id as string;

  if (!callId || !userId) return;

  // Only react to the remote peer — the local user already knows their own state.
  if (userId === currentUserId) return;

  sendApiUpdate({
    '@type': 'updatePhoneCallPeerState',
    peerIsScreenSharing: isActive,
  } as any);
}

function handleWebRTCSignaling(type: string, data: Record<string, unknown>) {
  const callId = data.call_id as string;
  const senderId = data.sender_id as string;

  if (!callId || !senderId) return;

  sendApiUpdate({
    '@type': 'updateWebRTCSignaling',
    signalingType: type,
    callId,
    senderId,
    sdp: data.sdp as string | undefined,
    candidate: data.candidate as string | undefined,
  });
}

function buildWsPollEntity(entity: SaturnMessageEntity) {
  const base = {
    type: entity.type as any,
    offset: entity.offset,
    length: entity.length,
  };

  if (entity.url) {
    return { ...base, type: 'MessageEntityTextUrl', url: entity.url };
  }

  if (entity.language) {
    return { ...base, type: 'MessageEntityPre', language: entity.language };
  }

  if (entity.user_id) {
    return { ...base, type: 'MessageEntityMentionName', userId: entity.user_id };
  }

  return base;
}
