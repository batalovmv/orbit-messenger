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
import { request } from '../client';

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

export function downloadMedia() {
  return Promise.resolve(undefined);
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

export function fetchNotifyDefaultSettings() {
  return Promise.resolve(undefined);
}

export function fetchPremiumPromo(): Promise<{ promo: ApiPremiumPromo } | undefined> {
  return Promise.resolve(undefined);
}

export function registerDevice() {
  return Promise.resolve(undefined);
}

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

export function fetchMembers() {
  return Promise.resolve(undefined);
}

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

export function fetchPrivacySettings() {
  return Promise.resolve(undefined);
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

export function fetchAuthorizations(): Promise<{ authorizations: Record<string, ApiSession>; ttlDays: number } | undefined> {
  return Promise.resolve(undefined);
}

export function fetchWallpapers(): Promise<{ wallpapers: ApiWallpaper[] } | undefined> {
  return Promise.resolve(undefined);
}

export function fetchBlockedUsers() {
  return Promise.resolve(undefined);
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
  createDirectChat, createGroupChat, fetchChats, fetchFullChat, getChatInviteLink, getChatMembers,
} from './chats';

export {
  deleteMessages, editMessage, fetchMessageLink, fetchMessages, fetchMessagesByDate,
  fetchPinnedMessages, forwardMessages, markMessageListRead,
  pinMessage, sendMessage, sendMessageAction, unpinAllMessages, unpinMessage,
} from './messages';

export { fetchDifference } from './sync';
export { fetchLangPack, fetchLangStrings, fetchLanguages, oldFetchLangPack } from './settings';
