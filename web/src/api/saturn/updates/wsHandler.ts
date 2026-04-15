import type { ApiGroupCall, ApiMessage } from '../../types';
import type { SaturnChat, SaturnMessage, SaturnMessageEntity, SaturnWsMessage } from '../types';

import { buildApiChat } from '../apiBuilders/chats';
import { buildApiMessage, buildApiPoll, getMessageSeqNum } from '../apiBuilders/messages';
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

  sendApiUpdate({
    '@type': 'newMessage',
    chatId: data.chat_id,
    id: apiMsg.id,
    message: apiMsg,
    poll: buildApiPoll(data.poll),
  });

  if (data.type === 'encrypted' && data.encrypted_content && data.sender_id) {
    void decryptAndPatchEncryptedMessage(data);
  }
}

export async function decryptAndPatchEncryptedMessage(data: SaturnMessage) {
  if (!data.encrypted_content || !data.sender_id) return;
  try {
    const { decodeEncryptedContentField, decryptIncomingEnvelopePayload } = await import('../methods/encryptedMessages');
    const envelope = decodeEncryptedContentField(data.encrypted_content);
    const decrypted = await decryptIncomingEnvelopePayload({
      senderUserId: data.sender_id,
      envelope,
    });

    if (!decrypted) {
      sendApiUpdate({
        '@type': 'updateMessage',
        chatId: data.chat_id,
        id: data.sequence_number,
        isFull: false,
        message: {
          content: {
            text: { text: '🔒 [зашифровано: не для этого устройства]', entities: [] },
          },
        } as Partial<ApiMessage>,
      });
      return;
    }

    // Phase 7.1: register per-media keys so subsequent downloadMedia
    // calls can transparently decrypt the ciphertext blob served by the
    // media service.
    if (decrypted.media.length > 0) {
      const keyStore = await import('../../../lib/crypto/media-key-store');
      for (const m of decrypted.media) {
        keyStore.registerEncryptedMedia(m.id, {
          key: m.key,
          nonce: m.nonce,
          mime: m.mime,
          filename: m.filename,
          size: m.size,
          width: m.width,
          height: m.height,
          duration: m.duration,
        });
      }
    }

    // Patch the message content in global state: always overwrite the
    // placeholder text; when the payload carries media, enrich the
    // existing ApiPhoto/ApiVideo/ApiDocument objects with the real
    // dimensions/mime/filename that the server never saw.
    const messagePatch: Partial<ApiMessage> = {
      content: buildEncryptedContentPatch(decrypted),
    };

    sendApiUpdate({
      '@type': 'updateMessage',
      chatId: data.chat_id,
      id: data.sequence_number,
      isFull: false,
      message: messagePatch,
    });

    // Populate the client-side search index so E2E messages become
    // searchable locally (server-side Meilisearch skips E2E traffic).
    if (decrypted.text) {
      try {
        const { addToIndex } = await import('../../../lib/search/client-index');
        await addToIndex(data.chat_id, data.sequence_number, decrypted.text);
      } catch {
        // Non-fatal.
      }
    }
  } catch (err) {
    // eslint-disable-next-line no-console
    console.warn('[crypto] failed to decrypt incoming E2E message', err);
    sendApiUpdate({
      '@type': 'updateMessage',
      chatId: data.chat_id,
      id: data.sequence_number,
      isFull: false,
      message: {
        content: {
          text: { text: '🔒 [не удалось расшифровать]', entities: [] },
        },
      } as Partial<ApiMessage>,
    });
  }
}

type DecryptedIncomingPayloadLike = {
  text?: string;
  media: ReadonlyArray<{
    id: string;
    mime: string;
    filename?: string;
    size: number;
    width?: number;
    height?: number;
    duration?: number;
    type: 'photo' | 'video' | 'voice' | 'file' | 'gif';
  }>;
};

function buildEncryptedContentPatch(decrypted: DecryptedIncomingPayloadLike): ApiMessage['content'] {
  const content: ApiMessage['content'] = {
    text: decrypted.text
      ? { text: decrypted.text, entities: [] }
      : undefined,
  };

  if (decrypted.media.length === 0) {
    // Pure text message. Provide an empty body so existing renderers
    // don't keep the '🔒' placeholder when the caption is blank.
    if (!content.text) {
      content.text = { text: '', entities: [] };
    }
    return content;
  }

  // Phase 7.1 composer only ships a single attachment at a time, so
  // patch the first item into the matching content slot. If/when
  // multi-attach encrypted messages land this becomes an album fan-out.
  const first = decrypted.media[0];
  switch (first.type) {
    case 'photo':
      content.photo = {
        mediaType: 'photo',
        id: first.id,
        date: 0,
        isEncrypted: true,
        sizes: [{
          width: first.width ?? 0,
          height: first.height ?? 0,
          type: 'x',
        }],
      };
      break;
    case 'video':
    case 'gif':
      content.video = {
        mediaType: 'video',
        id: first.id,
        mimeType: first.mime,
        duration: first.duration ?? 0,
        width: first.width ?? 0,
        height: first.height ?? 0,
        fileName: first.filename ?? (first.type === 'gif' ? 'animation.mp4' : 'video.mp4'),
        size: first.size,
        isEncrypted: true,
        isGif: first.type === 'gif' || undefined,
      };
      break;
    case 'voice':
      content.voice = {
        mediaType: 'voice',
        id: first.id,
        duration: first.duration ?? 0,
        size: first.size,
        isEncrypted: true,
      };
      break;
    case 'file':
    default:
      content.document = {
        mediaType: 'document',
        id: first.id,
        mimeType: first.mime,
        fileName: first.filename ?? 'file',
        size: first.size,
        isEncrypted: true,
      };
      break;
  }
  return content;
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

  // Phase 7 Step 10 security fix: drop the deleted message from the
  // client-side E2E search index so disappearing-message contents stop
  // being recoverable via local word search after the ciphertext is
  // gone. Fire-and-forget — index hygiene must never block the delete
  // from propagating to the UI.
  void (async () => {
    try {
      const { removeFromIndex } = await import('../../../lib/search/client-index');
      await removeFromIndex(chatId, seqNum);
    } catch {
      // Non-fatal — search index will rebuild on next decrypt populate.
    }
  })();
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
