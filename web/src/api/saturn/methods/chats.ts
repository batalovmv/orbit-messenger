import type {
  ApiChat, ApiChatInviteImporter, ApiMessage, ApiPoll, ApiUser, ApiUserStatus,
} from '../../types';
import type {
  SaturnChat,
  SaturnChatAvailableReactions,
  SaturnChatListItem,
  SaturnChatMember,
  SaturnJoinRequest,
  SaturnPaginatedResponse,
  SaturnUser,
} from '../types';

import type { ApiBotCommand } from '../../types';

import { ARCHIVED_FOLDER_ID } from '../../../config';
import { buildApiChat, buildApiChatFullInfo, buildApiChatMember } from '../apiBuilders/chats';
import { buildApiMessage, buildApiPoll } from '../apiBuilders/messages';
import { buildApiUser, buildApiUserStatus } from '../apiBuilders/users';
import * as client from '../client';
import { sendApiUpdate } from '../updates/apiUpdateEmitter';

let currentUserId: string | undefined;

export function setCurrentUserId(userId: string) {
  currentUserId = userId;
}

export function getCurrentUserId(): string | undefined {
  return currentUserId;
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
  const pollsById: Record<string, ApiPoll> = {};
  // TODO: This is an approximation — assumes contiguous sequence_numbers.
  // If messages were deleted, lastReadSeq may be off by the number of deletions.
  // Backend should return last_read_sequence_number directly in the chat list API.
  const threadReadStatesById: Record<string, {
    lastReadInboxMessageId: number;
    lastReadOutboxMessageId?: number;
    unreadCount: number;
  }> = {};

  for (const item of (result.data || [])) {
    const apiChat = buildApiChat(item);

    // Set creator flag from created_by field
    if (currentUserId && item.created_by === currentUserId) {
      apiChat.isCreator = true;
      // Creator is always owner with full admin rights
      apiChat.adminRights = {
        changeInfo: true, postMessages: true, deleteMessages: true,
        banUsers: true, inviteUsers: true, pinMessages: true,
        addAdmins: true, manageCall: true,
      };
    }

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
      // Some Saturn responses return a stale `last_message.chat_id` from another chat/thread.
      // In chat list context the parent item id is the source of truth for the preview message.
      const normalizedLastMessage = item.last_message.chat_id === item.id
        ? item.last_message
        : {
          ...item.last_message,
          chat_id: item.id,
        };
      const apiMessage = buildApiMessage(normalizedLastMessage);
      const poll = buildApiPoll(normalizedLastMessage.poll);
      if (currentUserId) {
        apiMessage.isOutgoing = item.last_message.sender_id === currentUserId;
      }
      apiChat.lastMessage = apiMessage;
      lastMessageByChatId[item.id] = apiMessage.id;
      messages.push(apiMessage);
      if (poll) {
        pollsById[poll.id] = poll;
      }

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
    polls: Object.values(pollsById),
    threadInfos: [],
    threadReadStatesById,
    hasMore: result.has_more,
    nextCursor: result.cursor,
  };
}

