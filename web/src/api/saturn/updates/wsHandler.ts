import type { SaturnChat, SaturnMessage, SaturnWsMessage } from '../types';

import { buildApiChat } from '../apiBuilders/chats';
import { buildApiMessage, buildApiPoll, getMessageSeqNum } from '../apiBuilders/messages';
import { setWsMessageHandler } from '../client';
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
      sendApiUpdate({
        '@type': 'updateChat',
        id: payload.chat_id as string,
        chat: { isRestricted: true } as any,
      });
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
      if (payload.user_id === currentUserId) {
        sendApiUpdate({
          '@type': 'updateChat',
          id: payload.chat_id as string,
          chat: { isRestricted: true } as any,
        });
      } else {
        const { fetchFullChat } = await import('../methods/chats');
        fetchFullChat({ id: payload.chat_id as string });
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
      break;
    }
    case 'media_upload_progress': {
      break;
    }
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

function handleMessagesRead(data: Record<string, unknown>) {
  const chatId = data.chat_id as string;
  const userId = data.user_id as string;
  const lastReadUuid = data.last_read_message_id as string | undefined;

  if (!lastReadUuid) return;

  const lastReadSeqNum = getMessageSeqNum(lastReadUuid);
  if (!lastReadSeqNum) return;

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
  });
}

function handleUserStatus(data: Record<string, unknown>) {
  const userId = data.user_id as string;
  const status = data.status as string;
  const lastSeen = data.last_seen as string | undefined;

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

function handlePollUpdated(data: Record<string, unknown>, isClosed = false) {
  const poll = data.poll as Record<string, unknown> | undefined;
  const pollId = (poll?.id || data.poll_id) as string | undefined;
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
      ...buildApiPollFromWs(poll, isClosed),
    } as any,
  });

  const peerId = data.user_id as string | undefined;
  const options = Array.isArray(data.option_ids)
    ? data.option_ids.filter((optionId): optionId is string => typeof optionId === 'string')
    : undefined;

  if (peerId && options) {
    sendApiUpdate({
      '@type': 'updateMessagePollVote',
      pollId,
      peerId,
      options,
    });
  }
}

function buildApiPollFromWs(poll?: Record<string, unknown>, isClosed = false) {
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
          isChosen: Boolean(option.is_chosen) || undefined,
          isCorrect: Boolean(option.is_correct) || undefined,
        })),
      totalVoters: Number(poll.total_voters || 0),
    },
  };
}
