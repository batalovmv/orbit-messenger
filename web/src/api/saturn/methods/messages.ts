import type { ApiMessage, ApiMessageEntity, ApiSendMessageAction } from '../../types';
import type { SaturnMessage, SaturnPaginatedResponse } from '../types';

import { buildApiMessage, buildSaturnEntities, getMessageUuid } from '../apiBuilders/messages';
import * as client from '../client';
import { sendApiUpdate, sendImmediateApiUpdate } from '../updates/apiUpdateEmitter';
import { trackPendingSend } from '../updates/wsHandler';

let currentUserId: string | undefined;

export function setCurrentUserId(userId: string) {
  currentUserId = userId;
}

// Resolve sequence_number → Saturn UUID, with fallback to global state's saturnId field
function resolveMessageUuid(chatId: string, seqNum: number): string | undefined {
  // Try in-memory map first (fast path)
  const uuid = getMessageUuid(chatId, seqNum);
  if (uuid) return uuid;
  // Fallback: look up saturnId from cached ApiMessage in global state
  const global = (window as any).getGlobal?.();
  const msg = global?.messages?.byChatId?.[chatId]?.byId?.[seqNum];
  return msg?.saturnId;
}

export async function fetchMessages({
  chat, chatId: chatIdDirect, limit = 50, cursor, offsetId,
}: {
  chat?: { id: string };
  chatId?: string;
  limit?: number;
  cursor?: string;
  offsetId?: number;
}) {
  const chatId = chat?.id || chatIdDirect!;
  const params = new URLSearchParams();
  params.set('limit', String(limit));
  if (cursor) {
    params.set('cursor', cursor);
  } else if (offsetId !== undefined) {
    // cursor returns messages with seq < cursor (DESC order)
    // Add half the limit as forward buffer so we load messages AROUND offsetId
    // (both older and newer), not just older ones
    const forwardBuffer = Math.ceil(limit / 2);
    params.set('cursor', btoa(String(offsetId + forwardBuffer)));
  }

  const result = await client.request<SaturnPaginatedResponse<SaturnMessage>>(
    'GET', `/chats/${chatId}/messages?${params.toString()}`,
  );

  const messages: ApiMessage[] = result.data.map((msg) => {
    const apiMsg = buildApiMessage(msg);
    if (currentUserId) {
      apiMsg.isOutgoing = msg.sender_id === currentUserId;
    }
    return apiMsg;
  });

  return {
    messages,
    count: messages.length,
    topics: [] as any[],
    hasMore: result.has_more,
    nextCursor: result.cursor,
  };
}

export async function fetchMessagesByDate({
  chatId, date, limit = 50,
}: {
  chatId: string;
  date: string; // RFC3339
  limit?: number;
}) {
  const params = new URLSearchParams();
  params.set('date', date);
  params.set('limit', String(limit));

  const result = await client.request<SaturnPaginatedResponse<SaturnMessage>>(
    'GET', `/chats/${chatId}/history?${params.toString()}`,
  );

  return {
    messages: result.data.map((msg) => {
      const apiMsg = buildApiMessage(msg);
      if (currentUserId) {
        apiMsg.isOutgoing = msg.sender_id === currentUserId;
      }
      return apiMsg;
    }),
    hasMore: result.has_more,
  };
}

let localMessageCounter = 0;
const LOCAL_MESSAGES_LIMIT = 1e6; // Must match config.ts — keeps local IDs fractional

