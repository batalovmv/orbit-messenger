// Phase 4: Search via Meilisearch backend

import type { SaturnSearchResponse } from '../types';
import { request } from '../client';

export async function searchMessagesGlobal({
  query, chatId, fromUserId, dateFrom, dateTo, type, hasMedia, limit, offset,
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
}): Promise<SaturnSearchResponse | undefined> {
  const params = new URLSearchParams();
  params.set('q', query);
  params.set('scope', 'messages');
  if (chatId) params.set('chat_id', chatId);
  if (fromUserId) params.set('from_user_id', fromUserId);
  if (dateFrom) params.set('date_from', dateFrom);
  if (dateTo) params.set('date_to', dateTo);
  if (type) params.set('type', type);
  if (hasMedia !== undefined) params.set('has_media', String(hasMedia));
  if (limit) params.set('limit', String(limit));
  if (offset) params.set('offset', String(offset));

  try {
    return await request<SaturnSearchResponse>('GET', `/search?${params.toString()}`);
  } catch {
    return undefined;
  }
}

export async function searchUsersGlobal({
  query, limit,
}: {
  query: string;
  limit?: number;
}): Promise<SaturnSearchResponse | undefined> {
  const params = new URLSearchParams();
  params.set('q', query);
  params.set('scope', 'users');
  if (limit) params.set('limit', String(limit));

  try {
    return await request<SaturnSearchResponse>('GET', `/search?${params.toString()}`);
  } catch {
    return undefined;
  }
}

export async function searchChatsGlobal({
  query, limit,
}: {
  query: string;
  limit?: number;
}): Promise<SaturnSearchResponse | undefined> {
  const params = new URLSearchParams();
  params.set('q', query);
  params.set('scope', 'chats');
  if (limit) params.set('limit', String(limit));

  try {
    return await request<SaturnSearchResponse>('GET', `/search?${params.toString()}`);
  } catch {
    return undefined;
  }
}
