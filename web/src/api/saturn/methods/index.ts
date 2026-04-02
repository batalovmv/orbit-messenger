import type {
  ApiChat,
  ApiAppConfig,
  ApiConfig,
  ApiFormattedText,
  ApiPremiumPromo,
  ApiSession,
  ApiStarGiftRegular,
  ApiSticker,
  ApiUser,
  ApiWallpaper,
  ApiWebPage,
} from '../../types';
import { DEFAULT_APP_CONFIG } from '../../../limits';

import { getRegisteredAsset } from '../apiBuilders/symbols';
import { ensureAuth, getBaseUrl, request } from '../client';

export {
  destroy, disconnect, init, setCurrentUser,
} from './client';

// Stubs for methods called during TG Web A initialization
export function fetchNearestCountry() {
  return Promise.resolve('US');
}

export function uploadProfilePhoto() {
  return Promise.resolve(undefined);
}

export function requestChannelDifference() {
  return Promise.resolve(undefined);
}

export async function downloadMedia(
  { url, mediaFormat }: { url: string; mediaFormat?: number; isHtmlAllowed?: boolean },
  onProgress?: (progress: number) => void,
) {
  const assetRef = parseAssetRef(url);
  if (!assetRef) return undefined;

  const sizeMatch = url.match(/[?&]size=(\w)/);
  const size = sizeMatch ? sizeMatch[1] : 'y';
  const isPreview = size === 'm' || size === 's' || size === 'a';

  try {
    const token = await ensureAuth();
    const asset = resolveRegisteredAsset(assetRef.kind, assetRef.id, isPreview);

    if (asset) {
      return fetchBinary(asset.url, mediaFormat, token, asset.mimeType, onProgress);
    }

    if (!/^[0-9a-f-]{36}$/i.test(assetRef.id)) {
      return undefined;
    }

    const endpoint = isPreview
      ? `/media/${assetRef.id}/thumbnail`
      : `/media/${assetRef.id}`;

    return fetchBinary(`${getBaseUrl()}${endpoint}`, mediaFormat, token, undefined, onProgress);
  } catch {
    return undefined;
  }
}

export function repairFileReference() {
  return Promise.resolve(undefined);
}

export function abortChatRequests() {
  // No-op
}

export function abortRequestGroup() {
  // No-op
}

export function setForceHttpTransport() {
  // No-op
}

export function setShouldDebugExportedSenders() {
  // No-op
}

export function setAllowHttpTransport() {
  // No-op
}

export function broadcastLocalDbUpdateFull() {
  // No-op
}

// Stubs for commonly called methods in Phase 1
export function fetchConfig(): Promise<ApiConfig | undefined> {
  return Promise.resolve(undefined);
}

export function fetchAppConfig() {
  return Promise.resolve({
    ...DEFAULT_APP_CONFIG,
  } as ApiAppConfig);
}

export function fetchPeerColors() {
  return Promise.resolve(undefined);
}

export function fetchPeerProfileColors() {
  return Promise.resolve(undefined);
}

export function fetchCountryList() {
  return Promise.resolve(undefined);
}

export { fetchContactList } from './users';

export function fetchSponsoredPeer() {
  return Promise.resolve(undefined);
}

export function fetchChatFolders() {
  return Promise.resolve(undefined);
}

export function fetchPinnedDialogs() {
  return Promise.resolve(undefined);
}

export {
  fetchStickerSets,
  fetchFavoriteStickers,
  fetchRecentStickers,
  fetchSavedGifs,
  fetchAnimatedEmojis,
  fetchAnimatedEmojiEffects,
  fetchFeaturedStickers,
  searchStickers,
  fetchStickers,
  installStickerSet,
  uninstallStickerSet,
  addRecentSticker,
  removeRecentSticker,
  clearRecentStickers,
  addFavoriteSticker,
  removeFavoriteSticker,
  fetchCustomEmoji,
  fetchCustomEmojiSets,
  fetchFeaturedEmojiStickers,
  fetchStickersForEmoji,
  fetchGifs,
  searchGifs,
  saveGif,
  removeGif,
} from './symbols';

export function fetchGenericEmojiEffects() {
  return Promise.resolve(undefined);
}

export {
  fetchAvailableReactions,
  fetchMessageReactions,
  fetchMessageReactionsList,
  sendReaction,
  setDefaultReaction,
  setChatEnabledReactions,
} from './reactions';

export function fetchAvailableEffects() {
  return Promise.resolve(undefined);
}