export async function fetchFullChat({ id: chatId, chatId: chatIdAlt }: { id?: string; chatId?: string }) {
  // TG Web A passes full ApiChat object with `id`, Saturn methods used `chatId`
  if (!chatId) chatId = chatIdAlt!;
  const [chat, membersResult, availableReactions] = await Promise.all([
    client.request<SaturnChat>('GET', `/chats/${chatId}`),
    client.request<SaturnPaginatedResponse<SaturnChatMember>>(
      'GET', `/chats/${chatId}/members?limit=200`,
    ),
    client.request<SaturnChatAvailableReactions>('GET', `/chats/${chatId}/available-reactions`)
      .catch(() => undefined),
  ]);

  const apiChat = buildApiChat(chat);
  const fullInfo = buildApiChatFullInfo(chat, membersResult.data, availableReactions);

  if (chat.type === 'direct') {
    const members = membersResult.data || [];
    const peerMember = members.find((member) => member.user_id !== currentUserId) || members[0];
    if (peerMember?.user_id) {
      apiChat.peerUserId = peerMember.user_id;

      try {
        const saturnUser = await client.request<SaturnUser>('GET', `/users/${peerMember.user_id}`);
        const apiUser = buildApiUser(saturnUser);
        const apiUserStatus = buildApiUserStatus(saturnUser);

        sendApiUpdate({
          '@type': 'updateUser',
          id: saturnUser.id,
          user: apiUser,
        });

        sendApiUpdate({
          '@type': 'updateUserStatus',
          userId: saturnUser.id,
          status: apiUserStatus,
        });

        if (!apiChat.title || apiChat.title === 'Saved Messages') {
          const fallbackTitle = [apiUser.firstName, apiUser.lastName].filter(Boolean).join(' ').trim();
          if (fallbackTitle) {
            apiChat.title = fallbackTitle;
          }
        }

        // Load bot commands for DM chats with bots
        if (apiUser.type === 'userTypeBot') {
          loadBotCommandsForChat(saturnUser.id, apiChat);
        }
      } catch (err) {
        // Keep the chat reachable even if user hydration fails.
      }
    }
  }

  // Set current user's admin rights and creator flag on chat object
  if (currentUserId && membersResult.data) {
    const me = membersResult.data.find((m) => m.user_id === currentUserId);
    if (me) {
      if (me.role === 'owner') {
        apiChat.isCreator = true;
      }
      if (me.role === 'owner' || me.role === 'admin') {
        const mask = me.permissions || 255;
        apiChat.adminRights = {
          changeInfo: Boolean(mask & (1 << 4)) || undefined,
          postMessages: Boolean(mask & (1 << 0)) || undefined,
          deleteMessages: Boolean(mask & (1 << 5)) || undefined,
          banUsers: Boolean(mask & (1 << 6)) || undefined,
          inviteUsers: Boolean(mask & (1 << 2)) || undefined,
          pinMessages: Boolean(mask & (1 << 3)) || undefined,
          addAdmins: me.role === 'owner' ? true as const : undefined,
          manageCall: true,
        };
      }
    }
  }

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
    members: (membersResult.data || []).map(buildApiChatMember),
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

export async function createDirectChatWithFallbackUser({
  user,
}: {
  user: Pick<ApiUser, 'id' | 'firstName' | 'lastName'>;
}) {
  const result = await createDirectChat({ userId: user.id });
  const { chat } = result;

  if (chat.type === 'chatTypePrivate') {
    chat.peerUserId ||= user.id;

    if (!chat.title || chat.title === 'Saved Messages') {
      const fallbackTitle = [user.firstName, user.lastName].filter(Boolean).join(' ').trim();
      if (fallbackTitle) {
        chat.title = fallbackTitle;
      }
    }
  }

  sendApiUpdate({
    '@type': 'updateChat',
    id: chat.id,
    chat,
    noTopChatsRequest: true,
  });

  return result;
}

export async function createGroupChat({
  name, description, memberIds,
}: {
  name: string;
  description?: string;
  memberIds?: string[];
}) {
  const body: Record<string, unknown> = { type: 'group', name };
  if (description) body.description = description;
  if (memberIds?.length) body.member_ids = memberIds;

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

export function getChatInviteLink({ chatId }: { chatId: string }) {
  // Saturn uses invite codes (not per-chat invite links) in Phase 1.
  // This will be implemented when per-chat invite link endpoint is added.
  void chatId;
  return undefined;
}

async function updateMemberPreferences(chatId: string, prefs: {
  is_pinned?: boolean;
  is_muted?: boolean;
  is_archived?: boolean;
}) {
  return client.request<SaturnChatMember>('PATCH', `/chats/${chatId}/members/me`, prefs);
}

function resolveChatId({ chatId, chat }: { chatId?: string; chat?: Pick<ApiChat, 'id'> }) {
  const resolvedChatId = chatId || chat?.id;

  if (!resolvedChatId) {
    throw new Error('chatId is required');
  }

  return resolvedChatId;
}

export async function editChatTitle({ chatId, title }: { chatId: string; title: string }) {
  await client.request('PUT', `/chats/${chatId}`, { name: title });
  sendApiUpdate({ '@type': 'updateChat', id: chatId, chat: { title } as any });
}

export async function editChatAbout({ chatId, about }: { chatId: string; about: string }) {
  await client.request('PUT', `/chats/${chatId}`, { description: about });
}

export async function deleteChat({ chatId }: { chatId: string }) {
  await client.request('DELETE', `/chats/${chatId}`);
}

export async function leaveChat({ chatId }: { chatId: string }) {
  await client.request('DELETE', `/chats/${chatId}/members/me`);
}

export async function addChatMembers({ chatId, userIds }: { chatId: string; userIds: string[] }) {
  await client.request('POST', `/chats/${chatId}/members`, { user_ids: userIds });
}

export async function deleteChatMember({ chatId, userId }: { chatId: string; userId: string }) {
  await client.request('DELETE', `/chats/${chatId}/members/${userId}`);
}

export async function updateChatAdmin({ chatId, userId, adminRights, customTitle }: {
  chatId: string; userId: string; adminRights?: any; customTitle?: string;
}) {
  const role = adminRights ? 'admin' : 'member';
  let permsBitmask = 0;
  if (adminRights) {
    if (adminRights.changeInfo) permsBitmask |= 1 << 4;
    if (adminRights.postMessages) permsBitmask |= 1 << 0;
    if (adminRights.deleteMessages) permsBitmask |= 1 << 5;
    if (adminRights.banUsers) permsBitmask |= 1 << 6;
    if (adminRights.inviteUsers) permsBitmask |= 1 << 2;
    if (adminRights.pinMessages) permsBitmask |= 1 << 3;
  }
  await client.request('PATCH', `/chats/${chatId}/members/${userId}`, {
    role, permissions: permsBitmask, custom_title: customTitle,
  });
}

export async function updateChatDefaultBannedRights({ chatId, bannedRights }: {
  chatId: string; bannedRights: any;
}) {
  let perms = 255;
  if (bannedRights?.sendMessages) perms &= ~(1 << 0);
  if (bannedRights?.sendMedia) perms &= ~(1 << 1);
  if (bannedRights?.inviteUsers) perms &= ~(1 << 2);
  if (bannedRights?.pinMessages) perms &= ~(1 << 3);
  if (bannedRights?.changeInfo) perms &= ~(1 << 4);
  await client.request('PUT', `/chats/${chatId}/permissions`, { permissions: perms });
}

export async function updateChatMemberBannedRights({ chatId, userId, bannedRights }: {
  chatId: string; userId: string; bannedRights: any;
}) {
  let perms = 255;
  if (bannedRights?.sendMessages) perms &= ~(1 << 0);
  if (bannedRights?.sendMedia) perms &= ~(1 << 1);
  if (bannedRights?.inviteUsers) perms &= ~(1 << 2);
  if (bannedRights?.pinMessages) perms &= ~(1 << 3);
  if (bannedRights?.changeInfo) perms &= ~(1 << 4);
  await client.request('PUT', `/chats/${chatId}/members/${userId}/permissions`, { permissions: perms });
}

export async function exportChatInviteLink({ chatId, title, expireDate, usageLimit, isRequestNeeded }: {
  chatId: string; title?: string; expireDate?: number; usageLimit?: number; isRequestNeeded?: boolean;
}) {
  const data = await client.request<any>('POST', `/chats/${chatId}/invite-link`, {
    title, expire_at: expireDate ? new Date(expireDate * 1000).toISOString() : undefined,
    usage_limit: usageLimit || 0, requires_approval: isRequestNeeded || false,
  });
  return data;
}

export async function fetchExportedChatInvites({
  peer,
}: { peer: { id: string }; admin?: unknown; limit?: number; isRevoked?: boolean }) {
  const data = await client.request<any[]>('GET', `/chats/${peer.id}/invite-links`);
  if (!data) return undefined;
  return {
    invites: data,
  };
}

export async function fetchChatInviteInfo({ hash }: { hash: string }) {
  const data = await client.request<any>('GET', `/chats/invite/${encodeURIComponent(hash)}`);
  return data;
}

export async function joinChat({ hash }: { hash: string }) {
  const data = await client.request<any>('POST', `/chats/join/${encodeURIComponent(hash)}`);
  return data;
}

export async function toggleSlowMode({ chatId, seconds }: { chatId: string; seconds: number }) {
  await client.request('POST', `/chats/${chatId}/slow-mode`, { seconds });
}

export async function fetchChatInviteImporters({ peer }: {
  peer: ApiChat; link?: string; offsetDate?: number; offsetUser?: ApiUser; limit?: number; isRequested?: boolean;
}): Promise<{ importers: ApiChatInviteImporter[] }> {
  const data = await client.request<SaturnJoinRequest[]>('GET', `/chats/${peer.id}/join-requests`);
  return {
    importers: (data || []).map((item) => ({
      userId: item.user_id,
      date: Math.floor(new Date(item.created_at).getTime() / 1000),
      about: item.message,
    })),
  };
}

export async function hideChatJoinRequest({ peer, user, isApproved }: {
  peer: ApiChat; user: ApiUser; isApproved: boolean;
}) {
  const action = isApproved ? 'approve' : 'reject';
  await client.request('POST', `/chats/${peer.id}/join-requests/${user.id}/${action}`);
  return true;
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
    replacedMembers: (result.data || []).map(buildApiChatMember),
  });

  return {
    members: (result.data || []).map(buildApiChatMember),
    hasMore: result.has_more,
  };
}

