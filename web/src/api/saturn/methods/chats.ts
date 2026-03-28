import type { ApiChat, ApiMessage, ApiUser, ApiUserStatus } from '../../types';
import type { SaturnChat, SaturnChatListItem, SaturnChatMember, SaturnPaginatedResponse } from '../types';

import { buildApiChat, buildApiChatFullInfo, buildApiChatMember } from '../apiBuilders/chats';
import { buildApiMessage } from '../apiBuilders/messages';
import { buildApiUser, buildApiUserStatus } from '../apiBuilders/users';
import * as client from '../client';
import { sendApiUpdate } from '../updates/apiUpdateEmitter';

let currentUserId: string | undefined;

export function setCurrentUserId(userId: string) {
  currentUserId = userId;
}

export async function fetchChats({
  limit = 50, cursor,
}: {
  limit?: number;
  cursor?: string;
}) {
  const params = new URLSearchParams();
  params.set('limit', String(limit));
  if (cursor) params.set('cursor', cursor);

  const result = await client.request<SaturnPaginatedResponse<SaturnChatListItem>>(
    'GET', `/chats?${params.toString()}`,
  );

  const apiChats: ApiChat[] = [];
  const apiUsers: ApiUser[] = [];
  const userStatusesById: Record<string, ApiUserStatus> = {};
  const lastMessageByChatId: Record<string, number> = {};
  const messages: ApiMessage[] = [];
  // TODO: This is an approximation — assumes contiguous sequence_numbers.
  // If messages were deleted, lastReadSeq may be off by the number of deletions.
  // Backend should return last_read_sequence_number directly in the chat list API.
  const threadReadStatesById: Record<string, { lastReadInboxMessageId: number; unreadCount: number }> = {};

  for (const item of (result.data || [])) {
    const apiChat = buildApiChat(item);
    apiChats.push(apiChat);

    sendApiUpdate({
      '@type': 'updateChat',
      id: item.id,
      chat: apiChat,
      noTopChatsRequest: true,
    });

    // For DM chats, collect the peer user
    if (item.type === 'direct' && item.other_user) {
      const peerUser = buildApiUser(item.other_user);
      apiUsers.push(peerUser);
      userStatusesById[item.other_user.id] = buildApiUserStatus(item.other_user);
      sendApiUpdate({ '@type': 'updateUser', id: item.other_user.id, user: peerUser });
      sendApiUpdate({
        '@type': 'updateUserStatus',
        userId: item.other_user.id,
        status: userStatusesById[item.other_user.id],
      });
      // Map chatId → peerUserId for DM chats.
      // In Telegram, chatId === peerId for DMs, but in Saturn they differ.
      // We store the peer userId on the chat object so MiddleHeader can resolve it.
      apiChat.peerUserId = item.other_user.id;
    }

    if (item.last_message) {
      const apiMessage = buildApiMessage(item.last_message);
      if (currentUserId) {
        apiMessage.isOutgoing = item.last_message.sender_id === currentUserId;
      }
      lastMessageByChatId[item.id] = apiMessage.id;
      messages.push(apiMessage);

      sendApiUpdate({
        '@type': 'updateChatLastMessage',
        id: item.id,
        lastMessage: apiMessage,
      });

      const lastMsgSeq = item.last_message.sequence_number;
      const lastReadSeq = item.unread_count > 0
        ? Math.max(lastMsgSeq - item.unread_count, 0)
        : lastMsgSeq;
      threadReadStatesById[item.id] = {
        lastReadInboxMessageId: lastReadSeq,
        lastReadOutboxMessageId: lastMsgSeq,
        unreadCount: item.unread_count,
      };
    } else if (item.unread_count > 0) {
      // Populate unread count even for chats without last_message
      threadReadStatesById[item.id] = {
        lastReadInboxMessageId: 0,
        unreadCount: item.unread_count,
      };
    }
  }

  const chatIds = apiChats.map((c) => c.id);
  const totalChatCount = result.has_more ? chatIds.length + 1 : chatIds.length;

  return {
    chatIds,
    chats: apiChats,
    users: apiUsers,
    userStatusesById,
    notifyExceptionById: {} as Record<string, never>,
    draftsById: {} as Record<string, undefined>,
    lastMessageByChatId,
    totalChatCount,
    messages,
    threadInfos: [],
    threadReadStatesById,
    hasMore: result.has_more,
    nextCursor: result.cursor,
  };
}

export async function fetchFullChat({ id: chatId, chatId: chatIdAlt }: { id?: string; chatId?: string }) {
  // TG Web A passes full ApiChat object with `id`, Saturn methods used `chatId`
  if (!chatId) chatId = chatIdAlt!;
  const [chat, membersResult] = await Promise.all([
    client.request<SaturnChat>('GET', `/chats/${chatId}`),
    client.request<SaturnPaginatedResponse<SaturnChatMember>>(
      'GET', `/chats/${chatId}/members?limit=200`,
    ),
  ]);

  const apiChat = buildApiChat(chat);
  const fullInfo = buildApiChatFullInfo(chat, membersResult.data);

  sendApiUpdate({
    '@type': 'updateChat',
    id: chatId,
    chat: apiChat,
    noTopChatsRequest: true,
  });

  sendApiUpdate({
    '@type': 'updateChatFullInfo',
    id: chatId,
    fullInfo,
  });

  return {
    chats: [apiChat],
    fullInfo,
    members: membersResult.data.map(buildApiChatMember),
    userStatusesById: {},
  };
}

export async function createDirectChat({ userId }: { userId: string }) {
  const chat = await client.request<SaturnChat>(
    'POST', '/chats/direct', { user_id: userId },
  );

  const apiChat = buildApiChat(chat);

  sendApiUpdate({
    '@type': 'updateChat',
    id: chat.id,
    chat: apiChat,
    noTopChatsRequest: true,
  });

  return { chat: apiChat };
}

export async function createGroupChat({
  name, description,
}: {
  name: string;
  description?: string;
}) {
  const body: Record<string, unknown> = { name };
  if (description) body.description = description;

  const chat = await client.request<SaturnChat>('POST', '/chats', body);
  const apiChat = buildApiChat(chat);

  sendApiUpdate({
    '@type': 'updateChat',
    id: chat.id,
    chat: apiChat,
    noTopChatsRequest: true,
  });

  return { chat: apiChat };
}

export async function getChatInviteLink({ chatId }: { chatId: string }) {
  // Saturn uses invite codes (not per-chat invite links) in Phase 1.
  // This will be implemented when per-chat invite link endpoint is added.
  return undefined;
}

export async function getChatMembers({
  chatId, limit = 50, cursor,
}: {
  chatId: string;
  limit?: number;
  cursor?: string;
}) {
  const params = new URLSearchParams();
  params.set('limit', String(limit));
  if (cursor) params.set('cursor', cursor);

  const result = await client.request<SaturnPaginatedResponse<SaturnChatMember>>(
    'GET', `/chats/${chatId}/members?${params.toString()}`,
  );

  sendApiUpdate({
    '@type': 'updateChatMembers',
    id: chatId,
    replacedMembers: result.data.map(buildApiChatMember),
  });

  return {
    members: result.data.map(buildApiChatMember),
    hasMore: result.has_more,
  };
}
