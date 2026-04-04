import type {
  ApiAvailableEffect,
  ApiAvailableReaction,
  ApiChatReactions,
  ApiDocument,
  ApiPeerReaction,
  ApiReactionCount,
  ApiReactionEmoji,
  ApiReactions,
  ApiUser,
} from '../../types';
import type {
  SaturnChatAvailableReactions,
  SaturnReaction,
  SaturnReactionSummary,
} from '../types';

import { getAvatarPhotoId } from './avatars';
import { buildStaticAssetDocument, registerAsset } from './symbols';

// Per-emoji reaction TGS — real Telegram animations downloaded via Bot API
import HeartCenter from '../../../assets/tgs/reactions/2764_fe0f_center.tgs';
import HeartEffect from '../../../assets/tgs/reactions/2764_fe0f_effect.tgs';
import ThumbsUpCenter from '../../../assets/tgs/reactions/1f44d_center.tgs';
import ThumbsUpEffect from '../../../assets/tgs/reactions/1f44d_effect.tgs';
import ThumbsDownCenter from '../../../assets/tgs/reactions/1f44e_center.tgs';
import ThumbsDownEffect from '../../../assets/tgs/reactions/1f44e_effect.tgs';
import FireCenter from '../../../assets/tgs/reactions/1f525_center.tgs';
import FireEffect from '../../../assets/tgs/reactions/1f525_effect.tgs';
import HeartEyesCenter from '../../../assets/tgs/reactions/1f970_center.tgs';
import HeartEyesEffect from '../../../assets/tgs/reactions/1f970_effect.tgs';
import ClapCenter from '../../../assets/tgs/reactions/1f44f_center.tgs';
import ClapEffect from '../../../assets/tgs/reactions/1f44f_effect.tgs';
import GrinCenter from '../../../assets/tgs/reactions/1f601_center.tgs';
import GrinEffect from '../../../assets/tgs/reactions/1f601_effect.tgs';
import PartyCenter from '../../../assets/tgs/reactions/1f389_center.tgs';
import PartyEffect from '../../../assets/tgs/reactions/1f389_effect.tgs';
import ThinkingCenter from '../../../assets/tgs/reactions/1f914_center.tgs';
import ThinkingEffect from '../../../assets/tgs/reactions/1f914_effect.tgs';
import CryCenter from '../../../assets/tgs/reactions/1f622_center.tgs';
import AngryCenter from '../../../assets/tgs/reactions/1f621_center.tgs';
import AngryEffect from '../../../assets/tgs/reactions/1f621_effect.tgs';
import EyesCenter from '../../../assets/tgs/reactions/1f440_center.tgs';
import EyesEffect from '../../../assets/tgs/reactions/1f440_effect.tgs';
import MindBlownCenter from '../../../assets/tgs/reactions/1f92f_center.tgs';
import MindBlownEffect from '../../../assets/tgs/reactions/1f92f_effect.tgs';
import HandshakeCenter from '../../../assets/tgs/reactions/1f91d_center.tgs';
import HandshakeEffect from '../../../assets/tgs/reactions/1f91d_effect.tgs';
import PrayCenter from '../../../assets/tgs/reactions/1f64f_center.tgs';
import PrayEffect from '../../../assets/tgs/reactions/1f64f_effect.tgs';
import OkCenter from '../../../assets/tgs/reactions/1f44c_center.tgs';
import OkEffect from '../../../assets/tgs/reactions/1f44c_effect.tgs';
import HundredCenter from '../../../assets/tgs/reactions/1f4af_center.tgs';
import RoflCenter from '../../../assets/tgs/reactions/1f923_center.tgs';
import RoflEffect from '../../../assets/tgs/reactions/1f923_effect.tgs';
import CoolCenter from '../../../assets/tgs/reactions/1f60e_center.tgs';
import CoolEffect from '../../../assets/tgs/reactions/1f60e_effect.tgs';
import StarStruckCenter from '../../../assets/tgs/reactions/1f929_center.tgs';
import StarStruckEffect from '../../../assets/tgs/reactions/1f929_effect.tgs';
import BrokenHeartCenter from '../../../assets/tgs/reactions/1f494_center.tgs';
import BrokenHeartEffect from '../../../assets/tgs/reactions/1f494_effect.tgs';
import CheckCenter from '../../../assets/tgs/reactions/2705_center.tgs';
import CheckEffect from '../../../assets/tgs/reactions/2705_effect.tgs';

type ReactionAnimationAsset = { center: string; effect: string };

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

