import { memo, useCallback, useEffect, useMemo, useRef, useState } from '@teact';
import { getActions, getGlobal, withGlobal } from '../../global';

import type {
  ApiBotPreviewMedia,
  ApiChat,
  ApiChatFullInfo,
  ApiChatMember,
  ApiMessage,
  ApiProfileTab,
  ApiStoryAlbum,
  ApiTypeStory,
  ApiUser,
  ApiUserFullInfo,
  ApiUserStatus,
} from '../../api/types';
import type { ProfileCollectionKey } from '../../global/selectors/payments';
import type { TabState } from '../../global/types';
import type { AnimationLevel, ProfileState, ProfileTabType, SharedMediaType, ThemeKey, ThreadId } from '../../types';
import type { RegularLangKey } from '../../types/language';
import { MAIN_THREAD_ID } from '../../api/types';
import { AudioOrigin, MediaViewerOrigin, NewChatMembersProgress } from '../../types';

import { MEMBERS_SLICE, PROFILE_SENSITIVE_AREA, SHARED_MEDIA_SLICE, SLIDE_TRANSITION_DURATION } from '../../config';
import { selectActiveGiftsCollectionId } from '../../global/selectors/payments';

const CONTENT_PANEL_SHOW_DELAY = 300;
import {
  getHasAdminRight,
  getIsDownloading,
  getIsSavedDialog,
  getMessageDocument,
  getMessageHtmlId,
  isChatGroup,
  isUserBot,
  isUserRightBanned,
} from '../../global/helpers';
import {
  selectActiveDownloads,
  selectCanUpdateMainTab,
  selectChat,
  selectChatFullInfo,
  selectChatMessages,
  selectCurrentSharedMediaSearch,
  selectDmPeerUserId,
  selectIsChatRestricted,
  selectIsChatWithSelf,
  selectIsCurrentUserPremium,
  selectIsRightColumnShown,
  selectMonoforumChannel,
  selectPerformanceSettingsValue,
  selectSimilarBotsIds,
  selectTabState,
  selectTheme,
  selectUser,
  selectUserCommonChats,
  selectUserFullInfo,
} from '../../global/selectors';
import { selectPremiumLimit } from '../../global/selectors/limits';
import { selectMessageDownloadableMedia } from '../../global/selectors/media';
import { selectSharedSettings } from '../../global/selectors/sharedState';
// import { selectActiveStoriesCollectionId } from '../../global/selectors/stories'; // stories removed
import {
  VTT_RIGHT_PROFILE_COLLAPSE,
  VTT_RIGHT_PROFILE_EXPAND,
} from '../../util/animations/viewTransitionTypes.ts';
import { IS_TOUCH_ENV } from '../../util/browser/windowEnvironment';
import buildClassName from '../../util/buildClassName';
import { captureEvents, SwipeDirection } from '../../util/captureEvents';
import { isUserId } from '../../util/entities/ids';
import { resolveTransitionName } from '../../util/resolveTransitionName.ts';
import renderText from '../common/helpers/renderText';
import { getSenderName } from '../left/search/helpers/getSenderName';

import { useViewTransition } from '../../hooks/animations/useViewTransition';
import { useVtn } from '../../hooks/animations/useVtn.ts';
import usePeerStoriesPolling from '../../hooks/polling/usePeerStoriesPolling';
import useTopOverscroll from '../../hooks/scroll/useTopOverscroll.tsx';
import useCacheBuster from '../../hooks/useCacheBuster';
import useEffectWithPrevDeps from '../../hooks/useEffectWithPrevDeps';
import useFlag from '../../hooks/useFlag';
import { useIntersectionObserver } from '../../hooks/useIntersectionObserver';
import useLang from '../../hooks/useLang';
import useLastCallback from '../../hooks/useLastCallback';
import useOldLang from '../../hooks/useOldLang';
import useSyncEffectWithPrevDeps from '../../hooks/useSyncEffectWithPrevDeps.ts';
import useAsyncRendering from './hooks/useAsyncRendering';
import useProfileState from './hooks/useProfileState';
import useProfileViewportIds from './hooks/useProfileViewportIds';
import useTransitionFixes from './hooks/useTransitionFixes';

import Audio from '../common/Audio';
import Document from '../common/Document';
import GroupChatInfo from '../common/GroupChatInfo';
import Media from '../common/Media';
import NothingFound from '../common/NothingFound';
import PreviewMedia from '../common/PreviewMedia';
import PrivateChatInfo from '../common/PrivateChatInfo';
import ChatExtra from '../common/profile/ChatExtra';
import ProfileInfo from '../common/profile/ProfileInfo.tsx';
import WebLink from '../common/WebLink';
import ChatList from '../left/main/ChatList';
// import MediaStory from '../story/MediaStory'; // stories removed
import Button from '../ui/Button';
import FloatingActionButton from '../ui/FloatingActionButton';
import InfiniteScroll from '../ui/InfiniteScroll';
import ListItem, { type MenuItemContextAction } from '../ui/ListItem';
import Spinner from '../ui/Spinner';
import TabList, { type TabWithProperties } from '../ui/TabList';
import Transition from '../ui/Transition';
import DeleteMemberModal from './DeleteMemberModal';

