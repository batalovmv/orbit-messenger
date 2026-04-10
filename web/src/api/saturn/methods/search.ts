// Phase 4: Search via Meilisearch backend

import { getGlobal } from '../../../global';

import type {
  ApiChat,
  ApiMessage,
  ApiUser,
  ApiUserStatus,
} from '../../types';
import type {
  SaturnChat,
  SaturnChatListItem,
  SaturnChatMember,
  SaturnChatSearchHit,
  SaturnMessage,
  SaturnMessageSearchHit,
  SaturnPaginatedResponse,
  SaturnSearchMatchPosition,
  SaturnSearchResponse,
  SaturnUser,
  SaturnUserSearchHit,
} from '../types';

import { DEBUG } from '../../../config';
import { buildApiChat } from '../apiBuilders/chats';
import { buildApiMessage } from '../apiBuilders/messages';
import { buildApiUser, buildApiUserStatus } from '../apiBuilders/users';
import { request } from '../client';
import { sendApiUpdate } from '../updates/apiUpdateEmitter';

const DEFAULT_SEARCH_LIMIT = 20;
const SEARCH_SNIPPET_PADDING = 48;
const SEARCH_SNIPPET_MAX_HIGHLIGHT = 160;

const userCache = new Map<string, SaturnUser>();
const chatCache = new Map<string, SaturnChat | SaturnChatListItem>();
const pendingUsersById = new Map<string, Promise<SaturnUser | undefined>>();
const pendingChatsById = new Map<string, Promise<SaturnChat | SaturnChatListItem | undefined>>();

let currentUserId: string | undefined;

interface SearchResults {
  messages: ApiMessage[];
  topics: never[];
  userStatusesById: Record<string, ApiUserStatus>;
  totalCount: number;
  nextOffsetRate?: number;
  nextOffsetPeerId?: string;
  nextOffsetId?: number;
}

type SearchMessageFilterType = 'text' | 'photo' | 'video' | 'file' | 'link';

export function setCurrentUserId(userId: string) {
  currentUserId = userId;
}

export async function searchMessagesGlobal({
  query,
  chatId,
  fromUserId,
  dateFrom,
  dateTo,
  type,
  hasMedia,
  limit = DEFAULT_SEARCH_LIMIT,
  offset,
  offsetId,
  minDate,
  maxDate,
}: {
  query: string;
  chatId?: string;
  fromUserId?: string;
  dateFrom?: string;
  dateTo?: string;
  type?: string;
  hasMedia?: boolean;
  limit?: number;
  offset?: number;
  // TG Web A passes these but Saturn doesn't use them
  offsetRate?: number;
  offsetPeer?: unknown;
  offsetId?: number;
  minDate?: number;
  maxDate?: number;
  context?: string;
}): Promise<SearchResults | undefined> {
  const normalizedQuery = normalizeSearchQuery(query, {
    chatId,
    fromUserId,
    dateFrom,
    dateTo,
    type,
    hasMedia,
    minDate,
    maxDate,
  });
  if (!normalizedQuery) return undefined;

  const normalizedOffset = offset ?? offsetId ?? 0;
  const params = buildSearchParams({
    query: normalizedQuery,
    scope: 'messages',
    limit,
    offset: normalizedOffset,
    chatId,
    fromUserId,
    dateFrom: dateFrom || formatSearchDate(minDate),
    dateTo: dateTo || formatSearchDate(maxDate),
    type,
    hasMedia,
  });

  try {
    const result = await request<SaturnSearchResponse<SaturnMessageSearchHit>>('GET', `/search?${params.toString()}`);
    const hits = result.results || [];

    const global = getGlobal();
    const missingChatIds = collectUniqueIds(
      hits.map((hit) => hit.chat_id),
      (id) => Boolean(id) && !global.chats.byId[id] && !chatCache.has(id),
    );
    const missingUserIds = collectUniqueIds(
      hits.map((hit) => hit.sender_id),
      (id) => Boolean(id) && !global.users.byId[id] && !userCache.has(id),
    );

    const [fetchedChats, fetchedUsers] = await Promise.all([
      fetchChatsByIds(missingChatIds),
      fetchUsersByIds(missingUserIds),
    ]);

    emitChats(Object.values(fetchedChats));
    emitUsers(Object.values(fetchedUsers));

    const userStatusesById = buildStatusesById(Object.values(fetchedUsers));
    const messages = (await Promise.all(
      hits.map(async (hit) => (
        await hydrateSearchMessage(hit) || buildSearchMessage(hit, fetchedUsers[hit.sender_id || ''])
      )),
    )).filter((message): message is ApiMessage => Boolean(message));
    const nextOffsetId = normalizedOffset + messages.length < result.total
      ? normalizedOffset + messages.length
      : undefined;

    return {
      messages,
      topics: [],
      userStatusesById,
      totalCount: result.total || 0,
      nextOffsetId,
    };
  } catch (err) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.error('[search] searchMessagesGlobal failed', err);
    }
    return undefined;
  }
}