// Real per-emoji Telegram reaction animations (center icon + effect)
const REACTION_ANIMATIONS: Record<string, ReactionAnimationAsset> = {
  '❤️': { center: HeartCenter, effect: HeartEffect },
  '👍': { center: ThumbsUpCenter, effect: ThumbsUpEffect },
  '👎': { center: ThumbsDownCenter, effect: ThumbsDownEffect },
  '🔥': { center: FireCenter, effect: FireEffect },
  '🥰': { center: HeartEyesCenter, effect: HeartEyesEffect },
  '👏': { center: ClapCenter, effect: ClapEffect },
  '😁': { center: GrinCenter, effect: GrinEffect },
  '🎉': { center: PartyCenter, effect: PartyEffect },
  '🤔': { center: ThinkingCenter, effect: ThinkingEffect },
  '😢': { center: CryCenter, effect: CryCenter },
  '😡': { center: AngryCenter, effect: AngryEffect },
  '👀': { center: EyesCenter, effect: EyesEffect },
  '🤯': { center: MindBlownCenter, effect: MindBlownEffect },
  '🤝': { center: HandshakeCenter, effect: HandshakeEffect },
  '🙏': { center: PrayCenter, effect: PrayEffect },
  '👌': { center: OkCenter, effect: OkEffect },
  '💯': { center: HundredCenter, effect: HundredCenter },
  '🤣': { center: RoflCenter, effect: RoflEffect },
  '😎': { center: CoolCenter, effect: CoolEffect },
  '🤩': { center: StarStruckCenter, effect: StarStruckEffect },
  '💔': { center: BrokenHeartCenter, effect: BrokenHeartEffect },
  '✅': { center: CheckCenter, effect: CheckEffect },
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
    '<text x="50%" y="54%" dominant-baseline="middle" text-anchor="middle" font-size="72">'
    + `${escapeXml(emoticon)}</text>`,
    '</svg>',
  ].join('');

  return `data:image/svg+xml;charset=UTF-8,${encodeURIComponent(svg)}`;
}

function getSafeReactionId(emoticon: string) {
  return Array.from(emoticon)
    .map((char) => char.codePointAt(0)?.toString(16) || '')
    .join('_');
}

function getReactionAnimationAsset(emoticon: string): ReactionAnimationAsset | undefined {
  return REACTION_ANIMATIONS[emoticon];
}

function buildLocalAnimatedReactionDocument(
  id: string,
  fileName: string,
  emoticon: string,
  assetUrl: string,
): ApiDocument {
  const thumbnailDataUri = buildReactionThumbnailDataUri(emoticon);

  registerAsset(id, {
    fileName,
    fullUrl: assetUrl,
    mimeType: LOCAL_REACTION_MIME_TYPE,
    previewUrl: assetUrl,
    thumbnailDataUri,
  }, ['document', 'sticker']);

  return {
    mediaType: 'document',
    id,
    fileName,
    mimeType: LOCAL_REACTION_MIME_TYPE,
    size: assetUrl.length,
    thumbnail: {
      dataUri: thumbnailDataUri,
      width: LOCAL_REACTION_ASSET_SIZE,
      height: LOCAL_REACTION_ASSET_SIZE,
    },
  };
}

function buildReactionDocuments(emoticon: string) {
  const safeId = getSafeReactionId(emoticon);
  const asset = getReactionAnimationAsset(emoticon);
  const staticIcon = buildStaticAssetDocument(`reaction_static_${safeId}`, emoticon, 'reaction');
  const centerUrl = asset?.center;
  const effectUrl = asset?.effect || centerUrl;

  const centerIcon = centerUrl
    ? buildLocalAnimatedReactionDocument(
      `reaction_center_${safeId}`,
      `reaction-${safeId}-center.tgs`,
      emoticon,
      centerUrl,
    )
    : undefined;

  const effectAnimation = effectUrl
    ? buildLocalAnimatedReactionDocument(
      `reaction_effect_${safeId}`,
      `reaction-${safeId}-effect.tgs`,
      emoticon,
      effectUrl,
    )
    : undefined;

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

export function buildApiAvailableReaction(emoticon: string): ApiAvailableReaction {
  const {
    staticIcon,
    centerIcon,
    effectAnimation,
  } = buildReactionDocuments(emoticon);

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
  };
}

export function buildAvailableReactions(emojis = DEFAULT_AVAILABLE_REACTION_EMOJIS) {
  return emojis.map((emoji) => buildApiAvailableReaction(emoji));
}

export function buildApiAvailableReactionEffect(emoticon: string): ApiAvailableEffect {
  const {
    safeId,
    staticIcon,
    centerIcon,
    effectAnimation,
  } = buildReactionDocuments(emoticon);

  return {
    id: `orbit_effect_${safeId}`,
    emoticon,
    staticIconId: staticIcon.id,
    effectAnimationId: effectAnimation?.id,
    effectStickerId: centerIcon?.id,
  };
}

export function buildAvailableEffects(emojis = DEFAULT_AVAILABLE_REACTION_EMOJIS) {
  return emojis.map((emoji) => buildApiAvailableReactionEffect(emoji));
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

export function buildApiReactionUsers(reactions: SaturnReaction[]): ApiUser[] {
  const uniqueUsers = new Map<string, ApiUser>();

  reactions.forEach((reaction) => {
    if (uniqueUsers.has(reaction.user_id)) {
      return;
    }

    const displayName = reaction.display_name?.trim() || reaction.user_id;
    const nameParts = displayName.split(/\s+/).filter(Boolean);
    const firstName = nameParts[0] || displayName;
    const lastName = nameParts.slice(1).join(' ') || undefined;

    uniqueUsers.set(reaction.user_id, {
      id: reaction.user_id,
      isMin: true,
      type: 'userTypeRegular',
      firstName,
      lastName,
      phoneNumber: '',
      avatarPhotoId: getAvatarPhotoId(reaction.user_id, reaction.avatar_url),
    });
  });

  return [...uniqueUsers.values()];
}
