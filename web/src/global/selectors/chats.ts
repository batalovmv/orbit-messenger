import type { ChatListType } from '../../types';
import type { GlobalState, TabArgs } from '../types';
import {
  type ApiChat, type ApiChatFullInfo, type ApiChatType,
} from '../../api/types';

import {
  ALL_FOLDER_ID, ARCHIVED_FOLDER_ID, MEMBERS_LOAD_SLICE, SAVED_FOLDER_ID, SERVICE_NOTIFICATIONS_USER_ID,
} from '../../config';
import { IS_TRANSLATION_SUPPORTED } from '../../util/browser/windowEnvironment';
import { isUserId } from '../../util/entities/ids';
import { getCurrentTabId } from '../../util/establishMultitabRole';
import {
  getHasAdminRight,
  isChatPublic,
  isChatSuperGroup,
  isHistoryClearMessage,
  isUserBot,
  isUserOnline,
} from '../helpers';
import { selectActiveRestrictionReasons } from './messages';
import { selectTabState } from './tabs';
import {
  selectBot, selectUser, selectUserFullInfo,
} from './users';

export function selectChat<T extends GlobalState>(global: T, chatId: string): ApiChat | undefined {
  return global.chats.byId[chatId];
}

export function selectChatFullInfo<T extends GlobalState>(global: T, chatId: string): ApiChatFullInfo | undefined {
  return global.chats.fullInfoById[chatId];
}

export function selectPeerFullInfo<T extends GlobalState>(global: T, peerId: string) {
  if (isUserId(peerId)) return selectUserFullInfo(global, peerId);
  return selectChatFullInfo(global, peerId);
}

export function selectChatListLoadingParameters<T extends GlobalState>(
  global: T, listType: ChatListType,
) {
  return global.chats.loadingParameters[listType];
}

export function selectIsChatWithSelf<T extends GlobalState>(global: T, chatId: string) {
  if (chatId === global.currentUserId) return true;
  // Saturn: DM chat has a distinct UUID; detect via peerUserId
  const chat = global.chats.byId[chatId];
  return Boolean(chat && chat.type === 'chatTypePrivate' && chat.peerUserId === global.currentUserId);
}

// Saturn: resolve the peer userId for a DM chat. In TG Web A chatId === userId,
// but in Saturn they are different UUIDs. Use this whenever you need the user
// behind a DM chat (instead of selectUser(global, chatId) which always fails).
// IMPORTANT: returns ONLY chat.peerUserId — no fallback through selectUser to avoid
// oscillating return values that cause infinite re-renders in withGlobal.
export function selectDmPeerUserId<T extends GlobalState>(global: T, chatId: string) {
  return global.chats.byId[chatId]?.peerUserId;
}

// Saturn: reverse lookup — find DM chat by the peer's userId.
// In TG Web A selectChat(global, userId) works because chatId === userId for DMs.
// Uses a WeakMap cache keyed by the byId object reference to avoid O(n) scans in withGlobal.
let peerUserIdCacheRef: Record<string, ApiChat> | undefined;
let peerUserIdCache: Map<string, string> | undefined;

export function selectChatByPeerUserId<T extends GlobalState>(global: T, userId: string) {
  // Fast path: if userId is itself a chatId (TG-style)
  const direct = global.chats.byId[userId];
  if (direct) return direct;

  const chatsById = global.chats.byId;

  // Rebuild cache when chats object reference changes
  if (peerUserIdCacheRef !== chatsById) {
    peerUserIdCache = new Map();
    for (const chatId of Object.keys(chatsById)) {
      const chat = chatsById[chatId];
      if (chat.type === 'chatTypePrivate' && chat.peerUserId) {
        peerUserIdCache.set(chat.peerUserId, chatId);
      }
    }
    peerUserIdCacheRef = chatsById;
  }

  const chatId = peerUserIdCache!.get(userId);
  return chatId ? chatsById[chatId] : undefined;
}

export function selectIsChatWithBot<T extends GlobalState>(global: T, chatId: string) {
  const userId = selectDmPeerUserId(global, chatId) || chatId;
  const user = selectUser(global, userId);
  return user && isUserBot(user);
}

