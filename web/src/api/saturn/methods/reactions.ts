import type {
  ApiChat,
  ApiChatReactions,
  ApiReaction,
  ApiSavedReactionTag,
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
  buildApiReactionUsers,
  buildAvailableEffects,
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
const SAVED_REACTION_TAGS_STORAGE_KEY = 'orbit-saved-reaction-tags';
const SAVED_REACTION_TAGS_HASH_PREFIX = 'orbit-saved-reaction-tags-v1';

type SavedReactionTagStorageRecord = Record<string, {
  reaction: ApiReaction;
  title?: string;
}>;

function getCurrentUserId() {
  return (window as any).getGlobal?.()?.currentUserId as string | undefined;
}

async function loadReactionSummaries(uuid: string) {
  return await client.request<SaturnReactionSummary[]>('GET', `/messages/${uuid}/reactions`) || [];
}

export function fetchAvailableReactions() {
  return Promise.resolve(buildAvailableReactions());
}

export function fetchAvailableEffects() {
  return Promise.resolve({
    effects: buildAvailableEffects(),
    emojis: [],
    stickers: [],
  });
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

export function fetchSavedReactionTags({ hash }: { hash?: string } = {}) {
  const tags = readSavedReactionTags();
  const nextHash = buildSavedReactionTagsHash(tags);

  if (hash === nextHash) {
    return Promise.resolve(undefined);
  }

  return Promise.resolve({
    hash: nextHash,
    tags,
  });
}

export function updateSavedReactionTag({
  reaction,
  title,
}: {
  reaction: ApiReaction;
  title?: string;
}) {
  const tagsByKey = loadSavedReactionTagsStore();
  const key = getSavedReactionTagKey(reaction);
  const normalizedTitle = title?.trim();

  if (normalizedTitle) {
    tagsByKey[key] = {
      reaction,
      title: normalizedTitle,
    };
  } else {
    delete tagsByKey[key];
  }

  saveSavedReactionTagsStore(tagsByKey);

  return Promise.resolve(true);
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
  saturnId,
}: {
  chat: ApiChat;
  messageId: number;
  reactions?: ApiReaction[];
  shouldAddToRecent?: boolean;
  saturnId?: string;
}) {
  const uuid = saturnId || resolveMessageUuid(chat.id, messageId);
  if (!uuid) return undefined;

  // Saturn backend only supports Unicode emoji reactions.
  // Custom emoji reactions are not supported — they should be resolved to emoji upstream.
  const desiredEmojiSet = new Set(
    (reactions || [])
      .reduce<string[]>((acc, reaction) => {
        if (reaction.type === 'emoji') {
          acc.push(reaction.emoticon);
        }
        return acc;
      }, []),
  );

  const summaries = await loadReactionSummaries(uuid) || [];
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
  if (reaction && reaction.type !== 'emoji') {
    return {
      count: 0,
      reactions: [],
      nextOffset: undefined,
      users: [],
    };
  }

  const uuid = resolveMessageUuid(chat.id, messageId);
  if (!uuid) {
    return undefined;
  }

  const params = new URLSearchParams({
    limit: '50',
  });
  if (reaction?.type === 'emoji') {
    params.set('emoji', reaction.emoticon);
  }
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
  const count = reaction?.type === 'emoji'
    ? (summaries.find((summary) => summary.emoji === reaction.emoticon)?.count || page.data.length)
    : (summaries.reduce((total, summary) => total + summary.count, 0) || page.data.length);

  return {
    count,
    reactions: buildApiPeerReactions(page.data, getCurrentUserId()),
    nextOffset: page.cursor,
    users: buildApiReactionUsers(page.data),
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

function readSavedReactionTags() {
  return Object.values(loadSavedReactionTagsStore())
    .map((tag): ApiSavedReactionTag => ({
      reaction: tag.reaction,
      title: tag.title,
      // Saturn does not persist saved tag counts yet, so keep local tags visible with a synthetic count.
      count: 1,
    }))
    .sort((left, right) => getSavedReactionTagKey(left.reaction).localeCompare(getSavedReactionTagKey(right.reaction)));
}

function buildSavedReactionTagsHash(tags: ApiSavedReactionTag[]) {
  const serialized = tags
    .map((tag) => `${getSavedReactionTagKey(tag.reaction)}:${tag.title || ''}`)
    .join('|');

  return `${SAVED_REACTION_TAGS_HASH_PREFIX}:${serialized}`;
}

function loadSavedReactionTagsStore(): SavedReactionTagStorageRecord {
  try {
    const raw = window.localStorage?.getItem(SAVED_REACTION_TAGS_STORAGE_KEY);
    if (!raw) {
      return {};
    }

    const parsed = JSON.parse(raw) as SavedReactionTagStorageRecord;
    if (!parsed || typeof parsed !== 'object') {
      return {};
    }

    return Object.entries(parsed).reduce<SavedReactionTagStorageRecord>((acc, [key, value]) => {
      if (!value || typeof value !== 'object' || !value.reaction) {
        return acc;
      }

      acc[key] = {
        reaction: value.reaction,
        title: typeof value.title === 'string' ? value.title : undefined,
      };

      return acc;
    }, {});
  } catch {
    return {};
  }
}

function saveSavedReactionTagsStore(tagsByKey: SavedReactionTagStorageRecord) {
  const keys = Object.keys(tagsByKey);
  if (!keys.length) {
    window.localStorage?.removeItem(SAVED_REACTION_TAGS_STORAGE_KEY);
    return;
  }

  window.localStorage?.setItem(SAVED_REACTION_TAGS_STORAGE_KEY, JSON.stringify(tagsByKey));
}

function getSavedReactionTagKey(reaction: ApiReaction) {
  switch (reaction.type) {
    case 'emoji':
      return `emoji-${reaction.emoticon}`;
    case 'custom':
      return `custom-${reaction.documentId}`;
    default:
      return 'unsupported';
  }
}
