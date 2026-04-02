import type {
  ApiAvailableReaction,
  ApiChatReactions,
  ApiPeerReaction,
  ApiReactionCount,
  ApiReactionEmoji,
  ApiReactions,
} from '../../types';
import type {
  SaturnChatAvailableReactions,
  SaturnReaction,
  SaturnReactionSummary,
} from '../types';

import { buildStaticAssetDocument } from './symbols';

export const DEFAULT_AVAILABLE_REACTION_EMOJIS = [
  '👍',
  '❤️',
  '🔥',
  '🎉',
  '👏',
  '😁',
  '🤔',
  '😢',
  '😡',
  '👀',
  '🤯',
  '🤝',
  '🙏',
  '👌',
  '💯',
  '🤣',
  '😎',
  '🤩',
  '💔',
  '✅',
];

export function buildApiEmojiReaction(emoticon: string): ApiReactionEmoji {
  return {
    type: 'emoji',
    emoticon,
  };
}

export function buildApiAvailableReaction(emoticon: string): ApiAvailableReaction {
  const safeId = Array.from(emoticon)
    .map((char) => char.codePointAt(0)?.toString(16) || '')
    .join('_');
  const staticIcon = buildStaticAssetDocument(`reaction_static_${safeId}`, emoticon, 'reaction');

  return {
    reaction: buildApiEmojiReaction(emoticon),
    title: emoticon,
    staticIcon,
  };
}

export function buildAvailableReactions(emojis = DEFAULT_AVAILABLE_REACTION_EMOJIS) {
  return emojis.map(buildApiAvailableReaction);
}

export function buildApiChatReactions(reactions?: SaturnChatAvailableReactions): ApiChatReactions | undefined {
  if (!reactions || reactions.mode === 'none') {
    return undefined;
  }

  if (reactions.mode === 'selected') {
    return {
      type: 'some',
      allowed: (reactions.allowed_emojis || []).map(buildApiEmojiReaction),
    };
  }

  return {
    type: 'all',
    areCustomAllowed: true,
  };
}

export function buildApiReactions(
  summaries?: SaturnReactionSummary[],
  currentUserId?: string,
): ApiReactions | undefined {
  if (!summaries?.length) {
    return undefined;
  }

  const results: ApiReactionCount[] = summaries
    .map((summary, index) => {
      const chosenOrder = currentUserId && summary.user_ids.includes(currentUserId) ? index : undefined;

      return {
        reaction: buildApiEmojiReaction(summary.emoji),
        count: summary.count,
        chosenOrder,
      };
    })
    .sort((left, right) => {
      if (left.count !== right.count) {
        return right.count - left.count;
      }

      if (left.chosenOrder !== undefined && right.chosenOrder === undefined) return -1;
      if (left.chosenOrder === undefined && right.chosenOrder !== undefined) return 1;
      return 0;
    });

  const recentReactions = summaries.flatMap((summary) => (
    summary.user_ids.slice(0, 3).map((userID): ApiPeerReaction => ({
      peerId: userID,
      reaction: buildApiEmojiReaction(summary.emoji),
      isOwn: currentUserId === userID || undefined,
      addedDate: 0,
    }))
  ));

  return {
    canSeeList: true,
    results,
    recentReactions: recentReactions.length ? recentReactions : undefined,
  };
}

export function buildApiPeerReactions(
  reactions: SaturnReaction[],
  currentUserId?: string,
): ApiPeerReaction[] {
  return reactions.map((reaction) => ({
    peerId: reaction.user_id,
    reaction: buildApiEmojiReaction(reaction.emoji),
    isOwn: reaction.user_id === currentUserId || undefined,
    addedDate: Math.floor(new Date(reaction.created_at).getTime() / 1000),
  }));
}
