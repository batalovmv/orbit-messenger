import type { ApiMessage, ApiMessageEntity, ApiSendMessageAction } from '../../types';
import type { SaturnMessage, SaturnPaginatedResponse } from '../types';

import { buildApiMessage, buildSaturnEntities, getMessageUuid } from '../apiBuilders/messages';
import * as client from '../client';
import { sendApiUpdate } from '../updates/apiUpdateEmitter';
import { trackPendingSend } from '../updates/wsHandler';

let currentUserId: string | undefined;

export function setCurrentUserId(userId: string) {
  currentUserId = userId;
}

export async function fetchMessages({
  chatId, limit = 50, cursor,
}: {
  chatId: string;
  limit?: number;
  cursor?: string;
}) {
  const params = new URLSearchParams();
  params.set('limit', String(limit));
  if (cursor) params.set('cursor', cursor);

  const result = await client.request<SaturnPaginatedResponse<SaturnMessage>>(
    'GET', `/api/v1/chats/${chatId}/messages?${params.toString()}`,
  );

  const messages: ApiMessage[] = result.items.map((msg) => {
    const apiMsg = buildApiMessage(msg);
    if (currentUserId) {
      apiMsg.isOutgoing = msg.sender_id === currentUserId;
    }
    return apiMsg;
  });

  return {
    messages,
    hasMore: result.has_more,
    nextCursor: result.next_cursor,
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
    'GET', `/api/v1/chats/${chatId}/history?${params.toString()}`,
  );

  return {
    messages: result.items.map((msg) => {
      const apiMsg = buildApiMessage(msg);
      if (currentUserId) {
        apiMsg.isOutgoing = msg.sender_id === currentUserId;
      }
      return apiMsg;
    }),
    hasMore: result.has_more,
  };
}

let localMessageCounter = -1; // Negative IDs for local/pending messages

export async function sendMessage({
  chatId, text, entities, replyToId,
}: {
  chatId: string;
  text: string;
  entities?: ApiMessageEntity[];
  replyToId?: number;
}) {
  // Create local (optimistic) message
  const localId = localMessageCounter--;
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

  // Dispatch optimistic update
  sendApiUpdate({
    '@type': 'newMessage',
    chatId,
    id: localId,
    message: localMessage,
  });

  try {
    const body: Record<string, unknown> = { content: text };
    if (entities?.length) {
      body.entities = buildSaturnEntities(entities);
    }
    if (replyToId) {
      const replyUuid = getMessageUuid(chatId, replyToId);
      if (replyUuid) body.reply_to_id = replyUuid;
    }

    const msg = await client.request<SaturnMessage>(
      'POST', `/api/v1/chats/${chatId}/messages`, body,
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
  const uuid = getMessageUuid(chatId, messageId);
  if (!uuid) return undefined;

  const body: Record<string, unknown> = { content: text };
  if (entities?.length) {
    body.entities = buildSaturnEntities(entities);
  }

  const msg = await client.request<SaturnMessage>(
    'PATCH', `/api/v1/messages/${uuid}`, body,
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
  chatId, messageIds,
}: {
  chatId: string;
  messageIds: number[];
}) {
  const deletePromises = messageIds.map(async (seqNum) => {
    const uuid = getMessageUuid(chatId, seqNum);
    if (!uuid) return;
    await client.request('DELETE', `/api/v1/messages/${uuid}`);
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
    .map((seqNum) => getMessageUuid(fromChatId, seqNum))
    .filter(Boolean) as string[];

  if (!uuids.length) return undefined;

  const result = await client.request<{ messages: SaturnMessage[] }>(
    'POST', '/api/v1/messages/forward', {
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

export async function fetchPinnedMessages({ chatId }: { chatId: string }) {
  const result = await client.request<{ messages: SaturnMessage[] }>(
    'GET', `/api/v1/chats/${chatId}/pinned`,
  );

  const messages = result.messages.map((msg) => {
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
  chatId, messageId,
}: {
  chatId: string;
  messageId: number;
}) {
  const uuid = getMessageUuid(chatId, messageId);
  if (!uuid) return;

  await client.request('POST', `/api/v1/chats/${chatId}/pin/${uuid}`);
}

export async function unpinMessage({
  chatId, messageId,
}: {
  chatId: string;
  messageId: number;
}) {
  const uuid = getMessageUuid(chatId, messageId);
  if (!uuid) return;

  await client.request('DELETE', `/api/v1/chats/${chatId}/pin/${uuid}`);
}

export async function unpinAllMessages({ chatId }: { chatId: string }) {
  await client.request('DELETE', `/api/v1/chats/${chatId}/pin`);

  sendApiUpdate({
    '@type': 'updatePinnedIds',
    chatId,
    messageIds: [],
  });
}

export async function markMessageListRead({
  chatId, maxId,
}: {
  chatId: string;
  maxId: number;
}) {
  const uuid = getMessageUuid(chatId, maxId);
  if (!uuid) return;

  await client.request('PATCH', `/api/v1/chats/${chatId}/read`, {
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
  const uuid = getMessageUuid(chatId, messageId);
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
