// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import type {
  ApiAppConfig,
  ApiChat,
  ApiChatFolder,
  ApiConfig,
  ApiDraft,
  ApiFormattedText,
  ApiMessage,
  ApiPeer,
  ApiPremiumPromo,
  ApiPromoData,
  ApiQuickReply,
  ApiSession,
  ApiStarGiftRegular,
  ApiSticker,
  ApiStickerSet,
  ApiUser,
  ApiWallpaper,
  ApiWebPage,
} from '../../types';
import type { SaturnStickerPack } from '../types';

import { DEFAULT_APP_CONFIG } from '../../../limits';
import { buildApiStickerSet, getRegisteredAsset } from '../apiBuilders/symbols';
import { ensureAuth, getBaseUrl, request, sendWsMessage } from '../client';
import { sendApiUpdate } from '../updates/apiUpdateEmitter';
import * as botsApi from './bots';
import {
  editChatAbout,
  editChatTitle,
  exportChatInviteLink,
  fetchChatInviteInfo,
  joinChat,
} from './chats';
import * as integrationsApi from './integrations';
import { fetchMessageLink } from './messages';

export {
  destroy, disconnect, init, setCurrentUser,
} from './client';
export { botsApi, integrationsApi };

// Phase 6: Call methods
export {
  getDhConfig, requestPhoneCall, createPhoneCallState, destroyPhoneCallState,
  acceptPhoneCall, encodePhoneCallData, decodePhoneCallData, sendSignalingData, setCallRating,
  requestCall, acceptCall, discardCall, receivedCall, confirmCall,
  createGroupCall, joinGroupCall, leaveGroupCall, discardGroupCall,
  getGroupCall, fetchGroupCallParticipants, editGroupCallParticipant,
  editGroupCallTitle, exportGroupCallInvite,
  joinGroupCallPresentation, leaveGroupCallPresentation, toggleGroupCallStartSubscription,
} from './calls';

// Stubs for methods called during TG Web A initialization
export function fetchNearestCountry() {
  return Promise.resolve('US');
}

export function fetchPopularAppBots() {
  return botsApi.fetchBots().then((r) => r?.data);
}

export function checkSearchPostsFlood() {
  return Promise.resolve(undefined);
}

export async function downloadMedia(
  { url, mediaFormat, start, end }: {
    url: string; mediaFormat?: number; isHtmlAllowed?: boolean;
    start?: number; end?: number;
  },
  onProgress?: (progress: number) => void,
) {
  const assetRef = parseAssetRef(url);
  if (!assetRef) return undefined;

  const sizeMatch = url.match(/[?&]size=(\w)/);
  const size = sizeMatch ? sizeMatch[1] : 'y';
  const isPreview = size === 'm' || size === 's' || size === 'a';

  try {
    let asset = resolveRegisteredAsset(assetRef.kind, assetRef.id, isPreview);
    const fallbackEndpoint = buildMediaEndpoint(assetRef.id, isPreview);

    if (!asset && assetRef.kind === 'stickerSet') {
      await hydrateStickerSetAsset(assetRef.id);
      asset = resolveRegisteredAsset(assetRef.kind, assetRef.id, isPreview);
    }

    // Local assets (data: URLs, webpack bundles) don't need auth
    if (asset) {
      const isLocalAsset = asset.url.startsWith('data:') || !asset.url.startsWith('http');
      const token = isLocalAsset ? undefined : await ensureAuth();
      const result = await fetchBinary(asset.url, mediaFormat, token, asset.mimeType, onProgress, start, end);
      if (result) {
        return result;
      }
    }

    if (assetRef.kind === 'stickerSet') {
      return undefined;
    }

    if (!fallbackEndpoint) {
      return undefined;
    }

    const token = await ensureAuth();
    return fetchBinary(
      `${getBaseUrl()}${fallbackEndpoint}`, mediaFormat, token, asset?.mimeType, onProgress, start, end,
    );
  } catch {
    return undefined;
  }
}