// import StoryAlbumList from './stories/StoryAlbumList'; // stories removed
import './Profile.scss';

type OwnProps = {
  chatId: string;
  threadId?: ThreadId;
  profileState: ProfileState;
  isMobile?: boolean;
  isActive: boolean;
  onProfileStateChange: (state: ProfileState) => void;
};

type StateProps = {
  monoforumChannel?: ApiChat;
  theme: ThemeKey;
  isBot?: boolean;
  currentUserId?: string;
  messagesById?: Record<number, ApiMessage>;
  foundIds?: number[];
  mediaSearchType?: SharedMediaType;
  hasCommonChatsTab?: boolean;
  hasStoriesTab?: boolean;
  hasMembersTab?: boolean;
  hasPreviewMediaTab?: boolean;
  hasGiftsTab?: boolean;
  storyAlbums?: ApiStoryAlbum[];
  areMembersHidden?: boolean;
  canAddMembers?: boolean;
  canDeleteMembers?: boolean;
  members?: ApiChatMember[];
  adminMembersById?: Record<string, ApiChatMember>;
  commonChatIds?: string[];
  storyIds?: number[];
  pinnedStoryIds?: number[];
  archiveStoryIds?: number[];
  storyByIds?: Record<number, ApiTypeStory>;
  selectedStoryAlbumId: ProfileCollectionKey;
  activeCollectionId: ProfileCollectionKey;
  chatsById: Record<string, ApiChat>;
  usersById: Record<string, ApiUser>;
  userStatusesById: Record<string, ApiUserStatus>;
  isRightColumnShown: boolean;
  isRestricted?: boolean;
  activeDownloads: TabState['activeDownloads'];
  isChatProtected?: boolean;
  chatInfo: TabState['chatInfo'];
  animationLevel: AnimationLevel;
  shouldWarnAboutFiles?: boolean;
  similarBots?: string[];
  botPreviewMedia?: ApiBotPreviewMedia[];
  isCurrentUserPremium?: boolean;
  limitSimilarPeers: number;
  isTopicInfo?: boolean;
  isSavedDialog?: boolean;
  isSavedMessages?: boolean;
  isSynced?: boolean;
  hasAvatar?: boolean;
  peerFullInfo?: ApiUserFullInfo | ApiChatFullInfo;
  canUpdateMainTab?: boolean;
  canAutoPlayGifs?: boolean;
};

type LocalTabProps = {
  type: ProfileTabType;
  key: RegularLangKey;
};

type TabWithPropertiesAndType = TabWithProperties & {
  type: ProfileTabType;
};

const TABS: LocalTabProps[] = [
  { type: 'media', key: 'ProfileTabMedia' },
  { type: 'documents', key: 'ProfileTabFiles' },
  { type: 'links', key: 'ProfileTabLinks' },
  { type: 'audio', key: 'ProfileTabMusic' },
  { type: 'gif', key: 'ProfileTabGifs' },
];

const HIDDEN_RENDER_DELAY = 1000;
const INTERSECTION_THROTTLE = 500;

const VALID_CHANNEL_MAIN_TAB_TYPES = new Set<StringAutocomplete<ApiProfileTab>>([
  'stories', 'gifts', 'media', 'documents', 'audio', 'voice', 'links', 'gif',
]);
const VALID_USER_MAIN_TAB_TYPES = new Set<StringAutocomplete<ApiProfileTab>>([
  'stories', 'gifts',
]);
const SHARED_MEDIA_TYPES = new Set<StringAutocomplete<SharedMediaType>>([
  'media', 'documents', 'links', 'audio', 'voice', 'gif',
]);