export async function searchMessagesInChatWithFilters({
  chatId,
  query,
  fromUserId,
  dateFrom,
  dateTo,
  type,
  hasMedia,
  limit = DEFAULT_SEARCH_LIMIT,
  offset = 0,
  minDate,
  maxDate,
}: {
  chatId: string;
  query?: string;
  fromUserId?: string;
  dateFrom?: string;
  dateTo?: string;
  type?: SearchMessageFilterType;
  hasMedia?: boolean;
  limit?: number;
  offset?: number;
  minDate?: number;
  maxDate?: number;
}): Promise<SearchResults | undefined> {
  const normalizedQuery = query?.trim()
    || (fromUserId || dateFrom || dateTo || type || hasMedia !== undefined ? ' ' : undefined);
  const normalizedOffset = offset || 0;
  if (!chatId || !normalizedQuery) {
    return undefined;
  }

  const params = buildSearchParams({
    query: normalizedQuery,
    scope: 'messages',
    limit,
    offset: normalizedOffset,
    chatId,
    fromUserId,
    dateFrom: dateFrom || formatSearchDate(minDate),
    dateTo: dateTo || formatSearchDate(maxDate),
    type,
    hasMedia,
  });

  try {
    const result = await request<SaturnSearchResponse<SaturnMessageSearchHit>>('GET', `/search?${params.toString()}`);
    const global = getGlobal();
    const missingChatIds = collectUniqueIds(
      result.results.map((hit) => hit.chat_id),
      (id) => Boolean(id) && !global.chats.byId[id] && !chatCache.has(id),
    );
    const missingUserIds = collectUniqueIds(
      result.results.map((hit) => hit.sender_id),
      (id) => Boolean(id) && !global.users.byId[id] && !userCache.has(id),
    );

    const [fetchedChats, fetchedUsers] = await Promise.all([
      fetchChatsByIds(missingChatIds),
      fetchUsersByIds(missingUserIds),
    ]);

    emitChats(Object.values(fetchedChats));
    emitUsers(Object.values(fetchedUsers));

    const messages = (await Promise.all(
      result.results.map((hit) => hydrateSearchMessage(hit)),
    )).filter((message): message is ApiMessage => Boolean(message));
    const nextOffsetId = normalizedOffset + result.results.length < result.total
      ? normalizedOffset + result.results.length
      : undefined;

    return {
      messages,
      topics: [],
      userStatusesById: buildStatusesById(Object.values(fetchedUsers)),
      totalCount: result.total || 0,
      nextOffsetId,
    };
  } catch (err) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.error('[search] searchMessagesInChatWithFilters failed', err);
    }
    return undefined;
  }
}

export async function searchUsersGlobal({
  query,
  limit = DEFAULT_SEARCH_LIMIT,
}: {
  query: string;
  limit?: number;
}): Promise<ApiUser[] | undefined> {
  if (!query) return undefined;

  const params = buildSearchParams({
    query,
    scope: 'users',
    limit,
  });

  try {
    const result = await request<SaturnSearchResponse<SaturnUserSearchHit>>('GET', `/search?${params.toString()}`);
    const global = getGlobal();
    const missingUserIds = collectUniqueIds(
      result.results.map((hit) => hit.id),
      (id) => Boolean(id) && !global.users.byId[id] && !userCache.has(id),
    );
    const fetchedUsers = await fetchUsersByIds(missingUserIds);
    emitUsers(Object.values(fetchedUsers));

    return result.results
      .map((hit) => {
        const user = fetchedUsers[hit.id] || normalizeUserHit(hit);
        if (!user) {
          return undefined;
        }

        const apiUser = buildApiUser(user);
        if (currentUserId && user.id === currentUserId) {
          apiUser.isSelf = true;
        }

        return apiUser;
      })
      .filter((user): user is ApiUser => Boolean(user));
  } catch (err) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.error('[search] searchUsersGlobal failed', err);
    }
    return undefined;
  }
}