export function selectSupportChat<T extends GlobalState>(global: T) {
  return Object.values(global.chats.byId).find(({ isSupport }: ApiChat) => isSupport);
}

export function selectChatOnlineCount<T extends GlobalState>(global: T, chat: ApiChat) {
  const fullInfo = selectChatFullInfo(global, chat.id);
  if (isUserId(chat.id) || !fullInfo) {
    return undefined;
  }

  if (!fullInfo.members || fullInfo.members.length === MEMBERS_LOAD_SLICE) {
    return fullInfo.onlineCount;
  }

  return fullInfo.members.reduce((onlineCount, { userId }) => {
    if (
      !selectIsChatWithSelf(global, userId)
      && global.users.byId[userId]
      && isUserOnline(global.users.byId[userId], global.users.statusesById[userId])
    ) {
      return onlineCount + 1;
    }

    return onlineCount;
  }, 0);
}

export function selectIsTrustedBot<T extends GlobalState>(global: T, botId: string) {
  return global.trustedBotIds.includes(botId) || global.appConfig.whitelistedBotIds?.includes(botId);
}

export function selectChatType<T extends GlobalState>(global: T, chatId: string): ApiChatType | undefined {
  const chat = selectChat(global, chatId);
  if (!chat) return undefined;

  // Saturn: DM chats have type 'chatTypePrivate' with peerUserId
  const peerUserId = chat.peerUserId;
  if (peerUserId) {
    const bot = selectBot(global, peerUserId);
    if (bot) return 'bots';
    return 'users';
  }

  // TG-style fallback: check if chatId is itself a userId
  const user = selectUser(global, chatId);
  if (user) {
    const bot = selectBot(global, chatId);
    return bot ? 'bots' : 'users';
  }

  return 'chats';
}

export function selectIsChatBotNotStarted<T extends GlobalState>(global: T, chatId: string) {
  const peerUserId = global.chats.byId[chatId]?.peerUserId;
  const bot = selectBot(global, peerUserId || chatId);
  if (!bot) {
    return false;
  }

  const lastMessage = selectChatLastMessage(global, chatId);
  if (lastMessage && isHistoryClearMessage(lastMessage)) {
    return true;
  }

  return Boolean(!lastMessage);
}

export function selectAreActiveChatsLoaded<T extends GlobalState>(global: T): boolean {
  return Boolean(global.chats.listIds.active);
}

export function selectIsChatListed<T extends GlobalState>(
  global: T, chatId: string, type?: ChatListType,
): boolean {
  const { listIds } = global.chats;
  if (type) {
    const targetList = listIds[type];
    return Boolean(targetList && targetList.includes(chatId));
  }

  return Object.values(listIds).some((list) => list && list.includes(chatId));
}

export function selectChatListType<T extends GlobalState>(
  global: T, chatId: string,
): 'active' | 'archived' | undefined {
  const chat = selectChat(global, chatId);
  if (!chat || !selectIsChatListed(global, chatId)) {
    return undefined;
  }

  return chat.folderId === ARCHIVED_FOLDER_ID ? 'archived' : 'active';
}

export function selectChatFolder<T extends GlobalState>(global: T, folderId: number) {
  return global.chatFolders.byId[folderId];
}

export function selectTotalChatCount<T extends GlobalState>(global: T, listType: 'active' | 'archived'): number {
  const { totalCount } = global.chats;
  const allChatsCount = totalCount.all;
  const archivedChatsCount = totalCount.archived || 0;

  if (listType === 'archived') {
    return archivedChatsCount;
  }

  return allChatsCount ? allChatsCount - archivedChatsCount : 0;
}

export function selectIsChatPinned<T extends GlobalState>(
  global: T, chatId: string, folderId = ALL_FOLDER_ID,
): boolean {
  const { active, archived, saved } = global.chats.orderedPinnedIds;

  if (folderId === ALL_FOLDER_ID) {
    return Boolean(active?.includes(chatId));
  }

  if (folderId === ARCHIVED_FOLDER_ID) {
    return Boolean(archived?.includes(chatId));
  }

  if (folderId === SAVED_FOLDER_ID) {
    return Boolean(saved?.includes(chatId));
  }

  const { byId: chatFoldersById } = global.chatFolders;

  const { pinnedChatIds } = chatFoldersById[folderId] || {};
  return Boolean(pinnedChatIds?.includes(chatId));
}