const Profile = ({
  chatId,
  isActive,
  threadId,
  chatInfo,
  profileState,
  theme,
  monoforumChannel,
  isBot,
  currentUserId,
  messagesById,
  foundIds,
  storyIds,
  pinnedStoryIds,
  archiveStoryIds,
  storyByIds,
  selectedStoryAlbumId,
  activeCollectionId,
  mediaSearchType,
  hasCommonChatsTab,
  hasStoriesTab,
  hasMembersTab,
  hasPreviewMediaTab,
  hasGiftsTab,
  storyAlbums,
  botPreviewMedia,
  areMembersHidden,
  canAddMembers,
  canDeleteMembers,
  commonChatIds,
  members,
  adminMembersById,
  usersById,
  userStatusesById,
  chatsById,
  isRightColumnShown,
  isRestricted,
  activeDownloads,
  isChatProtected,
  animationLevel,
  shouldWarnAboutFiles,
  similarBots,
  isCurrentUserPremium,
  limitSimilarPeers,
  isTopicInfo,
  isSavedDialog,
  isSavedMessages,
  isSynced,
  hasAvatar,
  peerFullInfo,
  canUpdateMainTab,
  canAutoPlayGifs,
  onProfileStateChange,
}: OwnProps & StateProps) => {
  const {
    setSharedMediaSearchType,
    loadMoreMembers,
    loadCommonChats,
    openChat,
    searchSharedMediaMessages,
    openMediaViewer,
    openAudioPlayer,
    focusMessage,
    setNewChatMembersDialogState,
    openPremiumModal,
    loadBotRecommendations,
    loadPreviewMedias,
    loadPeerSavedGifts,
    changeProfileTab,
    setMainProfileTab,
  } = getActions();

  const containerRef = useRef<HTMLDivElement>();
  const transitionRef = useRef<HTMLDivElement>();

  const shouldSkipTransitionRef = useRef(false);

  const oldLang = useOldLang();
  const lang = useLang();

  const [deletingUserId, setDeletingUserId] = useState<string | undefined>();

  const isClosed = !chatInfo.isOpen;
  const { profileTab, forceScrollProfileTab, isOwnProfile } = chatInfo;

  const profileId = isSavedDialog ? String(threadId) : chatId;
  const isGeneralSavedMessages = isSavedMessages && !isSavedDialog;
  const [isProfileExpanded, expandProfile, collapseProfile] = useFlag();

  const [restoreContentHeightKey, setRestoreContentHeightKey] = useState(0);

  const isUser = isUserId(chatId);
  const validMainTabTypes = isUser ? VALID_USER_MAIN_TAB_TYPES : VALID_CHANNEL_MAIN_TAB_TYPES;
  const mainTab = peerFullInfo?.mainTab;

  const tabs = useMemo(() => {
    const arr: LocalTabProps[] = [];
    if (isGeneralSavedMessages) {
      arr.push({ type: 'dialogs', key: 'ProfileTabSavedDialogs' });
    }

    if (hasStoriesTab) {
      arr.push({ type: 'stories', key: 'ProfileTabStories' });
    }

    if (hasGiftsTab) {
      arr.push({ type: 'gifts', key: 'ProfileTabGifts' });
    }

    if (hasStoriesTab && isOwnProfile) {
      arr.push({ type: 'storiesArchive', key: 'ProfileTabStoriesArchive' });
    }

    if (hasMembersTab && !isOwnProfile) {
      arr.push({ type: 'members', key: 'ProfileTabMembers' });
    }

    if (hasPreviewMediaTab && !isOwnProfile) {
      arr.push({ type: 'previewMedia', key: 'ProfileTabBotPreview' });
    }

    if (!isOwnProfile) {
      arr.push(...TABS);
    }

    // Voice messages filter currently does not work in forum topics. Return it when it's fixed on the server side.
    if (!isTopicInfo && !isOwnProfile) {
      arr.push({ type: 'voice', key: 'ProfileTabVoice' });
    }

    if (hasCommonChatsTab && !isOwnProfile) {
      arr.push({ type: 'commonChats', key: 'ProfileTabSharedGroups' });
    }

    if (isBot && similarBots?.length && !isOwnProfile) {
      arr.push({ type: 'similarBots', key: 'ProfileTabSimilarBots' });
    }

    // Fallback to prevent errors in edge cases
    // TODO: Handle no tabs case, skip shared media block
    if (!arr.length) {
      arr.push(TABS[0]);
    }

    if (mainTab) {
      const mainTabIndex = arr.findIndex((tab) => tab.type === mainTab);
      if (mainTabIndex !== -1) {
        const newFirstTab = arr[mainTabIndex];
        arr.splice(mainTabIndex, 1);
        arr.unshift(newFirstTab);
      }
    }

    return arr.map((tab) => {
      const contextActions: MenuItemContextAction[] | undefined = canUpdateMainTab && mainTab !== tab.type
        && validMainTabTypes.has(tab.type) ? [{
          title: lang('ProfileMenuSetMainTab'),
          icon: 'reorder-tabs',
          handler: () => {
            setMainProfileTab({ chatId, tab: tab.type as ApiProfileTab });
          },
        }] : undefined;

      return {
        type: tab.type,
        title: lang(tab.key),
        contextActions,
      } satisfies TabWithPropertiesAndType;
    });
  }, [
    isGeneralSavedMessages, hasStoriesTab, hasGiftsTab, hasMembersTab, hasPreviewMediaTab, isTopicInfo,
    hasCommonChatsTab, isBot, similarBots?.length, lang, isOwnProfile,
    mainTab, chatId, canUpdateMainTab, validMainTabTypes,
  ]);

  const [allowAutoScrollToTabs, startAutoScrollToTabsIfNeeded, stopAutoScrollToTabs] = useFlag(false);

  const setActiveTab = useLastCallback((type: ProfileTabType) => {
    if (isClosed) return;
    changeProfileTab({ profileTab: type });
    setSharedMediaSearchType({ mediaType: SHARED_MEDIA_TYPES.has(type) ? type as SharedMediaType : undefined });
  });

  useEffect(() => {
    if (isClosed) return;
    if (profileTab) {
      // Force reset scroll marker
      changeProfileTab({ profileTab, shouldScrollTo: undefined });
      return;
    };

    setActiveTab(tabs[0].type); // Set default tab
  }, [isClosed, profileTab, tabs]);

  useEffectWithPrevDeps(([prevPeerFullInfo]) => {
    if (prevPeerFullInfo || !peerFullInfo?.mainTab) return;
    setActiveTab(peerFullInfo.mainTab); // Only focus when loading full info
  }, [peerFullInfo]);

  const handleSwitchTab = useCallback((index: number) => {
    startAutoScrollToTabsIfNeeded();
    setActiveTab(tabs[index].type);
  }, [tabs]);

  useEffect(() => {
    if (hasPreviewMediaTab && !botPreviewMedia) {
      loadPreviewMedias({ botId: chatId });
    }
  }, [chatId, botPreviewMedia, hasPreviewMediaTab]);

  useEffect(() => {
    if (isBot && !similarBots && isSynced) {
      loadBotRecommendations({ userId: chatId });
    }
  }, [chatId, isBot, similarBots, isSynced]);

  // resetSelectedStoryAlbum and loadStoryAlbums removed — stories feature removed

  const { startViewTransition } = useViewTransition();
  const { createVtnStyle } = useVtn();

  const activeTabIndex = useMemo(() => {
    const index = tabs.findIndex(({ type }) => type === profileTab);
    return index === -1 ? 0 : index;
  }, [profileTab, tabs]);

  // Reset skip transition flag from previous render
  if (shouldSkipTransitionRef.current) {
    shouldSkipTransitionRef.current = false;
  }

  useSyncEffectWithPrevDeps(([prevProfileTab, prevActiveTabIndex]) => {
    if (prevProfileTab === profileTab && prevActiveTabIndex !== activeTabIndex) {
      shouldSkipTransitionRef.current = true;
    }
  }, [profileTab, activeTabIndex]);

  const tabType = tabs[activeTabIndex].type;
  const handleLoadCommonChats = useCallback(() => {
    loadCommonChats({ userId: chatId });
  }, [chatId]);
  // handleLoadPeerStories, handleLoadStoriesArchive removed — stories feature removed
  const handleLoadGifts = useCallback(() => {
    loadPeerSavedGifts({ peerId: chatId });
  }, [chatId]);

  const handleLoadMoreMembers = useCallback(() => {
    loadMoreMembers({ chatId });
  }, [chatId, loadMoreMembers]);

  const [resultType, viewportIds, getMore, noProfileInfo] = useProfileViewportIds({
    loadMoreMembers: handleLoadMoreMembers,
    searchMessages: searchSharedMediaMessages,
    loadStories: () => {},
    loadStoriesArchive: () => {},
    loadMoreGifts: handleLoadGifts,
    loadCommonChats: handleLoadCommonChats,
    tabType,
    mediaSearchType,
    groupChatMembers: members,
    commonChatIds,
    usersById,
    userStatusesById,
    chatsById,
    chatMessages: messagesById,
    foundIds,
    threadId,
    storyIds,
    pinnedStoryIds,
    archiveStoryIds,
    similarBots,
  });

  const shouldRenderProfileInfo = !noProfileInfo && !isSavedMessages;

  const isFirstTab = tabs[0].type === resultType;
  const activeKey = tabs.findIndex(({ type }) => type === resultType);

  const [isStoryAlbumsShowed, markStoryAlbumsShowed, unmarkStoryAlbums] = useFlag(false);

  const hasStoryAlbums = storyAlbums && storyAlbums.length > 0;
  const isStoriesResult = resultType === 'stories';
  const shouldShowContentPanel = isStoriesResult && hasStoryAlbums;

  useEffect(() => {
    if (hasStoryAlbums) {
      setTimeout(() => {
        markStoryAlbumsShowed();
      }, CONTENT_PANEL_SHOW_DELAY);
    } else {
      unmarkStoryAlbums();
    }
  }, [hasStoryAlbums, markStoryAlbumsShowed]);

  usePeerStoriesPolling(resultType === 'members' ? viewportIds as string[] : undefined);

  const handleStopAutoScrollToTabs = useLastCallback(() => {
    stopAutoScrollToTabs();
  });

  const handleExpandProfile = useLastCallback(() => {
    if (isProfileExpanded) return;
    startViewTransition(VTT_RIGHT_PROFILE_EXPAND, () => {
      expandProfile();
    });
  });

  const handleCollapseProfile = useLastCallback(() => {
    if (!isProfileExpanded) return;
    startViewTransition(VTT_RIGHT_PROFILE_COLLAPSE, () => {
      collapseProfile();
    });
  });

  const { handleScroll } = useProfileState({
    containerRef,
    tabType: resultType,
    profileState,
    forceScrollProfileTab,
    allowAutoScrollToTabs,
    onProfileStateChange,
    handleStopAutoScrollToTabs,
  });

  useTransitionFixes(containerRef);

  const [cacheBuster, resetCacheBuster] = useCacheBuster();

  const { observe: observeIntersectionForMedia } = useIntersectionObserver({
    rootRef: containerRef,
    throttleMs: INTERSECTION_THROTTLE,
  });

  const handleNewMemberDialogOpen = useLastCallback(() => {
    setNewChatMembersDialogState({ newChatMembersProgress: NewChatMembersProgress.InProgress });
  });

  const handleSelectMedia = useLastCallback((messageId: number) => {
    openMediaViewer({
      chatId: profileId,
      threadId: MAIN_THREAD_ID,
      messageId,
      origin: MediaViewerOrigin.SharedMedia,
    });
  });

  const handleSelectPreviewMedia = useLastCallback((index: number) => {
    openMediaViewer({
      standaloneMedia: botPreviewMedia?.flatMap((item) => item?.content.photo
        || item?.content.video).filter(Boolean),
      origin: MediaViewerOrigin.PreviewMedia,
      mediaIndex: index,
    });
  });

  const handlePlayAudio = useLastCallback((messageId: number) => {
    openAudioPlayer({ chatId: profileId, messageId });
  });

  const handleMemberClick = useLastCallback((id: string) => {
    openChat({ id });
  });

  const handleMessageFocus = useLastCallback((message: ApiMessage) => {
    focusMessage({ chatId: message.chatId, messageId: message.id });
  });

  const handleDeleteMembersModalClose = useLastCallback(() => {
    setDeletingUserId(undefined);
  });

  useTopOverscroll({
    containerRef,
    onOverscroll: handleExpandProfile,
    onReset: handleCollapseProfile,
    isOverscrolled: isProfileExpanded,
    isDisabled: !hasAvatar || !shouldRenderProfileInfo,
  });

  useEffect(() => {
    if (!transitionRef.current || !IS_TOUCH_ENV) {
      return undefined;
    }

    return captureEvents(transitionRef.current, {
      selectorToPreventScroll: '.Profile',
      onSwipe: (e, direction) => {
        if (direction === SwipeDirection.Left) {
          const nextIndex = Math.min(activeTabIndex + 1, tabs.length - 1);
          setActiveTab(tabs[nextIndex].type);
          return true;
        } else if (direction === SwipeDirection.Right) {
          const nextIndex = Math.max(0, activeTabIndex - 1);
          setActiveTab(tabs[nextIndex].type);
          return true;
        }

        return false;
      },
    });
  }, [activeTabIndex, tabs]);

  let renderingDelay;
  // @optimization Used to unparallelize rendering of message list and profile media
  if (isFirstTab) {
    renderingDelay = !isRightColumnShown ? HIDDEN_RENDER_DELAY : 0;
    // @optimization Used to delay first render of secondary tabs while animating
  } else if (!viewportIds && !botPreviewMedia) {
    renderingDelay = SLIDE_TRANSITION_DURATION;
  }

  const canRenderContent = useAsyncRendering([chatId, threadId, resultType,
    activeTabIndex, activeCollectionId, selectedStoryAlbumId], renderingDelay);

  function getMemberContextAction(memberId: string): MenuItemContextAction[] | undefined {
    return memberId === currentUserId || !canDeleteMembers ? undefined : [{
      title: oldLang('lng_context_remove_from_group'),
      icon: 'stop',
      handler: () => {
        setDeletingUserId(memberId);
      },
    }];
  }

  function renderContent() {
    if (resultType === 'dialogs') {
      return (
        <ChatList className="saved-dialogs" folderType="saved" isActive />
      );
    }

    const noContent = (!viewportIds && !botPreviewMedia) || !canRenderContent || !messagesById;
    const noSpinner = isFirstTab && !canRenderContent;

    return (
      <div>
        {renderCategories()}
        {renderSpinnerOrContent(noContent, noSpinner)}
      </div>
    );
  }

  function renderCategories() {
    return undefined; // Stories removed
  }

  function renderSpinnerOrContentBase(noContent: boolean, noSpinner: boolean) {
    if (noContent) {
      const forceRenderHiddenMembers = Boolean(resultType === 'members' && areMembersHidden);

      return (
        <div
          className="content empty-list"
        >
          {!noSpinner && !forceRenderHiddenMembers && <Spinner />}
          {forceRenderHiddenMembers && <NothingFound text={lang('ChatMemberListNoAccess')} />}
        </div>
      );
    }

    const isViewportIdsEmpty = viewportIds && !viewportIds?.length;

    if (isViewportIdsEmpty) {
      let text: string;

      switch (resultType) {
        case 'members':
          text = areMembersHidden ? lang('ChatMemberListNoAccess') : lang('NoMembersFound');
          break;
        case 'commonChats':
          text = oldLang('NoGroupsInCommon');
          break;
        case 'documents':
          text = oldLang('MediaFileEmpty');
          break;
        case 'links':
          text = oldLang('MediaLinkEmpty');
          break;
        case 'audio':
          text = oldLang('MediaSongEmpty');
          break;
        case 'voice':
          text = oldLang('MediaAudioEmpty');
          break;
        case 'stories':
          text = oldLang('StoryList.SavedEmptyState.Title');
          break;
        case 'storiesArchive':
          text = oldLang('StoryList.ArchivedEmptyState.Title');
          break;
        case 'gif':
          text = oldLang('NoGIFsFound');
          break;
        default:
          text = oldLang('SharedMediaEmptyTitle');
      }

      return (
        <div className="content empty-list">
          <NothingFound text={text} />
        </div>
      );
    }

    if (!messagesById) {
      // A TypeScript assertion, should never be really reached
      return;
    }

    const noTransition = resultType === 'stories' ? isStoryAlbumsShowed : false;
    return (
      <div
        className={buildClassName(
          `content ${resultType}-list`,
          shouldShowContentPanel && 'showContentPanel',
          noTransition && 'noTransition',
        )}
        dir={lang.isRtl && (resultType === 'media' || resultType === 'gif') ? 'rtl' : undefined}
        teactFastList
      >
        {resultType === 'media' || resultType === 'gif' ? (
          (viewportIds as number[]).map((id) => messagesById[id] && (
            <Media
              key={id}
              message={messagesById[id]}
              isProtected={isChatProtected || messagesById[id].isProtected}
              canAutoPlay={canAutoPlayGifs}
              observeIntersection={observeIntersectionForMedia}
              onClick={handleSelectMedia}
            />
          ))
        ) : resultType === 'documents' ? (
          (viewportIds as number[]).map((id) => messagesById[id] && (
            <Document
              key={id}
              id={`shared-media${getMessageHtmlId(id)}`}
              document={getMessageDocument(messagesById[id])!}
              datetime={messagesById[id].date}
              smaller
              className="scroll-item"
              isDownloading={getIsDownloading(activeDownloads, getMessageDocument(messagesById[id])!)}
              observeIntersection={observeIntersectionForMedia}
              onDateClick={handleMessageFocus}
              message={messagesById[id]}
              shouldWarnAboutFiles={shouldWarnAboutFiles}
              onMediaClick={handleSelectMedia}
            />
          ))
        ) : resultType === 'links' ? (
          (viewportIds as number[]).map((id) => messagesById[id] && (
            <WebLink
              key={id}
              message={messagesById[id]}
              isProtected={isChatProtected || messagesById[id].isProtected}
              observeIntersection={observeIntersectionForMedia}
              onMessageClick={handleMessageFocus}
            />
          ))
        ) : resultType === 'audio' ? (
          (viewportIds as number[]).map((id) => messagesById[id] && (
            <Audio
              key={id}
              theme={theme}
              message={messagesById[id]}
              origin={AudioOrigin.SharedMedia}
              date={messagesById[id].date}
              className="scroll-item"
              onPlay={handlePlayAudio}
              onDateClick={handleMessageFocus}
              canDownload={!isChatProtected && !messagesById[id].isProtected}
              isDownloading={messagesById[id].content?.audio
                ? getIsDownloading(activeDownloads, messagesById[id].content.audio)
                : false}
            />
          ))
        ) : resultType === 'voice' ? (
          (viewportIds as number[]).map((id) => {
            const global = getGlobal();
            const message = messagesById[id];
            if (!message) return undefined;

            const media = selectMessageDownloadableMedia(global, message)!;
            return messagesById[id] && (
              <Audio
                key={id}
                theme={theme}
                message={message}
                senderTitle={getSenderName(oldLang, message, chatsById, usersById)}
                origin={AudioOrigin.SharedMedia}
                date={message.date}
                className="scroll-item"
                onPlay={handlePlayAudio}
                onDateClick={handleMessageFocus}
                canDownload={!isChatProtected && !message.isProtected}
                isDownloading={getIsDownloading(activeDownloads, media)}
              />
            );
          })
        ) : resultType === 'members' ? (
          (viewportIds as string[]).map((id, i) => (
            <ListItem
              key={id}
              teactOrderKey={i}
              className="chat-item-clickable contact-list-item scroll-item small-icon"

              onClick={() => handleMemberClick(id)}
              contextActions={getMemberContextAction(id)}
            >
              <PrivateChatInfo userId={id} adminMember={adminMembersById?.[id]} forceShowSelf />
            </ListItem>
          ))
        ) : resultType === 'commonChats' ? (
          (viewportIds as string[]).map((id, i) => (
            <ListItem
              key={id}
              teactOrderKey={i}
              className="chat-item-clickable scroll-item small-icon"

              onClick={() => openChat({ id })}
            >
              <GroupChatInfo chatId={id} />
            </ListItem>
          ))
        ) : resultType === 'previewMedia' ? (
          botPreviewMedia!.map((media, i) => (
            <PreviewMedia
              key={media.date}
              media={media}
              isProtected={isChatProtected}
              observeIntersection={observeIntersectionForMedia}
              onClick={handleSelectPreviewMedia}
              index={i}
            />
          ))
        ) : resultType === 'similarBots' ? (
          <div key={resultType}>
            {(viewportIds as string[]).map((userId, i) => (
              <ListItem
                key={userId}
                teactOrderKey={i}
                className={buildClassName(
                  'chat-item-clickable search-result',
                  !isCurrentUserPremium && i === similarBots!.length - 1 && 'blured',
                )}

                onClick={() => openChat({ id: userId })}
              >
                <PrivateChatInfo
                  userId={userId}
                  avatarSize="medium"
                />
              </ListItem>
            ))}
            {!isCurrentUserPremium && (
              <>
                <Button className="show-more-bots" onClick={() => openPremiumModal()} iconName="unlock-badge">
                  {lang('UnlockMoreSimilarBots')}
                </Button>
                <div className="more-similar">
                  {renderText(lang('MoreSimilarBotsDescription', { count: limitSimilarPeers }, {
                    withNodes: true,
                    withMarkdown: true,
                    pluralValue: limitSimilarPeers,
                  }))}
                </div>
              </>
            )}
          </div>
        ) : undefined}
      </div>
    );
  }

  const shouldUseTransitionForContent = resultType === 'stories';
  const contentTransitionKey = (() => {
    if (resultType === 'stories') {
      return selectedStoryAlbumId === 'all' ? 0 : selectedStoryAlbumId;
    }
    return 0;
  })();

  const handleOnStop = useLastCallback(() => {
    setRestoreContentHeightKey(restoreContentHeightKey + 1);
  });

  function renderProfileInfo(peerId: string, isReady: boolean) {
    return (
      <div className="profile-info">
        <ProfileInfo
          isExpanded={isProfileExpanded}
          peerId={peerId}
          canPlayVideo={isReady}
          isForMonoforum={Boolean(monoforumChannel)}
          onExpand={handleExpandProfile}
        />
        <ChatExtra
          chatOrUserId={profileId}
          isSavedDialog={isSavedDialog}
          isOwnProfile={isOwnProfile}
        />
      </div>
    );
  }

  function renderSpinnerOrContent(noContent: boolean, noSpinner: boolean) {
    const baseContent = renderSpinnerOrContentBase(noContent, noSpinner);

    const isSpinner = noContent && !noSpinner;

    if (shouldUseTransitionForContent) {
      return (
        <Transition
          className={`${resultType}-list`}
          activeKey={contentTransitionKey}
          name={resolveTransitionName('slideOptimized', animationLevel, undefined, lang.isRtl)}
          shouldCleanup
          shouldRestoreHeight
          restoreHeightKey={restoreContentHeightKey}
          contentSelector=".Transition > .Transition_slide-active > .content"
        >
          <Transition
            activeKey={isSpinner ? 0 : 1}
            name="fade"
            shouldCleanup
            shouldRestoreHeight
            restoreHeightKey={restoreContentHeightKey}
            contentSelector=".content"
            onStop={handleOnStop}
          >
            {baseContent}
          </Transition>
        </Transition>
      );
    }

    return (
      <Transition
        activeKey={isSpinner ? 0 : 1}
        name="fade"
        shouldCleanup
        shouldRestoreHeight
      >
        {baseContent}
      </Transition>
    );
  }

  const activeListSelector = `.shared-media-transition > .Transition_slide-active`;
  // eslint-disable-next-line @stylistic/max-len
  const nestedSelector = `${activeListSelector} > .Transition > .Transition_slide-active > .Transition > .Transition_slide-active`;
  const itemSelector = !shouldUseTransitionForContent
    ? `${activeListSelector} .${resultType}-list > .scroll-item`
    : `${nestedSelector} > .${resultType}-list > .scroll-item`;

  return (
    <InfiniteScroll
      ref={containerRef}
      className="Profile custom-scroll"
      itemSelector={itemSelector}
      items={canRenderContent ? viewportIds : undefined}
      cacheBuster={cacheBuster}
      sensitiveArea={PROFILE_SENSITIVE_AREA}
      preloadBackwards={canRenderContent ? (resultType === 'members' ? MEMBERS_SLICE : SHARED_MEDIA_SLICE) : 0}
      // To prevent scroll jumps caused by reordering member list
      noScrollRestoreOnTop
      noFastList
      onLoadMore={getMore}
      onScroll={handleScroll}
    >
      {!noProfileInfo && !isSavedMessages && (
        renderProfileInfo(
          monoforumChannel?.id || profileId,
          isRightColumnShown && canRenderContent,
        )
      )}
      {!isRestricted && (
        <div
          className="shared-media"
          style={createVtnStyle('sharedMedia')}
        >
          <Transition
            ref={transitionRef}
            name={shouldSkipTransitionRef.current ? 'none'
              : resolveTransitionName('slideOptimized', animationLevel, undefined, lang.isRtl)}
            activeKey={activeKey}
            renderCount={tabs.length}
            shouldRestoreHeight
            className="shared-media-transition"
            onStop={resetCacheBuster}
            restoreHeightKey={shouldUseTransitionForContent ? restoreContentHeightKey : undefined}
            contentSelector={shouldUseTransitionForContent
              ? '.Transition > .Transition_slide-active > .Transition > .Transition_slide-active > .content'
              : undefined}
          >
            {renderContent()}
          </Transition>
          <TabList activeTab={activeTabIndex} tabs={tabs} onSwitchTab={handleSwitchTab} />
        </div>
      )}

      {canAddMembers && (
        <FloatingActionButton
          className={buildClassName(!isActive && 'hidden')}
          isShown={canRenderContent}
          onClick={handleNewMemberDialogOpen}
          ariaLabel={oldLang('lng_channel_add_users')}
          iconName="add-user-filled"
        />
      )}
      {canDeleteMembers && (
        <DeleteMemberModal
          isOpen={Boolean(deletingUserId)}
          userId={deletingUserId}
          onClose={handleDeleteMembersModalClose}
        />
      )}
    </InfiniteScroll>
  );
};

