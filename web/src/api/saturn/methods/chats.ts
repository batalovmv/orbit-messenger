import type { ApiChat } from '../../types';
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
    'GET', `/api/v1/chats?${params.toString()}`,
  );

  const apiChats: ApiChat[] = [];

  for (const item of result.items) {
    const apiChat = buildApiChat(item);
    apiChats.push(apiChat);

    sendApiUpdate({
      '@type': 'updateChat',
      id: item.id,
      chat: apiChat,
      noTopChatsRequest: true,
    });

    // For DM chats, dispatch the peer user so TG Web A can resolve the title/avatar
    if (item.type === 'direct' && item.other_user) {
      const peerUser = buildApiUser(item.other_user);
      sendApiUpdate({ '@type': 'updateUser', id: item.other_user.id, user: peerUser });
      sendApiUpdate({
        '@type': 'updateUserStatus',
        userId: item.other_user.id,
        status: buildApiUserStatus(item.other_user),
      });
    }

    if (item.last_message) {
      const apiMessage = buildApiMessage(item.last_message);
      if (currentUserId) {
        apiMessage.isOutgoing = item.last_message.sender_id === currentUserId;
      }

      sendApiUpdate({
        '@type': 'updateChatLastMessage',
        id: item.id,
        lastMessage: apiMessage,
      });
    }
  }

  return {
    chatIds: apiChats.map((c) => c.id),
    hasMore: result.has_more,
    nextCursor: result.next_cursor,
  };
}

export async function fetchFullChat({ chatId }: { chatId: string }) {
  const [chat, membersResult] = await Promise.all([
    client.request<SaturnChat>('GET', `/api/v1/chats/${chatId}`),
    client.request<SaturnPaginatedResponse<SaturnChatMember>>(
      'GET', `/api/v1/chats/${chatId}/members?limit=200`,
    ),
  ]);

  const apiChat = buildApiChat(chat);
  const fullInfo = buildApiChatFullInfo(chat, membersResult.items);

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
    chat: apiChat,
    fullInfo,
    members: membersResult.items.map(buildApiChatMember),
  };
}

export async function createDirectChat({ userId }: { userId: string }) {
  const chat = await client.request<SaturnChat>(
    'POST', '/api/v1/chats/direct', { user_id: userId },
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

  const chat = await client.request<SaturnChat>('POST', '/api/v1/chats', body);
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
    'GET', `/api/v1/chats/${chatId}/members?${params.toString()}`,
  );

  sendApiUpdate({
    '@type': 'updateChatMembers',
    id: chatId,
    replacedMembers: result.items.map(buildApiChatMember),
  });

  return {
    members: result.items.map(buildApiChatMember),
    hasMore: result.has_more,
  };
}
