import type {
  ApiChat,
  ApiChatReactions,
  ApiReaction,
} from '../../types';
import type {
  SaturnPaginatedResponse,
  SaturnReaction,
  SaturnReactionSummary,
} from '../types';

import {
  buildApiEmojiReaction,
  buildApiPeerReactions,
  buildApiReactions,
  buildAvailableReactions,
  DEFAULT_AVAILABLE_REACTION_EMOJIS,
} from '../apiBuilders/reactions';
import * as client from '../client';
import { sendApiUpdate } from '../updates/apiUpdateEmitter';
import { resolveMessageUuid } from './messages';

const DEFAULT_TOP_REACTION_EMOJIS = DEFAULT_AVAILABLE_REACTION_EMOJIS.slice(0, 7);
const DEFAULT_TOP_REACTIONS_HASH = 'orbit-top-reactions-v1';
const DEFAULT_RECENT_REACTIONS_HASH = 'orbit-recent-reactions-v1';
const DEFAULT_DEFAULT_TAG_REACTIONS_HASH = 'orbit-default-tag-reactions-v1';

function getCurrentUserId() {
  return (window as any).getGlobal?.()?.currentUserId as string | undefined;
}

async function loadReactionSummaries(uuid: string) {
  return client.request<SaturnReactionSummary[]>('GET', `/messages/${uuid}/reactions`);
}

export function fetchAvailableReactions() {
  return Promise.resolve(buildAvailableReactions());
}

export function fetchTopReactions({ hash }: { hash?: string } = {}) {
  if (hash === DEFAULT_TOP_REACTIONS_HASH) {
    return Promise.resolve(undefined);
  }

  return Promise.resolve({
    hash: DEFAULT_TOP_REACTIONS_HASH,
    reactions: DEFAULT_TOP_REACTION_EMOJIS.map(buildApiEmojiReaction),
  });
}

export function fetchRecentReactions({ hash }: { hash?: string } = {}) {
  if (hash === DEFAULT_RECENT_REACTIONS_HASH) {
    return Promise.resolve(undefined);
  }

  return Promise.resolve({
    hash: DEFAULT_RECENT_REACTIONS_HASH,
    reactions: DEFAULT_TOP_REACTION_EMOJIS.map(buildApiEmojiReaction),
  });
}

export function fetchDefaultTagReactions({ hash }: { hash?: string } = {}) {
  if (hash === DEFAULT_DEFAULT_TAG_REACTIONS_HASH) {
    return Promise.resolve(undefined);
  }

  return Promise.resolve({
    hash: DEFAULT_DEFAULT_TAG_REACTIONS_HASH,
    reactions: DEFAULT_TOP_REACTION_EMOJIS.map(buildApiEmojiReaction),
  });
}

export async function fetchMessageReactions({
  ids,
  chat,
}: {
  ids: number[];
  chat: ApiChat;
}) {
  const currentUserId = getCurrentUserId();

  await Promise.all(ids.map(async (messageId) => {
    const uuid = resolveMessageUuid(chat.id, messageId);
    if (!uuid) return;

    try {
      const summaries = await loadReactionSummaries(uuid);
      const reactions = buildApiReactions(summaries, currentUserId) || {
        canSeeList: true,
        results: [],
      };

      sendApiUpdate({
        '@type': 'updateMessageReactions',
        chatId: chat.id,
        id: messageId,
        reactions,
      });
    } catch {
      // Ignore per-message reaction refresh failures.
    }
  }));
}

export async function sendReaction({
  chat,
  messageId,
  reactions,
}: {
  chat: ApiChat;
  messageId: number;
  reactions?: ApiReaction[];
  shouldAddToRecent?: boolean;
}) {
  const uuid = resolveMessageUuid(chat.id, messageId);
  if (!uuid) return undefined;

  const desiredEmojiSet = new Set(
    (reactions || [])
      .filter((reaction): reaction is Extract<ApiReaction, { type: 'emoji' }> => reaction.type === 'emoji')
      .map((reaction) => reaction.emoticon),
  );

  const summaries = await loadReactionSummaries(uuid);
  const currentUserId = getCurrentUserId();
  const currentEmojiSet = new Set(
    summaries
      .filter((summary) => currentUserId && summary.user_ids.includes(currentUserId))
      .map((summary) => summary.emoji),
  );

  const removals = [...currentEmojiSet].filter((emoji) => !desiredEmojiSet.has(emoji));
  const additions = [...desiredEmojiSet].filter((emoji) => !currentEmojiSet.has(emoji));

  await Promise.all([
    ...removals.map((emoji) => client.request('DELETE', `/messages/${uuid}/reactions`, { emoji })),
    ...additions.map((emoji) => client.request('POST', `/messages/${uuid}/reactions`, { emoji })),
  ]);

  await fetchMessageReactions({ ids: [messageId], chat });
  return true;
}

export async function fetchMessageReactionsList({
  chat,
  messageId,
  reaction,
  offset,
}: {
  chat: ApiChat;
  messageId: number;
  reaction?: ApiReaction;
  offset?: string;
}) {
  if (!reaction || reaction.type !== 'emoji') {
    return {
      count: 0,
      reactions: [],
      nextOffset: undefined,
    };
  }

  const uuid = resolveMessageUuid(chat.id, messageId);
  if (!uuid) {
    return undefined;
  }

  const params = new URLSearchParams({
    emoji: reaction.emoticon,
    limit: '50',
  });
  if (offset) {
    params.set('cursor', offset);
  }

  const [page, summaries] = await Promise.all([
    client.request<SaturnPaginatedResponse<SaturnReaction>>(
      'GET',
      `/messages/${uuid}/reactions/users?${params.toString()}`,
    ),
    loadReactionSummaries(uuid),
  ]);
  const count = summaries.find((summary) => summary.emoji === reaction.emoticon)?.count || page.data.length;

  return {
    count,
    reactions: buildApiPeerReactions(page.data, getCurrentUserId()),
    nextOffset: page.cursor,
  };
}

export function setDefaultReaction({
  reaction,
}: {
  reaction: ApiReaction;
}) {
  if (reaction.type !== 'emoji') return undefined;
  window.localStorage?.setItem('orbit-default-reaction', reaction.emoticon);
  return Promise.resolve(true);
}

export async function setChatEnabledReactions({
  chat,
  enabledReactions,
  reactionsLimit,
}: {
  chat: ApiChat;
  enabledReactions?: ApiChatReactions;
  reactionsLimit?: number;
}) {
  const body = !enabledReactions ? {
    mode: 'none',
    emojis: [],
  } : enabledReactions.type === 'all' ? {
    mode: 'all',
    emojis: [],
  } : {
    mode: 'selected',
    emojis: enabledReactions.allowed
      .filter((reaction): reaction is Extract<ApiReaction, { type: 'emoji' }> => reaction.type === 'emoji')
      .map((reaction) => reaction.emoticon),
  };

  await client.request('PUT', `/chats/${chat.id}/available-reactions`, body);

  sendApiUpdate({
    '@type': 'updateChatFullInfo',
    id: chat.id,
    fullInfo: {
      enabledReactions,
      reactionsLimit,
    },
  });

  return true;
}