export async function searchChatsGlobal({
  query,
  limit = DEFAULT_SEARCH_LIMIT,
}: {
  query: string;
  limit?: number;
}): Promise<ApiChat[] | undefined> {
  if (!query) return undefined;

  const params = buildSearchParams({
    query,
    scope: 'chats',
    limit,
  });

  try {
    const result = await request<SaturnSearchResponse<SaturnChatSearchHit>>('GET', `/search?${params.toString()}`);
    const global = getGlobal();
    const missingChatIds = collectUniqueIds(
      result.results.map((hit) => hit.id),
      (id) => Boolean(id) && !global.chats.byId[id] && !chatCache.has(id),
    );
    const fetchedChats = await fetchChatsByIds(missingChatIds);
    emitChats(Object.values(fetchedChats));

    return result.results
      .map((hit) => {
        const chat = fetchedChats[hit.id] || normalizeChatHit(hit);
        return chat ? buildApiChat(chat) : undefined;
      })
      .filter((chat): chat is ApiChat => Boolean(chat));
  } catch (err) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.error('[search] searchChatsGlobal failed', err);
    }
    return undefined;
  }
}

function buildSearchParams({
  query,
  scope,
  limit,
  offset,
  chatId,
  fromUserId,
  dateFrom,
  dateTo,
  type,
  hasMedia,
}: {
  query: string;
  scope: 'messages' | 'users' | 'chats';
  limit?: number;
  offset?: number;
  chatId?: string;
  fromUserId?: string;
  dateFrom?: string;
  dateTo?: string;
  type?: string;
  hasMedia?: boolean;
}) {
  const params = new URLSearchParams();
  params.set('q', query);
  params.set('scope', scope);

  if (limit) {
    params.set('limit', String(limit));
  }

  if (offset) {
    params.set('offset', String(offset));
  }

  appendSearchParam(params, 'chat_id', chatId);
  appendSearchParam(params, 'from', fromUserId);
  appendSearchParam(params, 'after', dateFrom);
  appendSearchParam(params, 'before', dateTo);
  // Only pass message types that Saturn backend supports; ignore TG-specific types like 'text', 'channels'
  const SUPPORTED_TYPES = ['photo', 'video', 'file', 'voice', 'video_note', 'sticker', 'gif'];
  if (type && SUPPORTED_TYPES.includes(type)) {
    appendSearchParam(params, 'type', type);
  }

  if (hasMedia !== undefined) {
    params.set('has_media', String(hasMedia));
  }

  return params;
}

function appendSearchParam(params: URLSearchParams, key: string, value?: string) {
  if (value) {
    params.set(key, value);
  }
}

function collectUniqueIds(ids: Array<string | undefined>, predicate: (id: string) => boolean) {
  return [...new Set(ids.filter((id): id is string => Boolean(id) && predicate(id)))];
}

function normalizeMessageType(type?: string): SaturnMessage['type'] {
  if (type === 'video_note') {
    return 'videonote';
  }

  return type || 'text';
}

function normalizeTimestamp(timestamp: number) {
  return timestamp > 1e12 ? timestamp : timestamp * 1000;
}

function formatSearchDate(timestamp?: number) {
  if (!timestamp) {
    return undefined;
  }

  return new Date(normalizeTimestamp(timestamp)).toISOString();
}

function normalizeSearchQuery(query: string | undefined, filters: {
  chatId?: string;
  fromUserId?: string;
  dateFrom?: string;
  dateTo?: string;
  type?: string;
  hasMedia?: boolean;
  minDate?: number;
  maxDate?: number;
}) {
  const normalizedQuery = query?.trim();
  if (normalizedQuery) {
    return normalizedQuery;
  }

  const hasFilterOnlySearch = Boolean(
    filters.chatId
    || filters.fromUserId
    || filters.dateFrom
    || filters.dateTo
    || filters.type
    || filters.hasMedia !== undefined
    || filters.minDate
    || filters.maxDate,
  );

  return hasFilterOnlySearch ? ' ' : undefined;
}

async function fetchUsersByIds(userIds: string[]) {
  const users = await Promise.all(userIds.map((userId) => fetchUserById(userId)));

  return userIds.reduce<Record<string, SaturnUser>>((byId, userId, index) => {
    const user = users[index];
    if (user) {
      byId[userId] = user;
    }

    return byId;
  }, {});
}