export default memo(withGlobal<OwnProps>(
  (global, {
    chatId, threadId, isMobile,
  }): Complete<StateProps> => {
    const chat = selectChat(global, chatId);
    const dmPeerUserId = selectDmPeerUserId(global, chatId);
    const resolvedUserId = dmPeerUserId || chatId;
    const user = selectUser(global, resolvedUserId);
    const chatFullInfo = selectChatFullInfo(global, chatId);
    const userFullInfo = user ? selectUserFullInfo(global, resolvedUserId) : undefined;
    const messagesById = selectChatMessages(global, chatId);

    const tabState = selectTabState(global);
    const { chatInfo } = tabState;
    const { isOwnProfile } = chatInfo;

    const { animationLevel, shouldWarnAboutFiles } = selectSharedSettings(global);

    const { currentType: mediaSearchType, resultsByType } = selectCurrentSharedMediaSearch(global) || {};
    const { foundIds } = (resultsByType && mediaSearchType && resultsByType[mediaSearchType]) || {};

    const isTopicInfo = Boolean(chat?.isForum && threadId && threadId !== MAIN_THREAD_ID);

    const { byId: usersById, statusesById: userStatusesById } = global.users;
    const { byId: chatsById } = global.chats;

    const isSavedMessages = !isOwnProfile && chatId ? selectIsChatWithSelf(global, chatId) : false;
    const isSavedDialog = !isOwnProfile ? getIsSavedDialog(chatId, threadId, global.currentUserId) : undefined;

    const isGroup = chat && isChatGroup(chat);
    const isBot = user && isUserBot(user);
    const hasMembersTab = !isTopicInfo && !isSavedDialog && isGroup && !chat?.isMonoforum;
    const members = chatFullInfo?.members;
    const adminMembersById = chatFullInfo?.adminMembersById;
    const areMembersHidden = hasMembersTab && chat
      && (chat.isForbidden || (chatFullInfo && !chatFullInfo.canViewMembers));
    const canAddMembers = hasMembersTab && chat
      && (getHasAdminRight(chat, 'inviteUsers') || !isUserRightBanned(chat, 'inviteUsers')
        || chat.isCreator);
    const canDeleteMembers = hasMembersTab && chat && (getHasAdminRight(chat, 'banUsers') || chat.isCreator);
    const activeDownloads = selectActiveDownloads(global);
    const { similarBotsIds } = selectSimilarBotsIds(global, chatId) || {};
    const isCurrentUserPremium = selectIsCurrentUserPremium(global);

    const peer = user || chat;
    const peerFullInfo = userFullInfo || chatFullInfo;

    const hasCommonChatsTab = user && !user.isSelf && !isUserBot(user) && !isSavedMessages
      && Boolean(userFullInfo?.commonChatsCount);
    const commonChats = selectUserCommonChats(global, chatId);

    const hasPreviewMediaTab = userFullInfo?.botInfo?.hasPreviewMedia;
    const botPreviewMedia = global.users.previewMediaByBotId[chatId];

    const hasStoriesTab = false;
    const storyIds = undefined;
    const pinnedStoryIds = undefined;
    const storyByIds = undefined;
    const archiveStoryIds = undefined;

    const hasGiftsTab = Boolean(peerFullInfo?.starGiftCount) && !isSavedMessages;
    const activeCollectionId = selectActiveGiftsCollectionId(global, chatId);

    const storyAlbums = undefined;
    const selectedStoryAlbumId = 'all' as const;

    const monoforumChannel = selectMonoforumChannel(global, chatId);
    const isRestricted = chat && selectIsChatRestricted(global, chat.id);
    const hasAvatar = Boolean(peer?.avatarPhotoId);

    const canAutoPlayGifs = selectPerformanceSettingsValue(global, 'autoplayGifs');

    return {
      theme: selectTheme(global),
      isBot,
      messagesById,
      foundIds,
      mediaSearchType,
      hasCommonChatsTab,
      hasStoriesTab,
      hasMembersTab,
      hasPreviewMediaTab,
      areMembersHidden,
      canAddMembers,
      canDeleteMembers,
      currentUserId: global.currentUserId,
      isRightColumnShown: selectIsRightColumnShown(global, isMobile),
      isRestricted,
      activeDownloads,
      usersById,
      userStatusesById,
      chatsById,
      storyIds,
      hasGiftsTab,
      storyAlbums,
      pinnedStoryIds,
      archiveStoryIds,
      storyByIds,
      selectedStoryAlbumId,
      activeCollectionId,
      isChatProtected: chat?.isProtected,
      chatInfo,
      animationLevel,
      shouldWarnAboutFiles,
      similarBots: similarBotsIds,
      botPreviewMedia,
      isCurrentUserPremium,
      isTopicInfo,
      isSavedDialog,
      isSavedMessages,
      isSynced: global.isSynced,
      limitSimilarPeers: selectPremiumLimit(global, 'recommendedChannels'),
      members: hasMembersTab ? members : undefined,
      adminMembersById: hasMembersTab ? adminMembersById : undefined,
      commonChatIds: commonChats?.ids,
      monoforumChannel,
      hasAvatar,
      peerFullInfo,
      canUpdateMainTab: selectCanUpdateMainTab(global, chatId),
      canAutoPlayGifs,
    };
  },
)(Profile));