export function fetchTopReactions() {
  return Promise.resolve(undefined);
}

export function fetchRecentReactions() {
  return Promise.resolve(undefined);
}

export function fetchDefaultReactions() {
  return Promise.resolve(undefined);
}

export function fetchDefaultTagReactions() {
  return Promise.resolve(undefined);
}

export { getUserSettings as fetchNotifyDefaultSettings } from './settingsApi';

export function fetchPremiumPromo(): Promise<{ promo: ApiPremiumPromo } | undefined> {
  return Promise.resolve(undefined);
}

export { subscribePush as registerDevice } from './settingsApi';

export function updateIsOnline() {
  return Promise.resolve(undefined);
}

export function fetchSavedReactionTags() {
  return Promise.resolve(undefined);
}

export async function fetchChat({ type, user }: { type?: string; user?: { id: string } } = {}) {
  // When opening a DM with a user, create/get the direct chat
  if (type === 'user' && user) {
    try {
      const { createDirectChat: createDM } = await import('./chats');
      const result = await createDM({ userId: user.id });
      if (result) {
        return { chat: result.chat };
      }
    } catch (e) {
      // ignore
    }
  }
  return undefined;
}

// fetchFullUser — re-exported from ./users

export function fetchUsers() {
  return Promise.resolve(undefined);
}

export function fetchCommonChats() {
  return Promise.resolve(undefined);
}

// fetchMembers re-exported from ./chats

export function saveDraft() {
  return Promise.resolve(undefined);
}

export async function fetchWebPagePreview({ text }: {
  text: ApiFormattedText;
}): Promise<ApiWebPage | undefined> {
  // Extract first URL from text
  const urlMatch = text.text.match(/https?:\/\/[^\s<>]+/i);
  if (!urlMatch) return undefined;

  try {
    const result = await request<{
      preview: {
        url: string;
        display_url: string;
        site_name?: string;
        title?: string;
        description?: string;
        image?: string;
        type?: string;
      } | undefined;
    }>('GET', `/messages/link-preview?url=${encodeURIComponent(urlMatch[0])}`);

    if (!result.preview) return undefined;

    const p = result.preview;
    return {
      mediaType: 'webpage',
      webpageType: 'full',
      id: `lp_${btoa(p.url).slice(0, 16)}`,
      url: p.url,
      displayUrl: p.display_url,
      siteName: p.site_name,
      title: p.title,
      description: p.description,
      type: p.type,
    };
  } catch (e) {
    return undefined;
  }
}

// Saturn privacy key mapping: TG Web A key → Saturn field name
const PRIVACY_KEY_MAP: Record<string, string> = {
  lastSeen: 'last_seen',
  phoneNumber: 'phone',
  profilePhoto: 'avatar',
  forwards: 'forwarded',
  phoneCall: 'calls',
  chatInvite: 'groups',
};

// Saturn uses 'everyone', TG uses 'everybody'
function saturnToTgVisibility(v: string): string {
  return v === 'everyone' ? 'everybody' : v;
}

function tgToSaturnVisibility(v: string): string {
  return v === 'everybody' ? 'everyone' : v;
}

// Build ApiPrivacySettings-compatible response from a visibility value
function buildPrivacyRules(visibility: string) {
  return {
    rules: {
      visibility: saturnToTgVisibility(visibility),
      isUnspecified: false,
      allowUserIds: [],
      allowChatIds: [],
      blockUserIds: [],
      blockChatIds: [],
      botsPrivacy: 'none' as const,
    },
  };
}

// Cached Saturn privacy settings to avoid fetching per-key
let cachedPrivacy: Record<string, string> | undefined;
let cacheTimestamp = 0;
const CACHE_TTL = 5000;

// Clear privacy cache on logout to prevent data leaking between sessions
export function clearPrivacyCache() {
  cachedPrivacy = undefined;
  cacheTimestamp = 0;
}

async function loadSaturnPrivacy(): Promise<Record<string, string> | undefined> {
  if (cachedPrivacy && Date.now() - cacheTimestamp < CACHE_TTL) return cachedPrivacy;
  const { getPrivacySettings } = await import('./settingsApi');
  const result = await getPrivacySettings();
  if (!result) return undefined;
  cachedPrivacy = {
    last_seen: result.last_seen,
    phone: result.phone,
    avatar: result.avatar,
    forwarded: result.forwarded,
    calls: result.calls,
    groups: result.groups,
  };
  cacheTimestamp = Date.now();
  return cachedPrivacy;
}