// Slow, not to be used in `withGlobal`
export function selectChatByUsername<T extends GlobalState>(global: T, username: string) {
  const usernameLowered = username.toLowerCase();
  return Object.values(global.chats.byId).find(
    (chat) => chat.usernames?.some((c) => c.username.toLowerCase() === usernameLowered),
  );
}

export function selectIsServiceChatReady<T extends GlobalState>(global: T) {
  return Boolean(selectChat(global, SERVICE_NOTIFICATIONS_USER_ID));
}

export function selectSendAs<T extends GlobalState>(global: T, chatId: string) {
  const chat = selectChat(global, chatId);
  if (!chat) return undefined;

  const id = selectChatFullInfo(global, chatId)?.sendAsId;
  if (!id) return undefined;

  return selectUser(global, id) || selectChat(global, id);
}

export function selectRequestedDraft<T extends GlobalState>(
  global: T, chatId: string,
  ...[tabId = getCurrentTabId()]: TabArgs<T>
) {
  const { requestedDraft } = selectTabState(global, tabId);
  if (requestedDraft?.chatId === chatId && !requestedDraft.files?.length) {
    return requestedDraft.text;
  }
  return undefined;
}

export function selectRequestedDraftFiles<T extends GlobalState>(
  global: T, chatId: string,
  ...[tabId = getCurrentTabId()]: TabArgs<T>
) {
  const { requestedDraft } = selectTabState(global, tabId);
  if (requestedDraft?.chatId === chatId) {
    return requestedDraft.files;
  }
  return undefined;
}

export function filterChatIdsByType<T extends GlobalState>(
  global: T, chatIds: string[], filter: readonly ApiChatType[],
) {
  return chatIds.filter((id) => {
    const type = selectChatType(global, id);
    if (!type) {
      return false;
    }
    return filter.includes(type);
  });
}

export function selectCanInviteToChat<T extends GlobalState>(global: T, chatId: string) {
  const chat = selectChat(global, chatId);
  if (!chat) return false;

  // https://github.com/TelegramMessenger/Telegram-iOS/blob/5126be83b3b9578fb014eb52ca553da9e7a8b83a/submodules/TelegramCore/Sources/TelegramEngine/Peers/Communities.swift#L6
  return !chat.migratedTo && Boolean(!isUserId(chatId) && (isChatSuperGroup(chat) ? (
    chat.isCreator || getHasAdminRight(chat, 'inviteUsers')
    || (isChatPublic(chat) && !chat.isJoinRequest)
  ) : (chat.isCreator || getHasAdminRight(chat, 'inviteUsers'))));
}

export function selectCanShareFolder<T extends GlobalState>(global: T, folderId: number) {
  const folder = selectChatFolder(global, folderId);
  if (!folder) return false;

  const {
    bots, groups, contacts, nonContacts, includedChatIds, pinnedChatIds,
    excludeArchived, excludeMuted, excludeRead, excludedChatIds,
  } = folder;

  return !bots && !groups && !contacts && !nonContacts
    && !excludeArchived && !excludeMuted && !excludeRead && !excludedChatIds?.length
    && (pinnedChatIds?.length || includedChatIds.length)
    && folder.includedChatIds.concat(folder.pinnedChatIds || []).some((chatId) => {
      return selectCanInviteToChat(global, chatId);
    });
}

export function selectShouldDetectChatLanguage<T extends GlobalState>(
  global: T, chatId: string,
) {
  const chat = selectChat(global, chatId);
  if (!chat) return false;

  if (chat.hasAutoTranslation) return true;

  const { canTranslateChats } = global.settings.byKey;

  const isSavedMessages = selectIsChatWithSelf(global, chatId);

  // Orbit has no premium tier — gate removed. Translation available to every user.
  return IS_TRANSLATION_SUPPORTED && canTranslateChats && !isSavedMessages;
}