export async function sendMessage({
  chat, text, entities, replyInfo, lastMessageId,
}: {
  chat: { id: string };
  text?: string;
  entities?: ApiMessageEntity[];
  replyInfo?: { replyToMsgId?: number; type?: string };
  lastMessageId?: number;
}) {
  const chatId = chat.id;
  if (!text) return;

  // Create local (optimistic) message.
  // ID must be fractional so isLocalMessageId() recognizes it (checks !Number.isInteger).
  // Format matches TG Web A: lastMessageId + counter/1e6 — sorts after existing messages.
  const localId = (lastMessageId || Math.floor(Date.now() / 1000)) + (++localMessageCounter / LOCAL_MESSAGES_LIMIT);
  const now = Math.floor(Date.now() / 1000);

  const localMessage: ApiMessage = {
    id: localId,
    chatId,
    date: now,
    isOutgoing: true,
    senderId: currentUserId,
    content: {
      text: { text, entities: entities || [] },
    },
    sendingState: 'messageSendingStatePending',
  };

  // Dispatch optimistic update immediately (not batched) so the local message
  // renders in its own frame before the HTTP response arrives
  sendImmediateApiUpdate({
    '@type': 'newMessage',
    chatId,
    id: localId,
    message: localMessage,
  });

  // Yield to browser render loop so the local message with ⏱ appears
  // before the HTTP call blocks the microtask queue
  await new Promise((resolve) => { setTimeout(resolve, 0); });

  const replyToId = replyInfo?.type === 'message' ? replyInfo.replyToMsgId : undefined;

  try {
    const body: Record<string, unknown> = { content: text };
    if (entities?.length) {
      body.entities = buildSaturnEntities(entities);
    }
    if (replyToId) {
      const replyUuid = resolveMessageUuid(chatId, replyToId);
      if (replyUuid) body.reply_to_id = replyUuid;
    }

    const msg = await client.request<SaturnMessage>(
      'POST', `/chats/${chatId}/messages`, body,
    );

    // Track UUID so WS handler skips the echo of our own message
    trackPendingSend(msg.id);

    const apiMsg = buildApiMessage(msg);
    apiMsg.isOutgoing = true;

    sendApiUpdate({
      '@type': 'updateMessageSendSucceeded',
      chatId,
      localId,
      message: apiMsg,
    });

    return apiMsg;
  } catch (e) {
    sendApiUpdate({
      '@type': 'updateMessageSendFailed',
      chatId,
      localId,
      error: e instanceof Error ? e.message : 'Failed to send message',
    });
    return undefined;
  }
}

export async function editMessage({
  chatId, messageId, text, entities,
}: {
  chatId: string;
  messageId: number;
  text: string;
  entities?: ApiMessageEntity[];
}) {
  const uuid = resolveMessageUuid(chatId, messageId);
  if (!uuid) return undefined;

  const body: Record<string, unknown> = { content: text };
  if (entities?.length) {
    body.entities = buildSaturnEntities(entities);
  }

  const msg = await client.request<SaturnMessage>(
    'PATCH', `/messages/${uuid}`, body,
  );

  const apiMsg = buildApiMessage(msg);
  if (currentUserId) {
    apiMsg.isOutgoing = msg.sender_id === currentUserId;
  }

  sendApiUpdate({
    '@type': 'updateMessage',
    chatId,
    id: messageId,
    isFull: true,
    message: apiMsg,
  });

  return apiMsg;
}

export async function deleteMessages({
  chat, messageIds,
}: {
  chat: { id: string };
  messageIds: number[];
}) {
  const chatId = chat.id;
  const deletePromises = messageIds.map(async (seqNum) => {
    const uuid = resolveMessageUuid(chatId, seqNum);
    if (!uuid) {
      // eslint-disable-next-line no-console
      console.warn('[Saturn] deleteMessage: no UUID for', chatId, seqNum);
      return;
    }
    await client.request('DELETE', `/messages/${uuid}`);
  });

  await Promise.all(deletePromises);

  sendApiUpdate({
    '@type': 'deleteMessages',
    ids: messageIds,
    chatId,
  });
}

export async function forwardMessages({
  fromChatId, messageIds, toChatId,
}: {
  fromChatId: string;
  messageIds: number[];
  toChatId: string;
}) {
  const uuids = messageIds
    .map((seqNum) => resolveMessageUuid(fromChatId, seqNum))
    .filter(Boolean) as string[];

  if (!uuids.length) return undefined;

  const result = await client.request<{ messages: SaturnMessage[] }>(
    'POST', '/messages/forward', {
      message_ids: uuids,
      to_chat_id: toChatId,
    },
  );

  const apiMessages = result.messages.map((msg) => {
    const apiMsg = buildApiMessage(msg);
    apiMsg.isOutgoing = true;
    return apiMsg;
  });

  apiMessages.forEach((apiMsg) => {
    sendApiUpdate({
      '@type': 'newMessage',
      chatId: toChatId,
      id: apiMsg.id,
      message: apiMsg,
    });
  });

  return { messages: apiMessages };
}

