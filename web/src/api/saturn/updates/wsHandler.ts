import type { SaturnChat, SaturnMessage, SaturnWsMessage } from '../types';

import { buildApiChat } from '../apiBuilders/chats';
import { buildApiMessage, getMessageSeqNum } from '../apiBuilders/messages';
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
const deferredOwnMessages: Map<string, SaturnMessage> = new Map();
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
      handleMessageDeleted(msg.data as unknown as SaturnMessage);
      break;
    case 'messages_read':
      handleMessagesRead(msg.data as Record<string, unknown>);
      break;
    case 'typing':
      handleTyping(msg.data as Record<string, unknown>);
      break;
    case 'stop_typing':
      handleStopTyping(msg.data as Record<string, unknown>);
      break;
    case 'message_pinned':
    case 'message_unpinned':
      handleMessagePinChanged(msg.data as Record<string, unknown>);
      break;
    case 'user_status':
      handleUserStatus(msg.data as Record<string, unknown>);
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
      const payload = msg.data as Record<string, unknown>;
      const { fetchFullChat } = await import('../methods/chats');
      fetchFullChat({ id: (payload.chat_id || payload.id) as string });
      break;
    }
    case 'chat_deleted': {
      const payload = msg.data as Record<string, unknown>;
      sendApiUpdate({
        '@type': 'updateChat',
        id: payload.chat_id as string,
        chat: { isRestricted: true } as any,
      });
      break;
    }
    case 'chat_member_added': {
      const payload = msg.data as Record<string, unknown>;
      const { fetchFullChat } = await import('../methods/chats');
      fetchFullChat({ id: payload.chat_id as string });
      break;
    }
    case 'chat_member_removed': {
      const payload = msg.data as Record<string, unknown>;
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
      const payload = msg.data as Record<string, unknown>;
      const { fetchFullChat } = await import('../methods/chats');
      fetchFullChat({ id: payload.chat_id as string });
      break;
    }
    case 'mention': {
      const payload = msg.data as Record<string, unknown>;
      const { fetchFullChat } = await import('../methods/chats');
      fetchFullChat({ id: payload.chat_id as string });
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
