// Phase 4: Search via Meilisearch backend

import type { SaturnSearchResponse } from '../types';
import { request } from '../client';
import { DEBUG } from '../../../config';

// The return type expected by globalSearch.ts / middleSearch.ts consumers
interface SearchResults {
  messages: any[];
  topics: any[];
  userStatusesById: Record<string, any>;
  totalCount: number;
  nextOffsetRate?: number;
  nextOffsetPeerId?: string;
  nextOffsetId?: number;
}

export async function searchMessagesGlobal({
  query,
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
  offsetPeer?: any;
  offsetId?: number;
  minDate?: number;
  maxDate?: number;
  context?: string;
}): Promise<SearchResults | undefined> {
  if (!query) return undefined;
  const params = new URLSearchParams();
  params.set('q', query);
  params.set('scope', 'messages');

  try {
    const result = await request<SaturnSearchResponse>('GET', `/search?${params.toString()}`);
    // Meilisearch message results don't contain enough data for full ApiMessage objects
    // (missing sequence_number, entities, media_attachments). Return empty messages with
    // correct totalCount so UI doesn't crash but knows there are results.
    return {
      messages: [],
      topics: [],
      userStatusesById: {},
      totalCount: result?.total || 0,
    };
  } catch (err) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.error('[search] searchMessagesGlobal failed', err);
    }
    return undefined;
  }
}

export async function searchUsersGlobal({
  query, limit,
}: {
  query: string;
  limit?: number;
}): Promise<SaturnSearchResponse | undefined> {
  if (!query) return undefined;
  const params = new URLSearchParams();
  params.set('q', query);
  params.set('scope', 'users');
  if (limit) params.set('limit', String(limit));

  try {
    return await request<SaturnSearchResponse>('GET', `/search?${params.toString()}`);
  } catch (err) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.error('[search] searchUsersGlobal failed', err);
    }
    return undefined;
  }
}

export async function searchChatsGlobal({
  query, limit,
}: {
  query: string;
  limit?: number;
}): Promise<SaturnSearchResponse | undefined> {
  if (!query) return undefined;
  const params = new URLSearchParams();
  params.set('q', query);
  params.set('scope', 'chats');
  if (limit) params.set('limit', String(limit));

  try {
    return await request<SaturnSearchResponse>('GET', `/search?${params.toString()}`);
  } catch (err) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.error('[search] searchChatsGlobal failed', err);
    }
    return undefined;
  }
}
