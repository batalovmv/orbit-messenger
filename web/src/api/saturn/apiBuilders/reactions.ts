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

// ─── Per-emoji TGS imports ───────────────────────────────────────────────────
// Each reaction emoji has up to 6 animation types from different Telegram sticker sets:
//   center   — small looping icon on the reaction counter (EmojiCenterAnimations)
//   around   — burst/explosion effect around the reaction (EmojiAroundAnimations)
//   appear   — bounce-in animation in the picker (EmojiAppearAnimations)
//   select   — short hover-loop in the picker (EmojiShortAnimations)
//   effect   — large particle effect above the message (EmojiAnimations)
//   activate — full-size animation on click (AnimatedEmojies)

// ❤️
import HeartCenter from '../../../assets/tgs/reactions/2764_fe0f_center.tgs';
import HeartAround from '../../../assets/tgs/reactions/2764_fe0f_around.tgs';
import HeartAppear from '../../../assets/tgs/reactions/2764_fe0f_appear.tgs';
import HeartSelect from '../../../assets/tgs/reactions/2764_fe0f_select.tgs';
import HeartEffect from '../../../assets/tgs/reactions/2764_fe0f_effect.tgs';
import HeartActivate from '../../../assets/tgs/reactions/2764_fe0f_activate.tgs';
// 👍
import ThumbsUpCenter from '../../../assets/tgs/reactions/1f44d_center.tgs';
import ThumbsUpAround from '../../../assets/tgs/reactions/1f44d_around.tgs';
import ThumbsUpAppear from '../../../assets/tgs/reactions/1f44d_appear.tgs';
import ThumbsUpSelect from '../../../assets/tgs/reactions/1f44d_select.tgs';
import ThumbsUpEffect from '../../../assets/tgs/reactions/1f44d_effect.tgs';
import ThumbsUpActivate from '../../../assets/tgs/reactions/1f44d_activate.tgs';
// 👎
import ThumbsDownCenter from '../../../assets/tgs/reactions/1f44e_center.tgs';
import ThumbsDownAround from '../../../assets/tgs/reactions/1f44e_around.tgs';
import ThumbsDownAppear from '../../../assets/tgs/reactions/1f44e_appear.tgs';
import ThumbsDownSelect from '../../../assets/tgs/reactions/1f44e_select.tgs';
import ThumbsDownEffect from '../../../assets/tgs/reactions/1f44e_effect.tgs';
import ThumbsDownActivate from '../../../assets/tgs/reactions/1f44e_activate.tgs';
// 🔥
import FireCenter from '../../../assets/tgs/reactions/1f525_center.tgs';
import FireAround from '../../../assets/tgs/reactions/1f525_around.tgs';
import FireAppear from '../../../assets/tgs/reactions/1f525_appear.tgs';
import FireSelect from '../../../assets/tgs/reactions/1f525_select.tgs';
import FireEffect from '../../../assets/tgs/reactions/1f525_effect.tgs';
import FireActivate from '../../../assets/tgs/reactions/1f525_activate.tgs';
// 🥰
import HeartEyesCenter from '../../../assets/tgs/reactions/1f970_center.tgs';
import HeartEyesAround from '../../../assets/tgs/reactions/1f970_around.tgs';
import HeartEyesAppear from '../../../assets/tgs/reactions/1f970_appear.tgs';
import HeartEyesSelect from '../../../assets/tgs/reactions/1f970_select.tgs';
import HeartEyesEffect from '../../../assets/tgs/reactions/1f970_effect.tgs';
import HeartEyesActivate from '../../../assets/tgs/reactions/1f970_activate.tgs';
// 👏
import ClapCenter from '../../../assets/tgs/reactions/1f44f_center.tgs';
import ClapAround from '../../../assets/tgs/reactions/1f44f_around.tgs';
import ClapAppear from '../../../assets/tgs/reactions/1f44f_appear.tgs';
import ClapSelect from '../../../assets/tgs/reactions/1f44f_select.tgs';
import ClapEffect from '../../../assets/tgs/reactions/1f44f_effect.tgs';
import ClapActivate from '../../../assets/tgs/reactions/1f44f_activate.tgs';
// 😁
import GrinCenter from '../../../assets/tgs/reactions/1f601_center.tgs';
import GrinAround from '../../../assets/tgs/reactions/1f601_around.tgs';
import GrinAppear from '../../../assets/tgs/reactions/1f601_appear.tgs';
import GrinSelect from '../../../assets/tgs/reactions/1f601_select.tgs';
import GrinEffect from '../../../assets/tgs/reactions/1f601_effect.tgs';
import GrinActivate from '../../../assets/tgs/reactions/1f601_activate.tgs';
// 🎉
import PartyCenter from '../../../assets/tgs/reactions/1f389_center.tgs';
import PartyAround from '../../../assets/tgs/reactions/1f389_around.tgs';
import PartyAppear from '../../../assets/tgs/reactions/1f389_appear.tgs';
import PartySelect from '../../../assets/tgs/reactions/1f389_select.tgs';
import PartyEffect from '../../../assets/tgs/reactions/1f389_effect.tgs';
import PartyActivate from '../../../assets/tgs/reactions/1f389_activate.tgs';
// 🤔
import ThinkingCenter from '../../../assets/tgs/reactions/1f914_center.tgs';
import ThinkingAround from '../../../assets/tgs/reactions/1f914_around.tgs';
import ThinkingAppear from '../../../assets/tgs/reactions/1f914_appear.tgs';
import ThinkingSelect from '../../../assets/tgs/reactions/1f914_select.tgs';
import ThinkingEffect from '../../../assets/tgs/reactions/1f914_effect.tgs';
import ThinkingActivate from '../../../assets/tgs/reactions/1f914_activate.tgs';
// 😢
import CryCenter from '../../../assets/tgs/reactions/1f622_center.tgs';
import CryAround from '../../../assets/tgs/reactions/1f622_around.tgs';
import CryAppear from '../../../assets/tgs/reactions/1f622_appear.tgs';
import CrySelect from '../../../assets/tgs/reactions/1f622_select.tgs';
import CryActivate from '../../../assets/tgs/reactions/1f622_activate.tgs';
// 😡
import AngryCenter from '../../../assets/tgs/reactions/1f621_center.tgs';
import AngryAround from '../../../assets/tgs/reactions/1f621_around.tgs';
import AngryAppear from '../../../assets/tgs/reactions/1f621_appear.tgs';
import AngrySelect from '../../../assets/tgs/reactions/1f621_select.tgs';
import AngryEffect from '../../../assets/tgs/reactions/1f621_effect.tgs';
import AngryActivate from '../../../assets/tgs/reactions/1f621_activate.tgs';
// 👀
import EyesCenter from '../../../assets/tgs/reactions/1f440_center.tgs';
import EyesAround from '../../../assets/tgs/reactions/1f440_around.tgs';
import EyesAppear from '../../../assets/tgs/reactions/1f440_appear.tgs';
import EyesSelect from '../../../assets/tgs/reactions/1f440_select.tgs';
import EyesEffect from '../../../assets/tgs/reactions/1f440_effect.tgs';
import EyesActivate from '../../../assets/tgs/reactions/1f440_activate.tgs';
// 🤯
import MindBlownCenter from '../../../assets/tgs/reactions/1f92f_center.tgs';
import MindBlownAround from '../../../assets/tgs/reactions/1f92f_around.tgs';
import MindBlownAppear from '../../../assets/tgs/reactions/1f92f_appear.tgs';
import MindBlownSelect from '../../../assets/tgs/reactions/1f92f_select.tgs';
import MindBlownEffect from '../../../assets/tgs/reactions/1f92f_effect.tgs';
import MindBlownActivate from '../../../assets/tgs/reactions/1f92f_activate.tgs';
// 🤝
import HandshakeCenter from '../../../assets/tgs/reactions/1f91d_center.tgs';
import HandshakeAround from '../../../assets/tgs/reactions/1f91d_around.tgs';
import HandshakeAppear from '../../../assets/tgs/reactions/1f91d_appear.tgs';
import HandshakeSelect from '../../../assets/tgs/reactions/1f91d_select.tgs';
import HandshakeEffect from '../../../assets/tgs/reactions/1f91d_effect.tgs';
import HandshakeActivate from '../../../assets/tgs/reactions/1f91d_activate.tgs';
// 🙏
import PrayCenter from '../../../assets/tgs/reactions/1f64f_center.tgs';
import PrayAround from '../../../assets/tgs/reactions/1f64f_around.tgs';
import PrayAppear from '../../../assets/tgs/reactions/1f64f_appear.tgs';
import PraySelect from '../../../assets/tgs/reactions/1f64f_select.tgs';
import PrayEffect from '../../../assets/tgs/reactions/1f64f_effect.tgs';
import PrayActivate from '../../../assets/tgs/reactions/1f64f_activate.tgs';
// 👌
import OkCenter from '../../../assets/tgs/reactions/1f44c_center.tgs';
import OkAround from '../../../assets/tgs/reactions/1f44c_around.tgs';
import OkAppear from '../../../assets/tgs/reactions/1f44c_appear.tgs';
import OkSelect from '../../../assets/tgs/reactions/1f44c_select.tgs';
import OkEffect from '../../../assets/tgs/reactions/1f44c_effect.tgs';
import OkActivate from '../../../assets/tgs/reactions/1f44c_activate.tgs';
// 💯
import HundredCenter from '../../../assets/tgs/reactions/1f4af_center.tgs';
import HundredAround from '../../../assets/tgs/reactions/1f4af_around.tgs';
import HundredAppear from '../../../assets/tgs/reactions/1f4af_appear.tgs';
import HundredSelect from '../../../assets/tgs/reactions/1f4af_select.tgs';
import HundredActivate from '../../../assets/tgs/reactions/1f4af_activate.tgs';
// 🤣
import RoflCenter from '../../../assets/tgs/reactions/1f923_center.tgs';
import RoflAround from '../../../assets/tgs/reactions/1f923_around.tgs';
import RoflAppear from '../../../assets/tgs/reactions/1f923_appear.tgs';
import RoflSelect from '../../../assets/tgs/reactions/1f923_select.tgs';
import RoflEffect from '../../../assets/tgs/reactions/1f923_effect.tgs';
import RoflActivate from '../../../assets/tgs/reactions/1f923_activate.tgs';
// 😎
import CoolCenter from '../../../assets/tgs/reactions/1f60e_center.tgs';
import CoolAround from '../../../assets/tgs/reactions/1f60e_around.tgs';
import CoolAppear from '../../../assets/tgs/reactions/1f60e_appear.tgs';
import CoolSelect from '../../../assets/tgs/reactions/1f60e_select.tgs';
import CoolEffect from '../../../assets/tgs/reactions/1f60e_effect.tgs';
import CoolActivate from '../../../assets/tgs/reactions/1f60e_activate.tgs';
// 🤩
import StarStruckCenter from '../../../assets/tgs/reactions/1f929_center.tgs';
import StarStruckAround from '../../../assets/tgs/reactions/1f929_around.tgs';
import StarStruckAppear from '../../../assets/tgs/reactions/1f929_appear.tgs';
import StarStruckSelect from '../../../assets/tgs/reactions/1f929_select.tgs';
import StarStruckEffect from '../../../assets/tgs/reactions/1f929_effect.tgs';
import StarStruckActivate from '../../../assets/tgs/reactions/1f929_activate.tgs';
// 💔
import BrokenHeartCenter from '../../../assets/tgs/reactions/1f494_center.tgs';
import BrokenHeartAround from '../../../assets/tgs/reactions/1f494_around.tgs';
import BrokenHeartAppear from '../../../assets/tgs/reactions/1f494_appear.tgs';
import BrokenHeartSelect from '../../../assets/tgs/reactions/1f494_select.tgs';
import BrokenHeartEffect from '../../../assets/tgs/reactions/1f494_effect.tgs';
import BrokenHeartActivate from '../../../assets/tgs/reactions/1f494_activate.tgs';
// ✅
import CheckEffect from '../../../assets/tgs/reactions/2705_effect.tgs';
import CheckActivate from '../../../assets/tgs/reactions/2705_activate.tgs';