// --- Missing Phase 2 methods ---

export async function editExportedChatInvite({ chatId, link, title, expireDate, usageLimit, isRequestNeeded }: {
  chatId: string; link: string; title?: string; expireDate?: number;
  usageLimit?: number; isRequestNeeded?: boolean;
}) {
  // link contains the invite link ID
  const linkId = link;
  const data = await client.request<any>('PUT', `/invite-links/${linkId}`, {
    title,
    expire_at: expireDate ? new Date(expireDate * 1000).toISOString() : undefined,
    usage_limit: usageLimit,
    requires_approval: isRequestNeeded,
  });
  return data;
}

export async function deleteExportedChatInvite({ chatId, link }: { chatId: string; link: string }) {
  await client.request('DELETE', `/invite-links/${link}`);
}

export async function archiveChat({ chatId }: { chatId: string }) {
  await updateMemberPreferences(chatId, { is_archived: true });
  sendApiUpdate({
    '@type': 'updateChatListType',
    id: chatId,
    folderId: ARCHIVED_FOLDER_ID,
  });
}

export async function unarchiveChat({ chatId }: { chatId: string }) {
  await updateMemberPreferences(chatId, { is_archived: false });
  sendApiUpdate({
    '@type': 'updateChatListType',
    id: chatId,
    folderId: 0,
  });
}