export function selectCanTranslateChat<T extends GlobalState>(
  global: T, chatId: string, ...[tabId = getCurrentTabId()]: TabArgs<T>
) {
  const chat = selectChat(global, chatId);
  if (!chat) return false;

  const requestedTranslation = selectRequestedChatTranslationLanguage(global, chatId, tabId);
  if (requestedTranslation) return true; // Prevent translation dropping on reevaluation

  const { canTranslateChats, doNotTranslate } = global.settings.byKey;

  // Orbit backend doesn't populate chat.detectedLanguage (Telegram MTProto-only
  // field). Expose the translate dropdown whenever the user opts into chat
  // translation so they can pick a target language manually.
  if (canTranslateChats && !selectIsChatWithSelf(global, chatId)) {
    const detectedLanguage = chat.detectedLanguage;
    if (detectedLanguage && doNotTranslate.includes(detectedLanguage)) return false;
    return IS_TRANSLATION_SUPPORTED;
  }

  const isLanguageDetectable = selectShouldDetectChatLanguage(global, chatId);
  const detectedLanguage = chat.detectedLanguage;

  return Boolean(isLanguageDetectable && detectedLanguage && !doNotTranslate.includes(detectedLanguage));
}

export function selectRequestedChatTranslationLanguage<T extends GlobalState>(
  global: T, chatId: string,
  ...[tabId = getCurrentTabId()]: TabArgs<T>
) {
  const { requestedTranslations } = selectTabState(global, tabId);

  return requestedTranslations.byChatId[chatId]?.toLanguage;
}

// Channels not supported — always returns undefined
export function selectSimilarChannelIds<T extends GlobalState>(
  _global: T,
  _chatId: string,
) {
  return undefined;
}

export function selectSimilarBotsIds<T extends GlobalState>(
  global: T,
  chatId: string,
) {
  return global.chats.similarBotsById[chatId];
}

export function selectChatLastMessageId<T extends GlobalState>(
  global: T, chatId: string, listType: 'all' | 'saved' = 'all',
) {
  return global.chats.lastMessageIds[listType]?.[chatId];
}

export function selectChatLastMessage<T extends GlobalState>(
  global: T, chatId: string, listType: 'all' | 'saved' = 'all',
) {
  const id = selectChatLastMessageId(global, chatId, listType);
  if (!id) return undefined;

  const realChatId = listType === 'saved' ? global.currentUserId! : chatId;
  const stored = global.messages.byChatId[realChatId]?.byId[id];
  if (stored?.chatId === realChatId) {
    return stored;
  }

  if (listType === 'all') {
    const chatLastMessage = selectChat(global, chatId)?.lastMessage;
    if (chatLastMessage?.id === id && chatLastMessage.chatId === chatId) {
      return chatLastMessage;
    }
  }

  return stored;
}

export function selectIsMonoforumAdmin<T extends GlobalState>(
  global: T, chatId: string,
) {
  const chat = selectChat(global, chatId);
  if (!chat?.isMonoforum) return;

  const channel = selectMonoforumChannel(global, chatId);
  if (!channel) return;

  return Boolean(chat.isCreator || getHasAdminRight(channel, 'manageDirectMessages'));
}

/**
 * Only selects monoforum channel for monoforum chats.
 * Returns `undefined` for other chats, including channels that have linked monoforum.
 */
export function selectMonoforumChannel<T extends GlobalState>(
  global: T, chatId: string,
) {
  const chat = selectChat(global, chatId);
  if (!chat) return;

  return chat.isMonoforum ? selectChat(global, chat.linkedMonoforumId!) : undefined;
}

export function selectIsChatRestricted<T extends GlobalState>(global: T, chatId: string): boolean {
  const chat = selectChat(global, chatId);
  if (!chat) return false;

  const activeRestrictions = selectActiveRestrictionReasons(global, chat.restrictionReasons);
  return activeRestrictions.length > 0;
}

export function selectAreFoldersPresent<T extends GlobalState>(global: T) {
  const ids = global.chatFolders.orderedIds;
  return Boolean(ids && ids.length > 1);
}