export async function fetchPinnedMessages({ chat, chatId: chatIdDirect }: { chat?: { id: string }; chatId?: string }) {
  const chatId = chat?.id || chatIdDirect!;
  const result = await client.request<{ messages: SaturnMessage[] }>(
    'GET', `/chats/${chatId}/pinned`,
  );

  const rawMessages = result.messages || [];
  const messages = rawMessages.map((msg) => {
    const apiMsg = buildApiMessage(msg);
    if (currentUserId) {
      apiMsg.isOutgoing = msg.sender_id === currentUserId;
    }
    return apiMsg;
  });

  const pinnedIds = messages.map((m) => m.id);

  sendApiUpdate({
    '@type': 'updatePinnedIds',
    chatId,
    messageIds: pinnedIds,
  });

  return { messages, pinnedIds };
}

export async function pinMessage({
  chat, messageId, isUnpin,
}: {
  chat: { id: string };
  messageId: number;
  isUnpin?: boolean;
}) {
  const chatId = chat.id;
  const uuid = resolveMessageUuid(chatId, messageId);
  if (!uuid) {
    // eslint-disable-next-line no-console
    console.warn('[Saturn] pinMessage: no UUID for', chatId, messageId);
    return;
  }

  if (isUnpin) {
    await client.request('DELETE', `/chats/${chatId}/pin/${uuid}`);
  } else {
    await client.request('POST', `/chats/${chatId}/pin/${uuid}`);
  }
}

export async function unpinMessage({
  chat, messageId,
}: {
  chat: { id: string };
  messageId: number;
}) {
  const chatId = chat.id;
  const uuid = resolveMessageUuid(chatId, messageId);
  if (!uuid) return;

  await client.request('DELETE', `/chats/${chatId}/pin/${uuid}`);
}

export async function unpinAllMessages({ chat }: { chat: { id: string } }) {
  const chatId = chat.id;
  await client.request('DELETE', `/chats/${chatId}/pin`);

  sendApiUpdate({
    '@type': 'updatePinnedIds',
    chatId,
    messageIds: [],
  });
}

export async function markMessageListRead({
  chat, maxId,
}: {
  chat: { id: string };
  threadId?: number;
  maxId: number;
}) {
  const chatId = chat.id;
  const uuid = resolveMessageUuid(chatId, maxId);
  if (!uuid) {
    // eslint-disable-next-line no-console
    console.warn('[Saturn] markMessageListRead: no UUID for seq', maxId, 'in chat', chatId);
    return;
  }

  await client.request('PATCH', `/chats/${chatId}/read`, {
    last_read_message_id: uuid,
  });

  // Immediately update local state so unread badge clears
  sendApiUpdate({
    '@type': 'updateChat',
    id: chatId,
    chat: {},
    readState: {
      lastReadInboxMessageId: maxId,
      unreadCount: 0,
    },
    noTopChatsRequest: true,
  });
}

export async function fetchMessageLink({
  chatId, messageId,
}: {
  chatId: string;
  messageId: number;
}) {
  // Saturn doesn't have a message permalink endpoint yet.
  // Return a client-side constructed link for now.
  const uuid = resolveMessageUuid(chatId, messageId);
  if (!uuid) return undefined;

  return { link: `#chat/${chatId}/${uuid}` };
}

export function sendMessageAction({
  peer, action,
}: {
  peer: { id: string };
  threadId?: number;
  action: ApiSendMessageAction;
}) {
  if (action.type === 'cancel') {
    client.sendWsMessage('stop_typing', { chat_id: peer.id });
  } else {
    client.sendWsMessage('typing', { chat_id: peer.id });
  }
}
