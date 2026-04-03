import type {
  ApiAvailableEffect,
  ApiAvailableReaction,
  ApiChatReactions,
  ApiDocument,
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

import BrokenGift from '../../../assets/tgs/BrokenGift.tgs';
import Diamond from '../../../assets/tgs/Diamond.tgs';
import ReadTime from '../../../assets/tgs/ReadTime.tgs';
import Report from '../../../assets/tgs/Report.tgs';
import Search from '../../../assets/tgs/Search.tgs';
import HandFilled from '../../../assets/tgs/calls/HandFilled.tgs';
import HandOutline from '../../../assets/tgs/calls/HandOutline.tgs';
import Flame from '../../../assets/tgs/general/Flame.tgs';
import Fragment from '../../../assets/tgs/general/Fragment.tgs';
import Mention from '../../../assets/tgs/general/Mention.tgs';
import PartyPopper from '../../../assets/tgs/general/PartyPopper.tgs';
import Invite from '../../../assets/tgs/invites/Invite.tgs';
import DuckCake from '../../../assets/tgs/settings/DuckCake.tgs';
import Passkeys from '../../../assets/tgs/settings/Passkeys.tgs';
import StarReaction from '../../../assets/tgs/stars/StarReaction.tgs';
import StarReactionEffect from '../../../assets/tgs/stars/StarReactionEffect.tgs';
import { buildStaticAssetDocument, registerAsset } from './symbols';

const REACTION_ANIMATION_ASSETS = {
  BrokenGift,
  Diamond,
  ReadTime,
  Report,
  Search,
  HandFilled,
  HandOutline,
  Flame,
  Fragment,
  Mention,
  PartyPopper,
  Invite,
  DuckCake,
  Passkeys,
  StarReaction,
  StarReactionEffect,
} as const;

type LocalAnimationKey = keyof typeof REACTION_ANIMATION_ASSETS;
type ReactionAnimationPreset = {
  center: LocalAnimationKey;
  effect?: LocalAnimationKey;
};

export const DEFAULT_AVAILABLE_REACTION_EMOJIS = [
  '❤️',
  '👍',
  '👎',
  '🔥',
  '🥰',
  '👏',
  '😁',
  '🎉',
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

const DEFAULT_CENTER_ANIMATIONS: LocalAnimationKey[] = [
  'StarReaction',
  'PartyPopper',
  'Flame',
  'Mention',
  'Diamond',
  'Invite',
  'ReadTime',
  'DuckCake',
];

const DEFAULT_EFFECT_ANIMATIONS: LocalAnimationKey[] = [
  'StarReactionEffect',
  'PartyPopper',
  'Flame',
  'Fragment',
  'Mention',
];

const REACTION_ANIMATION_PRESETS: Record<string, ReactionAnimationPreset> = {
  '❤️': { center: 'StarReaction', effect: 'StarReactionEffect' },
  '👍': { center: 'HandFilled', effect: 'Mention' },
  '👎': { center: 'HandOutline', effect: 'Fragment' },
  '🔥': { center: 'Flame', effect: 'Flame' },
  '🥰': { center: 'StarReaction', effect: 'StarReactionEffect' },
  '👏': { center: 'HandFilled', effect: 'PartyPopper' },
  '😁': { center: 'DuckCake', effect: 'PartyPopper' },
  '🎉': { center: 'PartyPopper', effect: 'PartyPopper' },
  '🤔': { center: 'Search', effect: 'Search' },
  '😢': { center: 'BrokenGift', effect: 'Fragment' },
  '😡': { center: 'Report', effect: 'Fragment' },
  '👀': { center: 'Search', effect: 'Mention' },
  '🤯': { center: 'Fragment', effect: 'Fragment' },
  '🤝': { center: 'Invite', effect: 'Mention' },
  '🙏': { center: 'HandFilled', effect: 'Mention' },
  '👌': { center: 'HandOutline', effect: 'Mention' },
  '💯': { center: 'Diamond', effect: 'StarReactionEffect' },
  '🤣': { center: 'DuckCake', effect: 'PartyPopper' },
  '😎': { center: 'Passkeys', effect: 'StarReactionEffect' },
  '🤩': { center: 'Diamond', effect: 'StarReactionEffect' },
  '💔': { center: 'BrokenGift', effect: 'Fragment' },
  '✅': { center: 'ReadTime', effect: 'Mention' },
};

const LOCAL_REACTION_ASSET_SIZE = 128;
const LOCAL_REACTION_MIME_TYPE = 'application/x-tgsticker';

function escapeXml(value: string) {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll('\'', '&apos;');
}

function buildReactionThumbnailDataUri(emoticon: string) {
  const svg = [
    '<svg xmlns="http://www.w3.org/2000/svg" width="128" height="128" viewBox="0 0 128 128">',
    '<rect width="128" height="128" rx="32" fill="transparent"/>',
    `<text x="50%" y="54%" dominant-baseline="middle" text-anchor="middle" font-size="72">${escapeXml(emoticon)}</text>`,
    '</svg>',
  ].join('');

  return `data:image/svg+xml;charset=UTF-8,${encodeURIComponent(svg)}`;
}

function getSafeReactionId(emoticon: string) {
  return Array.from(emoticon)
    .map((char) => char.codePointAt(0)?.toString(16) || '')
    .join('_');
}

function getReactionAnimationPreset(emoticon: string, index: number): ReactionAnimationPreset {
  const preset = REACTION_ANIMATION_PRESETS[emoticon];
  if (preset) {
    return preset;
  }

  return {
    center: DEFAULT_CENTER_ANIMATIONS[index % DEFAULT_CENTER_ANIMATIONS.length],
    effect: DEFAULT_EFFECT_ANIMATIONS[index % DEFAULT_EFFECT_ANIMATIONS.length],
  };
}

function buildLocalAnimatedReactionDocument(
  id: string,
  fileName: string,
  emoticon: string,
  assetKey: LocalAnimationKey,
): ApiDocument {
  const url = REACTION_ANIMATION_ASSETS[assetKey];
  const thumbnailDataUri = buildReactionThumbnailDataUri(emoticon);

  registerAsset(id, {
    fileName,
    fullUrl: url,
    mimeType: LOCAL_REACTION_MIME_TYPE,
    previewUrl: url,
    thumbnailDataUri,
  }, ['document', 'sticker']);

  return {
    mediaType: 'document',
    id,
    fileName,
    mimeType: LOCAL_REACTION_MIME_TYPE,
    size: url.length,
    thumbnail: {
      dataUri: thumbnailDataUri,
      width: LOCAL_REACTION_ASSET_SIZE,
      height: LOCAL_REACTION_ASSET_SIZE,
    },
  };
}

function buildReactionDocuments(emoticon: string, index: number) {
  const safeId = getSafeReactionId(emoticon);
  const preset = getReactionAnimationPreset(emoticon, index);
  const staticIcon = buildStaticAssetDocument(`reaction_static_${safeId}`, emoticon, 'reaction');
  const centerIcon = buildLocalAnimatedReactionDocument(
    `reaction_center_${safeId}`,
    `reaction-${safeId}-center.tgs`,
    emoticon,
    preset.center,
  );
  const effectAnimation = buildLocalAnimatedReactionDocument(
    `reaction_effect_${safeId}`,
    `reaction-${safeId}-effect.tgs`,
    emoticon,
    preset.effect || preset.center,
  );

  return {
    safeId,
    staticIcon,
    centerIcon,
    effectAnimation,
  };
}

export function buildApiEmojiReaction(emoticon: string): ApiReactionEmoji {
  return {
    type: 'emoji',
    emoticon,
  };
}

export function buildApiAvailableReaction(emoticon: string, index = 0): ApiAvailableReaction {
  const {
    staticIcon,
    centerIcon,
    effectAnimation,
  } = buildReactionDocuments(emoticon, index);

  return {
    reaction: buildApiEmojiReaction(emoticon),
    title: emoticon,
    selectAnimation: centerIcon,
    appearAnimation: centerIcon,
    activateAnimation: centerIcon,
    effectAnimation,
    staticIcon,
    centerIcon,
    aroundAnimation: effectAnimation,
    isLocalCache: true,
  };
}

export function buildAvailableReactions(emojis = DEFAULT_AVAILABLE_REACTION_EMOJIS) {
  return emojis.map((emoji, index) => buildApiAvailableReaction(emoji, index));
}

export function buildApiAvailableReactionEffect(emoticon: string, index = 0): ApiAvailableEffect {
  const {
    safeId,
    staticIcon,
    centerIcon,
    effectAnimation,
  } = buildReactionDocuments(emoticon, index);

  return {
    id: `orbit_effect_${safeId}`,
    emoticon,
    staticIconId: staticIcon.id,
    effectAnimationId: effectAnimation.id,
    effectStickerId: centerIcon.id!,
  };
}

export function buildAvailableEffects(emojis = DEFAULT_AVAILABLE_REACTION_EMOJIS) {
  return emojis.map((emoji, index) => buildApiAvailableReactionEffect(emoji, index));
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