function decodeDataUrl(url: string) {
  const match = url.match(/^data:([^;,]+)?(?:;charset=[^;,]+)?(;base64)?,(.*)$/);
  if (!match) {
    return undefined;
  }

  const mimeType = match[1] || 'application/octet-stream';
  const isBase64 = Boolean(match[2]);
  const payload = match[3] || '';
  const bytes = isBase64
    ? Uint8Array.from(atob(payload), (char) => char.charCodeAt(0))
    : encodeUtf8(decodeURIComponent(payload));

  return {
    mimeType,
    blob: new Blob([bytes], { type: mimeType }),
  };
}

function encodeUtf8(value: string) {
  const encoded = encodeURIComponent(value);
  const parts = encoded.split(/%([0-9A-F]{2})/i);
  const bytes: number[] = [];

  for (let i = 0; i < parts.length; i++) {
    if (i % 2 === 1) {
      bytes.push(parseInt(parts[i], 16));
    } else {
      for (const char of parts[i]) {
        bytes.push(char.charCodeAt(0));
      }
    }
  }

  return new Uint8Array(bytes);
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

export function fetchPeerSettings(_peer: ApiPeer) {
  return Promise.resolve({
    settings: {},
  });
}

export function fetchCountryList() {
  return Promise.resolve(undefined);
}

export { fetchContactList } from './users';

export function fetchSponsoredPeer() {
  return Promise.resolve(undefined);
}

interface BackendFolder {
  id: number;
  title: string;
  emoticon?: string;
  color?: number;
  position: number;
  included_chat_ids: string[];
  excluded_chat_ids: string[];
  pinned_chat_ids: string[];
}

export async function fetchChatFolders() {
  try {
    const folders = await request<BackendFolder[]>('GET', '/folders');
    const apiFolders: ApiChatFolder[] = folders.map((f: BackendFolder) => ({
      id: f.id,
      title: { text: f.title },
      emoticon: f.emoticon,
      color: f.color,
      includedChatIds: f.included_chat_ids,
      excludedChatIds: f.excluded_chat_ids,
      pinnedChatIds: f.pinned_chat_ids.length ? f.pinned_chat_ids : undefined,
    }));
    return apiFolders;
  } catch {
    return undefined;
  }
}

export function fetchRecommendedChatFolders() {
  return Promise.resolve([] as ApiChatFolder[]);
}

export function toggleDialogFilterTags() {
  return Promise.resolve(true);
}

export async function sortChatFolders(folderIds: number[]) {
  try {
    await request('PUT', '/folders/order', { folder_ids: folderIds });
    sendApiUpdate({
      '@type': 'updateChatFoldersOrder',
      orderedIds: folderIds,
    });
    return true;
  } catch {
    return false;
  }
}

export async function editChatFolder({
  id,
  folderUpdate,
}: {
  id: number;
  folderUpdate: ApiChatFolder;
}) {
  try {
    const body = {
      title: folderUpdate.title.text,
      emoticon: folderUpdate.emoticon ?? '',
      color: folderUpdate.color,
      included_chat_ids: folderUpdate.includedChatIds,
      excluded_chat_ids: folderUpdate.excludedChatIds,
      pinned_chat_ids: folderUpdate.pinnedChatIds ?? [],
    };
    if (id === 0) {
      const created = await request<BackendFolder>('POST', '/folders', body);
      const apiFolder: ApiChatFolder = {
        id: created.id,
        title: { text: created.title },
        emoticon: created.emoticon,
        color: created.color,
        includedChatIds: created.included_chat_ids,
        excludedChatIds: created.excluded_chat_ids,
        pinnedChatIds: created.pinned_chat_ids.length ? created.pinned_chat_ids : undefined,
      };
      sendApiUpdate({
        '@type': 'updateChatFolder',
        id: created.id,
        folder: apiFolder,
      });
    } else {
      const updated = await request<BackendFolder>('PUT', `/folders/${id}`, body);
      const apiFolder: ApiChatFolder = {
        id: updated.id,
        title: { text: updated.title },
        emoticon: updated.emoticon,
        color: updated.color,
        includedChatIds: updated.included_chat_ids,
        excludedChatIds: updated.excluded_chat_ids,
        pinnedChatIds: updated.pinned_chat_ids.length ? updated.pinned_chat_ids : undefined,
      };
      sendApiUpdate({
        '@type': 'updateChatFolder',
        id: updated.id,
        folder: apiFolder,
      });
    }
    return true;
  } catch {
    return false;
  }
}

export async function deleteChatFolder(id: number) {
  try {
    await request('DELETE', `/folders/${id}`);
    sendApiUpdate({
      '@type': 'updateChatFolder',
      id,
      folder: undefined,
    });
    return true;
  } catch {
    return false;
  }
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
  fetchDefaultStatusEmojis,
  fetchRecentEmojiStatuses,
  fetchCollectibleEmojiStatuses,
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
  fetchAvailableEffects,
  fetchAvailableReactions,
  fetchDefaultTagReactions,
  fetchMessageReactions,
  fetchMessageReactionsList,
  fetchRecentReactions,
  sendReaction,
  setDefaultReaction,
  setChatEnabledReactions,
  fetchTopReactions,
  fetchSavedReactionTags,
  updateSavedReactionTag,
} from './reactions';

export function fetchDefaultReactions() {
  return Promise.resolve(undefined);
}

export async function fetchNotifyDefaultSettings() {
  try {
    const result = await request<{
      users_muted: boolean; groups_muted: boolean;
      users_preview: boolean; groups_preview: boolean;
    }>('GET', '/users/me/settings/notifications');

    return {
      users: {
        mutedUntil: result.users_muted ? 2147483647 : 0,
        shouldShowPreviews: result.users_preview,
      },
      groups: {
        mutedUntil: result.groups_muted ? 2147483647 : 0,
        shouldShowPreviews: result.groups_preview,
      },
    };
  } catch {
    return undefined;
  }
}

export async function updateNotificationSettings(
  peerType: string,
  settings: { isMuted?: boolean; shouldShowPreviews?: boolean },
) {
  // Map peerType + settings to backend fields
  const body: Record<string, boolean> = {};
  if (settings.isMuted !== undefined) {
    body[`${peerType}_muted`] = settings.isMuted;
  }
  if (settings.shouldShowPreviews !== undefined) {
    body[`${peerType}_preview`] = settings.shouldShowPreviews;
  }

  try {
    await request('PUT', '/users/me/settings/notifications', body);
  } catch { /* empty */ }
  return true;
}

export function fetchPremiumPromo(): Promise<{ promo: ApiPremiumPromo } | undefined> {
  return Promise.resolve(undefined);
}

export { subscribePush as registerDevice } from './settingsApi';
export { unsubscribePush as unregisterDevice } from './settingsApi';

export function updateIsOnline(isOnline: boolean) {
  sendWsMessage('set_online', { is_online: isOnline });
}

export async function fetchSavedChats() {
  try {
    const chatData = await request<any>('GET', '/users/me/saved-chat');
    if (!chatData?.id) {
      return undefined;
    }
    const { buildApiChat: build } = await import('../apiBuilders/chats');
    const chat = build(chatData);
    sendApiUpdate({ '@type': 'updateChat', id: chat.id, chat });
    return {
      chatIds: [chat.id],
      chats: [chat],
      users: [],
      userStatusesById: {},
      notifyExceptionById: {},
      draftsById: {},
      lastMessageByChatId: {},
      totalChatCount: 1,
      messages: [],
      polls: [],
      threadInfos: [],
      threadReadStatesById: {},
    };
  } catch {
    return undefined;
  }
}

export function requestChatUpdate() {
  return Promise.resolve(undefined);
}

export async function updateChatTitle(chat: ApiChat, title: string) {
  return editChatTitle({ chatId: chat.id, title });
}

export async function updateChatAbout(chat: ApiChat, about: string) {
  return editChatAbout({ chatId: chat.id, about });
}

export async function checkChatInvite(hash: string) {
  return fetchChatInviteInfo({ hash });
}

export async function importChatInvite(hash: string) {
  return joinChat({ hash });
}

export async function exportChatInvite({
  chat, title, expireDate, usageLimit, isRequestNeeded,
}: {
  chat: ApiChat;
  title?: string;
  expireDate?: number;
  usageLimit?: number;
  isRequestNeeded?: boolean;
}) {
  return exportChatInviteLink({
    chatId: chat.id,
    title,
    expireDate,
    usageLimit,
    isRequestNeeded,
  });
}

export function exportMessageLink({
  chat, messageId,
}: {
  chat: ApiChat;
  messageId: number;
}) {
  return fetchMessageLink({ chatId: chat.id, messageId });
}

export async function deleteChatUser({ chat, user }: { chat: ApiChat; user: ApiUser }) {
  return request('DELETE', `/chats/${chat.id}/members/${user.id}`);
}

export function fetchPromoData(): Promise<ApiPromoData | undefined> {
  return Promise.resolve(undefined);
}

export function loadAttachBots() {
  // Stub: attach bots need chat-specific installed bots, not global list
  return Promise.resolve(undefined);
}

export async function fetchNotificationExceptions() {
  try {
    const result = await request<{
      exceptions: Array<{
        user_id: string;
        chat_id: string;
        muted_until?: string;
        sound: string;
        show_preview: boolean;
      }>;
    }>('GET', '/users/me/notification-exceptions');

    if (!result?.exceptions?.length) return undefined;

    const MUTE_INDEFINITE_TIMESTAMP = 2147483647;
    const exceptionsById: Record<string, {
      mutedUntil?: number;
      hasSound?: boolean;
      isSilentPosting?: boolean;
      shouldShowPreviews?: boolean;
    }> = {};

    for (const ex of result.exceptions) {
      const isMuted = ex.muted_until
        ? new Date(ex.muted_until).getTime() > Date.now()
        : false;

      exceptionsById[ex.chat_id] = {
        mutedUntil: isMuted ? MUTE_INDEFINITE_TIMESTAMP : 0,
        hasSound: ex.sound !== 'none',
        shouldShowPreviews: ex.show_preview,
      };
    }

    return exceptionsById;
  } catch {
    return undefined;
  }
}

export function fetchTopInlineBots() {
  return Promise.resolve(undefined);
}

export function fetchTopBotApps() {
  return Promise.resolve(undefined);
}

export function fetchPaidReactionPrivacy() {
  return Promise.resolve(undefined);
}

export function fetchPremiumGifts(): Promise<{
  set: ApiStickerSet;
  stickers: ApiSticker[];
} | undefined> {
  return Promise.resolve(undefined);
}

export function fetchQuickReplies(): Promise<{
  messages: ApiMessage[];
  quickReplies: Record<number, ApiQuickReply>;
} | undefined> {
  return Promise.resolve(undefined);
}

export function fetchTonGifts(): Promise<{
  set: ApiStickerSet;
  stickers: ApiSticker[];
} | undefined> {
  return Promise.resolve(undefined);
}

export async function fetchChat({
  type,
  user,
}: {
  type?: string;
  user?: Pick<ApiUser, 'id' | 'firstName' | 'lastName'>;
} = {}) {
  // Self-chat (Saved Messages): create/get DM with yourself
  if (type === 'self') {
    try {
      const { createDirectChat, getCurrentUserId } = await import('./chats');
      const selfId = getCurrentUserId();
      if (selfId) {
        const result = await createDirectChat({ userId: selfId });
        if (result) {
          return { chat: result.chat };
        }
      }
    } catch (e) {
      // ignore
    }
    return undefined;
  }

  // When opening a DM with a user, create/get the direct chat
  if (type === 'user' && user) {
    try {
      const { createDirectChatWithFallbackUser: createDM } = await import('./chats');
      const result = await createDM({ user });
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

export async function fetchCommonChats({ user }: { user: ApiUser; maxId?: string }) {
  const result = await request<{ chats: any[]; count: number }>(
    'GET', `/users/${user.id}/common-chats?limit=100`,
  );
  if (!result?.chats?.length) return undefined;

  const { buildApiChat: build } = await import('../apiBuilders/chats');
  const chats = result.chats.map(build);
  const chatIds = chats.map((c) => c.id);

  // Add chats to global state so Profile can render them
  for (const chat of chats) {
    sendApiUpdate({ '@type': 'updateChat', id: chat.id, chat });
  }

  return { chatIds, count: result.count };
}

export async function findFirstMessageIdAfterDate({
  chat, timestamp,
}: { chat: ApiChat; timestamp: number }) {
  const { fetchMessagesByDate: fetchByDate } = await import('./messages');
  const isoDate = new Date(timestamp * 1000).toISOString();
  const result = await fetchByDate({ chatId: chat.id, date: isoDate, limit: 1 });
  if (!result?.messages?.length) return undefined;
  return result.messages[0].id;
}

// fetchMembers re-exported from ./chats

export async function saveDraft({
  chat,
  draft,
}: {
  chat: ApiChat;
  draft?: ApiDraft;
}) {
  const text = draft?.text?.text || '';
  await request('PATCH', `/chats/${chat.id}/draft`, { text });
  return true;
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
  return Promise.resolve(false);
}

type AuthorizationsResult = { authorizations: Record<string, ApiSession>; ttlDays: number };
type StarGiftsResult = {
  gifts: ApiStarGiftRegular[];
  chats: ApiChat[] | undefined;
  users: ApiUser[] | undefined;
};

type SaturnAuthSession = {
  id: string;
  ip_address?: string | null;
  user_agent?: string | null;
  created_at?: string;
};

export async function fetchAuthorizations(): Promise<AuthorizationsResult | undefined> {
  try {
    const result = await request<{ sessions?: SaturnAuthSession[] }>('GET', '/auth/sessions');
    const sessions = result.sessions || [];

    const authorizations = sessions.reduce<Record<string, ApiSession>>((acc, session, index) => {
      const parsed = parseSessionUserAgent(session.user_agent);
      const timestamp = session.created_at ? Math.floor(new Date(session.created_at).getTime() / 1000) : 0;

      acc[session.id] = {
        hash: session.id,
        isCurrent: index === 0,
        isOfficialApp: false,
        isPasswordPending: false,
        deviceModel: parsed.deviceModel,
        platform: parsed.platform,
        systemVersion: parsed.systemVersion,
        appName: 'Orbit Web',
        appVersion: parsed.browserVersion,
        dateCreated: timestamp,
        dateActive: timestamp,
        ip: session.ip_address || '',
        country: '',
        region: '',
        areCallsEnabled: true,
        areSecretChatsEnabled: true,
      };

      return acc;
    }, {});

    return {
      authorizations,
      ttlDays: 183,
    };
  } catch {
    return undefined;
  }
}

export function changeSessionTtl() {
  return Promise.resolve(true);
}

export function changeSessionSettings() {
  return Promise.resolve(true);
}

export async function terminateAuthorization(hash: string) {
  try {
    await request('DELETE', `/auth/sessions/${encodeURIComponent(hash)}`);
    return true;
  } catch {
    return false;
  }
}

export async function terminateAllAuthorizations() {
  try {
    const result = await request<{ sessions?: SaturnAuthSession[] }>('GET', '/auth/sessions');
    const sessions = result.sessions || [];

    await Promise.all(
      sessions
        .slice(1)
        .map((session) => request('DELETE', `/auth/sessions/${encodeURIComponent(session.id)}`)),
    );

    return true;
  } catch {
    return false;
  }
}

export function fetchWebAuthorizations() {
  return Promise.resolve({
    webAuthorizations: {},
  });
}

export function terminateWebAuthorization() {
  return Promise.resolve(true);
}

export function terminateAllWebAuthorizations() {
  return Promise.resolve(true);
}

export function fetchAccountTTL() {
  return Promise.resolve({
    days: 365,
  });
}

export function setAccountTTL() {
  return Promise.resolve(true);
}

export function fetchWallpapers(): Promise<{ wallpapers: ApiWallpaper[] } | undefined> {
  return Promise.resolve({
    wallpapers: [],
  });
}

export function uploadWallpaper(file: File): Promise<{ wallpaper: ApiWallpaper }> {
  const url = URL.createObjectURL(file);
  const slug = `local-${Date.now()}`;

  return Promise.resolve({
    wallpaper: {
      slug,
      document: {
        mediaType: 'document',
        id: slug,
        fileName: file.name,
        size: file.size,
        mimeType: file.type,
        blobUrl: url,
        previewBlobUrl: url,
      },
    },
  });
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

export function fetchStarGifts(): Promise<StarGiftsResult | undefined> {
  return Promise.resolve(undefined);
}

export function fetchDiceStickers() {
  return Promise.resolve(undefined);
}

function parseSessionUserAgent(userAgent?: string | null) {
  const source = userAgent || '';
  const browser = detectBrowser(source);
  const os = detectOs(source);

  return {
    deviceModel: browser.name,
    browserVersion: browser.version,
    platform: os.name,
    systemVersion: os.version,
  };
}

function detectBrowser(userAgent: string) {
  const checks: Array<{ pattern: RegExp; name: string }> = [
    { pattern: /Edg\/([\d.]+)/i, name: 'Edge' },
    { pattern: /OPR\/([\d.]+)/i, name: 'Opera' },
    { pattern: /Firefox\/([\d.]+)/i, name: 'Firefox' },
    { pattern: /SamsungBrowser\/([\d.]+)/i, name: 'SamsungBrowser' },
    { pattern: /Chrome\/([\d.]+)/i, name: 'Chrome' },
    { pattern: /Version\/([\d.]+).*Safari/i, name: 'Safari' },
  ];

  for (const { pattern, name } of checks) {
    const match = userAgent.match(pattern);
    if (match) {
      return {
        name,
        version: match[1],
      };
    }
  }

  return {
    name: 'Browser',
    version: '',
  };
}

function detectOs(userAgent: string) {
  const checks: Array<{ pattern: RegExp; name: string; format?: (value: string) => string }> = [
    { pattern: /Windows NT ([\d.]+)/i, name: 'Windows' },
    {
      pattern: /Android ([\d.]+)/i,
      name: 'Android',
    },
    {
      pattern: /(?:CPU (?:iPhone )?OS|iPhone OS) ([\d_]+)/i,
      name: 'iOS',
      format: (value) => value.replace(/_/g, '.'),
    },
    {
      pattern: /Mac OS X ([\d_]+)/i,
      name: 'macOS',
      format: (value) => value.replace(/_/g, '.'),
    },
    { pattern: /Linux/i, name: 'Linux' },
  ];

  for (const { pattern, name, format } of checks) {
    const match = userAgent.match(pattern);
    if (match) {
      return {
        name,
        version: format ? format(match[1] || '') : (match[1] || ''),
      };
    }
  }

  return {
    name: 'Unknown',
    version: '',
  };
}

export {
  checkAuth, createAuthInvite, fetchOIDCConfig, loginWithEmail, logout, provideAuthPhoneNumber, provideAuthCode,
  provideAuthPassword, provideAuthRegistration, registerWithInvite,
  restartAuth, restartAuthWithQr, restartAuthWithPasskey, startOIDCAuthorize, validateInviteCode,
} from './auth';

export { getPasswordInfo, checkPassword, updatePassword, clearPassword } from './twoFaSettings';

export {
  checkUsername,
  fetchCurrentUser,
  fetchFullUser,
  fetchGlobalUsers,
  fetchUser,
  searchChats,
  searchUsers,
  updateProfile,
  updateEmojiStatus,
  updateUsername,
} from './users';

export {
  createDirectChat, createGroupChat, fetchChats, fetchFullChat,
  getChatInviteLink, getChatMembers, editChatTitle, editChatAbout,
  deleteChat, leaveChat, addChatMembers, deleteChatMember,
  updateChatAdmin, updateChatDefaultBannedRights, updateChatMemberBannedRights,
  exportChatInviteLink, fetchExportedChatInvites, editExportedChatInvite,
  deleteExportedChatInvite, fetchChatInviteInfo,
  joinChat, toggleSlowMode, fetchChatInviteImporters, hideChatJoinRequest,
  archiveChat, unarchiveChat, toggleChatPinned, toggleSavedDialogPinned, setChatMuted,
  toggleIsProtected,
  fetchMembers, searchMembers,
} from './chats';

// Phase 8A AI integration — Claude + Whisper
export {
  summarizeChat,
  translateMessages,
  fetchTranslation,
  fetchTranslationsBatch,
  suggestReply,
  transcribeVoice,
  fetchAiUsage,
  semanticSearch,
} from './ai';

export { translateText } from './translateText';
export { toggleAutoTranslation } from './toggleAutoTranslation';

export {
  deleteHistory, deleteMessages, editMessage, fetchMessageLink, fetchMessages, fetchMessagesByDate,
  fetchPinnedMessages, forwardMessages, markMessageListRead, markMessagesRead,
  pinMessage, searchMessagesInChat, sendMessage, sendMessageAction, unpinAllMessages, unpinMessage,
  fetchMessage, sendPollVote, closePoll, loadPollOptionResults, fetchScheduledHistory,
  sendScheduledMessages, editScheduledMessage, deleteScheduledMessages, rescheduleMessage,
  viewOneTimeMessage,
} from './messages';

export {
  sendMessage as sendPoll,
  sendPollVote as votePoll,
  loadPollOptionResults as fetchPollVoters,
} from './messages';

export { fetchDifference } from './sync';
export {
  fetchLangDifference,
  fetchLangPack,
  fetchLangStrings,
  fetchLanguage,
  fetchLanguages,
  oldFetchLangPack,
} from './settings';

export {
  uploadMedia, initChunkedUpload, uploadChunk, completeChunkedUpload, abortChunkedUpload, cancelMediaUpload,
  fetchMediaInfo, deleteMedia, fetchSharedMedia,
  updateChatPhoto, deleteChatPhoto,
} from './media';

export {
  editChatPhoto,
  fetchProfilePhotos,
  toggleChatArchived,
  updateProfilePhoto,
  uploadProfilePhoto,
} from './compat';

export {
  getUserSettings, updateUserSettings,
  fetchBlockedUsersList,
  subscribePush, unsubscribePush,
} from './settingsApi';

// Adapt Saturn settingsApi (expects { chatId }) to TG Web A format (expects { chat, settings })
export async function updateChatNotifySettings({
  chat, settings,
}: {
  chat: { id: string };
  settings: { mutedUntil?: number; shouldShowPreviews?: boolean; isSilentPosting?: boolean };
}) {
  if (!chat?.id) return undefined;

  const { updateChatNotifySettings: update } = await import('./settingsApi');
  return update({
    chatId: chat.id,
    mutedUntil: settings.mutedUntil !== undefined
      ? new Date(settings.mutedUntil * 1000).toISOString()
      : undefined,
    showPreview: settings.shouldShowPreviews,
    silentPosting: settings.isSilentPosting,
    sound: undefined,
  });
}

export async function getChatNotifySettings({ chat }: { chat: { id: string } }) {
  if (!chat?.id) return undefined;

  const { getChatNotifySettings: get } = await import('./settingsApi');
  return get({ chatId: chat.id });
}

export async function deleteChatNotifySettings({ chat }: { chat: { id: string } }) {
  if (!chat?.id) return undefined;

  const { deleteChatNotifySettings: del } = await import('./settingsApi');
  return del({ chatId: chat.id });
}

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
  searchMessagesGlobal, searchMessagesInChatWithFilters, searchUsersGlobal, searchChatsGlobal,
  searchPublicPosts,
} from './search';

function parseAssetRef(url: string) {
  const normalizedUrl = url.replace(/^\.\//, '');
  const match = normalizedUrl.match(
    /(?:progressive\/)?(avatar|profile|photo|video|document|stickerSet|sticker)([^/?&#]+)/,
  );
  if (!match) {
    return undefined;
  }

  return {
    kind: match[1],
    id: match[2],
  };
}

function buildMediaEndpoint(id: string, isPreview: boolean) {
  if (!/^[0-9a-f-]{36}$/i.test(id)) {
    return undefined;
  }

  return isPreview
    ? `/media/${id}/thumbnail`
    : `/media/${id}`;
}

function resolveRegisteredAsset(kind: string, id: string, isPreview: boolean) {
  const asset = kind === 'stickerSet'
    ? getRegisteredAsset(id, 'stickerSet')
    : kind === 'sticker'
      ? getRegisteredAsset(id, 'sticker') || getRegisteredAsset(id, 'document')
      : kind === 'avatar'
        ? getRegisteredAsset(id, 'avatar') || getRegisteredAsset(id, 'profile')
        : kind === 'profile'
          ? getRegisteredAsset(id, 'profile') || getRegisteredAsset(id, 'avatar')
          : kind === 'photo'
            ? getRegisteredAsset(id, 'photo') || getRegisteredAsset(id, 'document')
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

async function hydrateStickerSetAsset(stickerSetId: string) {
  if (!/^[0-9a-f-]{36}$/i.test(stickerSetId)) {
    return;
  }

  try {
    const pack = await request<SaturnStickerPack>('GET', `/stickers/sets/${stickerSetId}`);
    if (!pack) {
      return;
    }

    buildApiStickerSet(pack);
  } catch {
    // Ignore and let the caller fall back gracefully.
  }
}

async function fetchBinary(
  url: string,
  mediaFormat?: number,
  token?: string,
  mimeTypeHint?: string,
  onProgress?: (progress: number) => void,
  rangeStart?: number,
  rangeEnd?: number,
) {
  if (url.startsWith('data:')) {
    const decoded = decodeDataUrl(url);
    if (!decoded) {
      return undefined;
    }

    if (onProgress) {
      onProgress(1);
    }

    const mimeType = decoded.mimeType || mimeTypeHint || 'application/octet-stream';
    if (mediaFormat === 1 /* ApiMediaFormat.Progressive */) {
      const arrayBuffer = await decoded.blob.arrayBuffer();
      return { arrayBuffer, mimeType, fullSize: arrayBuffer.byteLength };
    }

    return { dataBlob: decoded.blob, mimeType };
  }

  const headers: Record<string, string> = {};
  if (token && (url.startsWith(getBaseUrl()) || url.startsWith('/'))) {
    headers.Authorization = `Bearer ${token}`;
  }

  if (rangeStart !== undefined) {
    const rangeEndStr = rangeEnd !== undefined ? String(rangeEnd) : '';
    headers.Range = `bytes=${rangeStart}-${rangeEndStr}`;
  }

  const response = await fetch(url, { headers, redirect: 'follow' });
  if (!response.ok && response.status !== 206) {
    return undefined;
  }

  if (onProgress) {
    onProgress(1);
  }

  const mimeType = response.headers.get('content-type') || mimeTypeHint || 'application/octet-stream';
  if (mediaFormat === 1 /* ApiMediaFormat.Progressive */) {
    const arrayBuffer = await response.arrayBuffer();
    // Derive full file size from Content-Range header (bytes 0-N/TOTAL) or fall back to body length
    let fullSize = arrayBuffer.byteLength;
    const contentRange = response.headers.get('content-range');
    if (contentRange) {
      const totalMatch = contentRange.match(/\/(\d+)/);
      if (totalMatch) {
        fullSize = Number(totalMatch[1]);
      }
    } else if (rangeStart === undefined) {
      // No range request — Content-Length is the full size
      const cl = response.headers.get('content-length');
      if (cl) fullSize = Number(cl);
    }
    return { arrayBuffer, mimeType, fullSize };
  }

  const dataBlob = await response.blob();
  return { dataBlob, mimeType };
}
