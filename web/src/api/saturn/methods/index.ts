import type {
  ApiAvailableReaction,
  ApiConfig,
  ApiFormattedText,
  ApiPremiumPromo,
  ApiSession,
  ApiStarGiftRegular,
  ApiSticker,
  ApiStickerSet,
  ApiUser,
  ApiUserStatus,
  ApiWallpaper,
  ApiWebPage,
  ApiChat,
} from '../../types';
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
  // url may be a full URL from service worker (http://.../progressive/document<id>)
  // or a relative mediaHash like "document<id>" or "./progressive/document<id>"
  const match = url.match(/(?:photo|video|document)([a-f0-9-]+)/);
  if (!match) return undefined;

  const mediaId = match[1];
  // Determine which variant to fetch based on size parameter
  const sizeMatch = url.match(/[?&]size=(\w)/);
  const size = sizeMatch ? sizeMatch[1] : 'y';

  let endpoint: string;
  if (size === 'm' || size === 's' || size === 'a') {
    endpoint = `/media/${mediaId}/thumbnail`;
  } else {
    endpoint = `/media/${mediaId}`;
  }

  try {
    const token = await ensureAuth();
    const fullUrl = `${getBaseUrl()}${endpoint}`;
    const headers: Record<string, string> = {};
    if (token) {
      headers.Authorization = `Bearer ${token}`;
    }

    const response = await fetch(fullUrl, { headers, redirect: 'follow' });
    if (!response.ok) return undefined;

    if (onProgress) onProgress(1);

    const mimeType = response.headers.get('content-type') || 'application/octet-stream';

    // Progressive format (used by service worker for audio/video streaming):
    // must return arrayBuffer + fullSize for range-request simulation
    if (mediaFormat === 1 /* ApiMediaFormat.Progressive */) {
      const arrayBuffer = await response.arrayBuffer();
      return { arrayBuffer, mimeType, fullSize: arrayBuffer.byteLength };
    }

    const dataBlob = await response.blob();
    return { dataBlob, mimeType };
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
  return Promise.resolve(undefined);
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

export function fetchStickerSets() {
  return Promise.resolve(undefined);
}

export function fetchFavoriteStickers() {
  return Promise.resolve(undefined);
}

export function fetchRecentStickers() {
  return Promise.resolve(undefined);
}

export function fetchSavedGifs() {
  return Promise.resolve(undefined);
}

export function fetchAnimatedEmojis(): Promise<{ set: ApiStickerSet; stickers: ApiSticker[] } | undefined> {
  return Promise.resolve(undefined);
}

export function fetchAnimatedEmojiEffects(): Promise<{ set: ApiStickerSet; stickers: ApiSticker[] } | undefined> {
  return Promise.resolve(undefined);
}

export function fetchGenericEmojiEffects() {
  return Promise.resolve(undefined);
}

export function fetchAvailableReactions(): Promise<ApiAvailableReaction[] | undefined> {
  return Promise.resolve(undefined);
}

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

export function fetchStickers() {
  return Promise.resolve(undefined);
}

export function fetchCustomEmoji() {
  return Promise.resolve(undefined);
}

export function fetchCustomEmojiSets() {
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

export function fetchMessage() {
  return Promise.resolve(undefined);
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

export { getPrivacySettings as fetchPrivacySettings } from './settingsApi';

export function fetchGlobalPrivacySettings() {
  return Promise.resolve(undefined);
}

export function fetchContentSettings() {
  return Promise.resolve(undefined);
}

export function fetchContactSignUpSetting() {
  return Promise.resolve(undefined);
}

export function fetchAuthorizations(): Promise<{ authorizations: Record<string, ApiSession>; ttlDays: number } | undefined> {
  return Promise.resolve(undefined);
}

export function fetchWallpapers(): Promise<{ wallpapers: ApiWallpaper[] } | undefined> {
  return Promise.resolve(undefined);
}

// fetchBlockedUsers: adapt Saturn format to TG Web A format
export async function fetchBlockedUsers() {
  const { fetchBlockedUsersList: fetchList } = await import('./settingsApi');
  const result = await fetchList({ limit: 100 });
  if (!result) return undefined;
  return {
    blockedIds: result.blocked_users.map((u: { blocked_user_id: string }) => u.blocked_user_id),
    totalCount: result.blocked_users.length,
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

export function fetchRecentEmojiStatuses(): Promise<{ hash: string; emojiStatuses: ApiSticker[] } | undefined> {
  return Promise.resolve(undefined);
}

export function fetchStarGifts(): Promise<{ gifts: ApiStarGiftRegular[]; chats: ApiChat[] | undefined; users: ApiUser[] | undefined } | undefined> {
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
} from './messages';

export { fetchDifference } from './sync';
export { fetchLangPack, fetchLangStrings, fetchLanguages, oldFetchLangPack } from './settings';

export {
  uploadMedia, initChunkedUpload, uploadChunk, completeChunkedUpload,
  fetchMediaInfo, deleteMedia, fetchSharedMedia,
  updateChatPhoto, deleteChatPhoto,
} from './media';

export {
  getPrivacySettings, setPrivacySettings,
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