// ─── Animation asset types ──────────────────────────────────────────────────

type ReactionAnimationAsset = {
  center?: string;
  around?: string;
  appear?: string;
  select?: string;
  effect?: string;
  activate?: string;
};

export const DEFAULT_AVAILABLE_REACTION_EMOJIS = [
  '❤️', '👍', '👎', '🔥', '🥰', '👏', '😁', '🎉',
  '🤔', '😢', '😡', '👀', '🤯', '🤝', '🙏', '👌',
  '💯', '🤣', '😎', '🤩', '💔', '✅',
];

// Real per-emoji Telegram reaction animations — each field points to a different TGS
const REACTION_ANIMATIONS: Record<string, ReactionAnimationAsset> = {
  '❤️': { center: HeartCenter, around: HeartAround, appear: HeartAppear, select: HeartSelect, effect: HeartEffect, activate: HeartActivate },
  '👍': { center: ThumbsUpCenter, around: ThumbsUpAround, appear: ThumbsUpAppear, select: ThumbsUpSelect, effect: ThumbsUpEffect, activate: ThumbsUpActivate },
  '👎': { center: ThumbsDownCenter, around: ThumbsDownAround, appear: ThumbsDownAppear, select: ThumbsDownSelect, effect: ThumbsDownEffect, activate: ThumbsDownActivate },
  '🔥': { center: FireCenter, around: FireAround, appear: FireAppear, select: FireSelect, effect: FireEffect, activate: FireActivate },
  '🥰': { center: HeartEyesCenter, around: HeartEyesAround, appear: HeartEyesAppear, select: HeartEyesSelect, effect: HeartEyesEffect, activate: HeartEyesActivate },
  '👏': { center: ClapCenter, around: ClapAround, appear: ClapAppear, select: ClapSelect, effect: ClapEffect, activate: ClapActivate },
  '😁': { center: GrinCenter, around: GrinAround, appear: GrinAppear, select: GrinSelect, effect: GrinEffect, activate: GrinActivate },
  '🎉': { center: PartyCenter, around: PartyAround, appear: PartyAppear, select: PartySelect, effect: PartyEffect, activate: PartyActivate },
  '🤔': { center: ThinkingCenter, around: ThinkingAround, appear: ThinkingAppear, select: ThinkingSelect, effect: ThinkingEffect, activate: ThinkingActivate },
  '😢': { center: CryCenter, around: CryAround, appear: CryAppear, select: CrySelect, activate: CryActivate },
  '😡': { center: AngryCenter, around: AngryAround, appear: AngryAppear, select: AngrySelect, effect: AngryEffect, activate: AngryActivate },
  '👀': { center: EyesCenter, around: EyesAround, appear: EyesAppear, select: EyesSelect, effect: EyesEffect, activate: EyesActivate },
  '🤯': { center: MindBlownCenter, around: MindBlownAround, appear: MindBlownAppear, select: MindBlownSelect, effect: MindBlownEffect, activate: MindBlownActivate },
  '🤝': { center: HandshakeCenter, around: HandshakeAround, appear: HandshakeAppear, select: HandshakeSelect, effect: HandshakeEffect, activate: HandshakeActivate },
  '🙏': { center: PrayCenter, around: PrayAround, appear: PrayAppear, select: PraySelect, effect: PrayEffect, activate: PrayActivate },
  '👌': { center: OkCenter, around: OkAround, appear: OkAppear, select: OkSelect, effect: OkEffect, activate: OkActivate },
  '💯': { center: HundredCenter, around: HundredAround, appear: HundredAppear, select: HundredSelect, activate: HundredActivate },
  '🤣': { center: RoflCenter, around: RoflAround, appear: RoflAppear, select: RoflSelect, effect: RoflEffect, activate: RoflActivate },
  '😎': { center: CoolCenter, around: CoolAround, appear: CoolAppear, select: CoolSelect, effect: CoolEffect, activate: CoolActivate },
  '🤩': { center: StarStruckCenter, around: StarStruckAround, appear: StarStruckAppear, select: StarStruckSelect, effect: StarStruckEffect, activate: StarStruckActivate },
  '💔': { center: BrokenHeartCenter, around: BrokenHeartAround, appear: BrokenHeartAppear, select: BrokenHeartSelect, effect: BrokenHeartEffect, activate: BrokenHeartActivate },
  '✅': { effect: CheckEffect, activate: CheckActivate },
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

function buildOptionalDoc(
  prefix: string,
  safeId: string,
  emoticon: string,
  url?: string,
): ApiDocument | undefined {
  if (!url) return undefined;
  return buildLocalAnimatedReactionDocument(
    `${prefix}_${safeId}`,
    `${prefix}-${safeId}.tgs`,
    emoticon,
    url,
  );
}

function buildReactionDocuments(emoticon: string) {
  const safeId = getSafeReactionId(emoticon);
  const asset = REACTION_ANIMATIONS[emoticon];
  const staticIcon = buildStaticAssetDocument(`reaction_static_${safeId}`, emoticon, 'reaction');

  const centerIcon = buildOptionalDoc('reaction_center', safeId, emoticon, asset?.center);
  const aroundAnimation = buildOptionalDoc('reaction_around', safeId, emoticon, asset?.around);
  const appearAnimation = buildOptionalDoc('reaction_appear', safeId, emoticon, asset?.appear);
  const selectAnimation = buildOptionalDoc('reaction_select', safeId, emoticon, asset?.select);
  const effectAnimation = buildOptionalDoc('reaction_effect', safeId, emoticon, asset?.effect);
  const activateAnimation = buildOptionalDoc('reaction_activate', safeId, emoticon, asset?.activate);

  return {
    safeId,
    staticIcon,
    centerIcon,
    aroundAnimation,
    appearAnimation,
    selectAnimation,
    effectAnimation,
    activateAnimation,
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
    aroundAnimation,
    appearAnimation,
    selectAnimation,
    effectAnimation,
    activateAnimation,
  } = buildReactionDocuments(emoticon);

  return {
    reaction: buildApiEmojiReaction(emoticon),
    title: emoticon,
    selectAnimation: selectAnimation || centerIcon,
    appearAnimation: appearAnimation || centerIcon,
    activateAnimation: activateAnimation || centerIcon,
    effectAnimation,
    staticIcon,
    centerIcon,
    aroundAnimation,
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