export async function toggleChatArchived({ chat, folderId }: { chat: ApiChat; folderId: number }) {
  if (folderId === ARCHIVED_FOLDER_ID) {
    await archiveChat({ chatId: chat.id });
    return;
  }

  await unarchiveChat({ chatId: chat.id });
}

export async function toggleChatPinned({
  chatId,
  isPinned,
  chat,
  shouldBePinned,
}: {
  chatId?: string;
  isPinned?: boolean;
  chat?: ApiChat;
  shouldBePinned?: boolean;
}) {
  const resolvedChatId = resolveChatId({ chatId, chat });
  const nextPinned = isPinned ?? shouldBePinned ?? false;

  await updateMemberPreferences(resolvedChatId, { is_pinned: nextPinned });
  sendApiUpdate({
    '@type': 'updateChat',
    id: resolvedChatId,
    chat: { isPinned: nextPinned } as any,
  });
}

// Orbit has a single Saved Messages chat per user (see GetOrCreateSavedChat in
// services/messaging), not the Telegram-style sub-dialog threads produced by
// forwarding from multiple peers. `isSavedDialog` is therefore never true for
// a concrete pinnable entity, and this handler exists purely to silence the
// "[Saturn] Method not implemented" warning from the inherited TG Web A UI
// path. If you need to pin the Saved Messages chat itself, use toggleChatPinned.
export async function toggleSavedDialogPinned(_args: {
  chat: ApiChat;
  shouldBePinned: boolean;
}) {
  return undefined;
}

export async function setChatMuted({
  chatId,
  isMuted,
  chat,
}: {
  chatId?: string;
  isMuted: boolean;
  chat?: ApiChat;
}) {
  const resolvedChatId = resolveChatId({ chatId, chat });

  await updateMemberPreferences(resolvedChatId, { is_muted: isMuted });
  sendApiUpdate({
    '@type': 'updateChat',
    id: resolvedChatId,
    chat: { isMuted } as any,
  });
}

// Phase 7 follow-up: toggle the chat's "protected content" flag. The
// backend stores `is_protected` and re-emits a chat_updated NATS event;
// the frontend uses the flag to disable forwarding/copying/saving of
// messages from the chat.
export async function toggleIsProtected({
  chat,
  isProtected,
}: {
  chat: ApiChat;
  isProtected: boolean;
}) {
  await client.request<unknown>(
    'PUT',
    `/chats/${chat.id}/protected`,
    { is_protected: isProtected },
  );
  sendApiUpdate({
    '@type': 'updateChat',
    id: chat.id,
    chat: { isProtected } as any,
  });
}

export async function fetchMembers({ chat, type, offset, limit }: {
  chat: { id: string }; type?: string; offset?: number; limit?: number;
}) {
  return getChatMembers({ chatId: chat.id, limit: limit || 200 });
}

export async function searchMembers({ chatId, query, limit }: {
  chatId: string; query: string; limit?: number;
}) {
  const result = await client.request<SaturnChatMember[]>(
    'GET', `/chats/${chatId}/members?q=${encodeURIComponent(query)}&limit=${limit || 20}`,
  );
  if (!result) return undefined;
  return { members: result.map(buildApiChatMember) };
}

async function loadBotCommandsForChat(botUserId: string, apiChat: ApiChat) {
  try {
    const bot = await client.request<{ id: string }>('GET', `/bots/by-user/${botUserId}`);
    if (!bot?.id) return;

    const commands = await client.request<Array<{ command: string; description: string }>>(
      'GET', `/bots/${bot.id}/commands`,
    );
    if (!commands?.length) return;

    const botCommands: ApiBotCommand[] = commands.map((cmd) => ({
      botId: botUserId,
      command: cmd.command,
      description: cmd.description,
    }));

    apiChat.botCommands = botCommands;

    sendApiUpdate({
      '@type': 'updateChat',
      id: apiChat.id,
      chat: { botCommands },
      noTopChatsRequest: true,
    });
  } catch {
    // Bot commands loading is non-critical
  }
}