async function fetchUserById(userId: string) {
  const cached = userCache.get(userId);
  if (cached) {
    return cached;
  }

  const pending = pendingUsersById.get(userId);
  if (pending) {
    return pending;
  }

  const promise = request<SaturnUser>('GET', `/users/${userId}`)
    .then((user) => {
      userCache.set(userId, user);
      return user;
    })
    .catch(() => undefined)
    .finally(() => {
      pendingUsersById.delete(userId);
    });

  pendingUsersById.set(userId, promise);
  return promise;
}

async function fetchChatsByIds(chatIds: string[]) {
  const chats = await Promise.all(chatIds.map((chatId) => fetchChatById(chatId)));

  return chatIds.reduce<Record<string, SaturnChat | SaturnChatListItem>>((byId, chatId, index) => {
    const chat = chats[index];
    if (chat) {
      byId[chatId] = chat;
    }

    return byId;
  }, {});
}

async function fetchChatById(chatId: string) {
  const cached = chatCache.get(chatId);
  if (cached) {
    return cached;
  }

  const pending = pendingChatsById.get(chatId);
  if (pending) {
    return pending;
  }

  const promise = request<SaturnChat>('GET', `/chats/${chatId}`)
    .then(async (chat) => {
      let hydratedChat: SaturnChat | SaturnChatListItem = chat;

      if (chat.type === 'direct' && !chat.name && currentUserId) {
        const members = await request<SaturnPaginatedResponse<SaturnChatMember>>(
          'GET',
          `/chats/${chatId}/members?limit=2`,
        ).catch(() => undefined);
        const otherMember = members?.data.find((member) => member.user_id !== currentUserId);

        if (otherMember?.user_id) {
          const otherUser = userCache.get(otherMember.user_id) || await fetchUserById(otherMember.user_id);
          if (otherUser) {
            hydratedChat = {
              ...chat,
              other_user: otherUser,
            };
          }
        }
      }

      chatCache.set(chatId, hydratedChat);
      return hydratedChat;
    })
    .catch(() => undefined)
    .finally(() => {
      pendingChatsById.delete(chatId);
    });

  pendingChatsById.set(chatId, promise);
  return promise;
}

function emitUsers(users: SaturnUser[]) {
  users.forEach((user) => {
    const apiUser = buildApiUser(user);
    if (currentUserId && user.id === currentUserId) {
      apiUser.isSelf = true;
    }

    sendApiUpdate({
      '@type': 'updateUser',
      id: user.id,
      user: apiUser,
    });

    sendApiUpdate({
      '@type': 'updateUserStatus',
      userId: user.id,
      status: buildApiUserStatus(user),
    });
  });
}

function emitChats(chats: Array<SaturnChat | SaturnChatListItem>) {
  chats.forEach((chat) => {
    if ('other_user' in chat && chat.other_user) {
      emitUsers([chat.other_user]);
    }

    sendApiUpdate({
      '@type': 'updateChat',
      id: chat.id,
      chat: buildApiChat(chat),
      noTopChatsRequest: true,
    });
  });
}

function buildStatusesById(users: SaturnUser[]) {
  return users.reduce<Record<string, ApiUserStatus>>((statusesById, user) => {
    statusesById[user.id] = buildApiUserStatus(user);
    return statusesById;
  }, {});
}

function buildSearchMessage(hit: SaturnMessageSearchHit, sender?: SaturnUser) {
  if (!hit.id || !hit.chat_id || !hit.sequence_number || !hit.created_at_ts) {
    return undefined;
  }

  const message = buildApiMessage({
    id: hit.id,
    chat_id: hit.chat_id,
    sender_id: hit.sender_id || undefined,
    type: normalizeMessageType(hit.type),
    content: hit.content || undefined,
    is_edited: false,
    is_deleted: false,
    is_pinned: false,
    is_forwarded: false,
    sequence_number: hit.sequence_number,
    created_at: new Date(normalizeTimestamp(hit.created_at_ts)).toISOString(),
    sender_name: sender?.display_name || '',
    sender_avatar_url: sender?.avatar_url,
  } satisfies SaturnMessage);

  message.isOutgoing = Boolean(currentUserId && hit.sender_id === currentUserId);
  message.searchSnippet = buildSearchSnippet(hit.content, hit._matchesPosition);

  return message;
}

async function hydrateSearchMessage(hit: SaturnMessageSearchHit) {
  if (!hit.id) {
    return undefined;
  }

  try {
    const fullMessage = await request<SaturnMessage>('GET', `/messages/${hit.id}`);
    const message = buildApiMessage(fullMessage);

    message.isOutgoing = Boolean(currentUserId && fullMessage.sender_id === currentUserId);
    message.searchSnippet = buildSearchSnippet(hit.content || fullMessage.content, hit._matchesPosition);

    return message;
  } catch {
    return undefined;
  }
}