// TG Web A calls fetchPrivacySettings(privacyKey) per-key
export async function fetchPrivacySettings(privacyKey: string) {
  const saturnField = PRIVACY_KEY_MAP[privacyKey];
  if (!saturnField) {
    // Unsupported key (addByPhone, phoneP2P, voiceMessages, bio, birthday, gifts, noPaidMessages)
    return buildPrivacyRules('everybody');
  }
  const privacy = await loadSaturnPrivacy();
  if (!privacy) {
    // Network failure — return undefined so the caller retries later
    // instead of silently falling back to 'everyone' (most permissive)
    return undefined;
  }
  const value = privacy[saturnField] || 'everyone';
  return buildPrivacyRules(value);
}

// TG Web A calls setPrivacySettings(privacyKey, rules)
export async function setPrivacySettings(privacyKey: string, rules: { visibility: string }) {
  const saturnField = PRIVACY_KEY_MAP[privacyKey];
  if (!saturnField) {
    // Unsupported key — return as-is
    return buildPrivacyRules(rules.visibility);
  }

  // Load current settings, update one field
  const current = await loadSaturnPrivacy();
  const updated = { ...current, [saturnField]: tgToSaturnVisibility(rules.visibility) };

  const { setPrivacySettings: apiSet } = await import('./settingsApi');
  await apiSet({
    lastSeen: updated.last_seen || 'everyone',
    avatar: updated.avatar || 'everyone',
    phone: updated.phone || 'contacts',
    calls: updated.calls || 'everyone',
    groups: updated.groups || 'everyone',
    forwarded: updated.forwarded || 'everyone',
  });

  // Invalidate cache
  cachedPrivacy = undefined;

  return buildPrivacyRules(tgToSaturnVisibility(rules.visibility));
}

export function fetchGlobalPrivacySettings() {
  return Promise.resolve(undefined);
}

export function fetchContentSettings() {
  return Promise.resolve(undefined);
}

export function fetchContactSignUpSetting() {
  return Promise.resolve(undefined);
}

type AuthorizationsResult = { authorizations: Record<string, ApiSession>; ttlDays: number };
type RecentEmojiStatusesResult = { hash: string; emojiStatuses: ApiSticker[] };
type StarGiftsResult = {
  gifts: ApiStarGiftRegular[];
  chats: ApiChat[] | undefined;
  users: ApiUser[] | undefined;
};

export function fetchAuthorizations(): Promise<AuthorizationsResult | undefined> {
  return Promise.resolve(undefined);
}

export function fetchWallpapers(): Promise<{ wallpapers: ApiWallpaper[] } | undefined> {
  return Promise.resolve(undefined);
}

export function fetchPasskeys() {
  return Promise.resolve({ passkeys: [] });
}

// fetchBlockedUsers: adapt Saturn format to TG Web A format
export async function fetchBlockedUsers() {
  const { fetchBlockedUsersList: fetchList } = await import('./settingsApi');
  const result = await fetchList({ limit: 100 });
  if (!result) return undefined;
  const blocked = result.blocked_users || [];
  return {
    blockedIds: blocked.map((u: { blocked_user_id: string }) => u.blocked_user_id),
    totalCount: blocked.length,
  };
}

export function fetchDefaultTopicIcons() {
  return Promise.resolve(undefined);
}

export function fetchEmojiKeywords() {
  return Promise.resolve(undefined);
}

export function fetchTimezones() {
  return Promise.resolve(undefined);
}

export function fetchCollectibleEmojiStatuses() {
  return Promise.resolve(undefined);
}

export function fetchDefaultStatusEmojis() {
  return Promise.resolve(undefined);
}

export function fetchRecentEmojiStatuses(): Promise<RecentEmojiStatusesResult | undefined> {
  return Promise.resolve(undefined);
}

export function fetchStarGifts(): Promise<StarGiftsResult | undefined> {
  return Promise.resolve(undefined);
}

export function fetchDiceStickers() {
  return Promise.resolve(undefined);
}

export {
  checkAuth, loginWithEmail, logout, provideAuthPhoneNumber, provideAuthCode,
  provideAuthPassword, provideAuthRegistration, registerWithInvite,
  restartAuth, restartAuthWithQr, restartAuthWithPasskey, validateInviteCode,
} from './auth';

export {
  fetchCurrentUser, fetchFullUser, fetchGlobalUsers, fetchUser, searchChats, searchUsers, updateProfile,
} from './users';