function buildSearchSnippet(content?: string, matchesPosition?: SaturnSearchMatchPosition) {
  const firstMatch = matchesPosition?.content?.[0];
  if (!content || !firstMatch) {
    return undefined;
  }

  const rawStart = Math.max(firstMatch.start - SEARCH_SNIPPET_PADDING, 0);
  const rawEnd = Math.min(
    firstMatch.start + Math.max(firstMatch.length, SEARCH_SNIPPET_MAX_HIGHLIGHT) + SEARCH_SNIPPET_PADDING,
    content.length,
  );
  let snippetStart = rawStart;
  let snippetEnd = rawEnd;

  while (snippetStart < snippetEnd && /\s/.test(content[snippetStart])) {
    snippetStart++;
  }

  while (snippetEnd > snippetStart && /\s/.test(content[snippetEnd - 1])) {
    snippetEnd--;
  }

  const prefix = snippetStart > 0 ? '…' : '';
  const suffix = snippetEnd < content.length ? '…' : '';
  const snippetText = `${prefix}${content.slice(snippetStart, snippetEnd)}${suffix}`;
  const relativeStart = prefix.length + (firstMatch.start - snippetStart);
  const relativeEnd = relativeStart + firstMatch.length;
  const highlight = snippetText.slice(relativeStart, relativeEnd) || undefined;

  return {
    text: snippetText,
    highlight,
  };
}

function normalizeUserHit(hit: SaturnUserSearchHit) {
  if (!hit.id) {
    return undefined;
  }

  const createdAt = hit.created_at || hit.updated_at || new Date(0).toISOString();
  return {
    id: hit.id,
    email: hit.email,
    phone: hit.phone,
    display_name: hit.display_name || hit.email || hit.id,
    avatar_url: hit.avatar_url,
    bio: hit.bio,
    status: hit.status || 'offline',
    custom_status: hit.custom_status,
    custom_status_emoji: hit.custom_status_emoji,
    role: hit.role || 'member',
    is_active: hit.is_active ?? true,
    totp_enabled: hit.totp_enabled,
    invited_by: hit.invited_by,
    last_seen_at: hit.last_seen_at,
    created_at: createdAt,
    updated_at: hit.updated_at || createdAt,
  } satisfies SaturnUser;
}

function normalizeChatHit(hit: SaturnChatSearchHit) {
  if (!hit.id) {
    return undefined;
  }

  const createdAt = hit.created_at || hit.updated_at || new Date(0).toISOString();
  return {
    id: hit.id,
    type: hit.type || 'group',
    name: hit.name,
    description: hit.description,
    avatar_url: hit.avatar_url,
    created_by: hit.created_by,
    is_encrypted: hit.is_encrypted || false,
    max_members: hit.max_members || 0,
    created_at: createdAt,
    updated_at: hit.updated_at || createdAt,
    default_permissions: hit.default_permissions ?? 255,
    slow_mode_seconds: hit.slow_mode_seconds ?? 0,
    is_signatures: hit.is_signatures || false,
  } satisfies SaturnChat;
}

// searchPublicPosts — search messages across public channels (used by type === 'channels' in middle search)
export async function searchPublicPosts({
  hashtag,
  query,
  fromUserId,
  dateFrom,
  dateTo,
  type,
  limit,
  offsetId,
  offsetRate,
  offsetPeer,
}: {
  hashtag?: string;
  query?: string;
  fromUserId?: string;
  dateFrom?: string;
  dateTo?: string;
  type?: string;
  limit?: number;
  offsetId?: number;
  offsetRate?: number;
  offsetPeer?: { id: string; accessHash?: string };
}): Promise<SearchResults | undefined> {
  const searchQuery = hashtag ? `#${hashtag}` : query;
  if (!searchQuery) return undefined;

  return searchMessagesGlobal({
    query: searchQuery,
    fromUserId,
    dateFrom,
    dateTo,
    type,
    limit,
    offsetId,
  });
}

// ─── Search History ────────────────────────────────────────────────────────

export async function fetchSearchHistory(limit = 10) {
  const result = await request<{ history: Array<{ query: string; scope: string; created_at: string }> }>(
    'GET', `/search/history?limit=${limit}`,
  );
  return result?.history || [];
}

export async function saveSearchQuery(query: string, scope = 'global') {
  await request('POST', '/search/history', { query, scope });
}

export async function clearSearchHistory() {
  await request('DELETE', '/search/history');
}