export {
  createDirectChat, createGroupChat, createChannel, fetchChats, fetchFullChat,
  getChatInviteLink, getChatMembers, editChatTitle, editChatAbout,
  deleteChat, leaveChat, addChatMembers, deleteChatMember,
  updateChatAdmin, updateChatDefaultBannedRights, updateChatMemberBannedRights,
  exportChatInviteLink, fetchExportedChatInvites, editExportedChatInvite,
  deleteExportedChatInvite, fetchChatInviteInfo,
  joinChat, toggleSlowMode, fetchChatInviteImporters, hideChatJoinRequest,
  archiveChat, unarchiveChat, toggleChatPinned, setChatMuted,
  fetchMembers, searchMembers,
} from './chats';

export {
  deleteMessages, editMessage, fetchMessageLink, fetchMessages, fetchMessagesByDate,
  fetchPinnedMessages, forwardMessages, markMessageListRead,
  pinMessage, searchMessagesInChat, sendMessage, sendMessageAction, unpinAllMessages, unpinMessage,
  fetchMessage, sendPollVote, closePoll, loadPollOptionResults, fetchScheduledHistory,
  sendScheduledMessages, editScheduledMessage, deleteScheduledMessages, rescheduleMessage,
} from './messages';

export {
  sendMessage as sendPoll,
  sendPollVote as votePoll,
  loadPollOptionResults as fetchPollVoters,
} from './messages';

export { fetchDifference } from './sync';
export { fetchLangPack, fetchLangStrings, fetchLanguages, oldFetchLangPack } from './settings';

export {
  uploadMedia, initChunkedUpload, uploadChunk, completeChunkedUpload,
  fetchMediaInfo, deleteMedia, fetchSharedMedia,
  updateChatPhoto, deleteChatPhoto,
} from './media';

export {
  getUserSettings, updateUserSettings,
  fetchBlockedUsersList,
  getChatNotifySettings, updateChatNotifySettings, deleteChatNotifySettings,
  subscribePush, unsubscribePush,
} from './settingsApi';

// blockUser/unblockUser adapted for TG Web A action format: { user: ApiUser }
export async function blockUser({ user }: { user: { id: string }; isOnlyStories?: boolean }) {
  const { blockUser: block } = await import('./settingsApi');
  return block({ userId: user.id });
}

export async function unblockUser({ user }: { user: { id: string }; isOnlyStories?: boolean }) {
  const { unblockUser: unblock } = await import('./settingsApi');
  return unblock({ userId: user.id });
}

export {
  searchMessagesGlobal, searchUsersGlobal, searchChatsGlobal,
} from './search';

function parseAssetRef(url: string) {
  const normalizedUrl = url.replace(/^\.\//, '');
  const match = normalizedUrl.match(/(?:progressive\/)?(photo|video|document|sticker|stickerSet)([^/?&#]+)/);
  if (!match) {
    return undefined;
  }

  return {
    kind: match[1],
    id: match[2],
  };
}

function resolveRegisteredAsset(kind: string, id: string, isPreview: boolean) {
  const asset = kind === 'stickerSet'
    ? getRegisteredAsset(id, 'stickerSet')
    : kind === 'sticker'
      ? getRegisteredAsset(id, 'sticker') || getRegisteredAsset(id, 'document')
      : getRegisteredAsset(id, 'document');

  if (!asset) {
    return undefined;
  }

  const url = isPreview
    ? asset.previewUrl || asset.thumbnailDataUri || asset.fullUrl
    : asset.fullUrl || asset.previewUrl || asset.thumbnailDataUri;
  if (!url) {
    return undefined;
  }

  return {
    url,
    mimeType: asset.mimeType,
  };
}

async function fetchBinary(
  url: string,
  mediaFormat?: number,
  token?: string,
  mimeTypeHint?: string,
  onProgress?: (progress: number) => void,
) {
  const headers: Record<string, string> = {};
  if (token && (url.startsWith(getBaseUrl()) || url.startsWith('/'))) {
    headers.Authorization = `Bearer ${token}`;
  }

  const response = await fetch(url, { headers, redirect: 'follow' });
  if (!response.ok) {
    return undefined;
  }

  if (onProgress) {
    onProgress(1);
  }

  const mimeType = response.headers.get('content-type') || mimeTypeHint || 'application/octet-stream';
  if (mediaFormat === 1 /* ApiMediaFormat.Progressive */) {
    const arrayBuffer = await response.arrayBuffer();
    return { arrayBuffer, mimeType, fullSize: arrayBuffer.byteLength };
  }

  const dataBlob = await response.blob();
  return { dataBlob, mimeType };
}
