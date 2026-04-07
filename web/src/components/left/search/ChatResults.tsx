import type { FC } from '../../../lib/teact/teact';
import type React from '../../../lib/teact/teact';
import {
  memo, useCallback, useEffect,
  useMemo, useRef, useState,
} from '../../../lib/teact/teact';
import { getActions, getGlobal, withGlobal } from '../../../global';

import type { ApiMessage, ApiMessageSearchContext } from '../../../api/types';
import { LoadMoreDirection } from '../../../types';

import { ALL_FOLDER_ID } from '../../../config';
import { filterPeersByQuery } from '../../../global/helpers/peers';
import { selectTabState } from '../../../global/selectors';
import buildClassName from '../../../util/buildClassName';
import { getOrderedIds } from '../../../util/folderManager';
import { unique } from '../../../util/iteratees';
import { parseSearchResultKey, type SearchResultKey } from '../../../util/keys/searchResultKey';
import { MEMO_EMPTY_ARRAY } from '../../../util/memo';
import { throttle } from '../../../util/schedulers';
import { renderMessageSummary } from '../../common/helpers/renderMessageText';
import sortChatIds from '../../common/helpers/sortChatIds';

import useAppLayout from '../../../hooks/useAppLayout';
import useContextMenuHandlers from '../../../hooks/useContextMenuHandlers';
import useHorizontalScroll from '../../../hooks/useHorizontalScroll';
import { useIntersectionObserver } from '../../../hooks/useIntersectionObserver';
import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import Icon from '../../common/icons/Icon';
import NothingFound from '../../common/NothingFound';
import PeerChip from '../../common/PeerChip';
import InfiniteScroll from '../../ui/InfiniteScroll';
import Link from '../../ui/Link';
import Loading from '../../ui/Loading';
import Menu from '../../ui/Menu';
import MenuItem from '../../ui/MenuItem';
import Transition from '../../ui/Transition';
import ChatMessage from './ChatMessage';
import DateSuggest from './DateSuggest';
import LeftSearchResultChat from './LeftSearchResultChat';
import RecentContacts from './RecentContacts';

import './ChatResults.scss';

export type OwnProps = {
  searchQuery?: string;
  dateSearchQuery?: string;
  searchDate?: number;
  onReset: () => void;
  onSearchDateSelect: (value: Date) => void;
};

type StateProps = {
  currentUserId?: string;
  contactIds?: string[];
  accountPeerIds?: string[];
  globalPeerIds?: string[];
  foundIds?: SearchResultKey[];
  globalMessagesByChatId?: Record<string, { byId: Record<number, ApiMessage> }>;
  fetchingStatus?: { chats?: boolean; messages?: boolean };
};

const MIN_QUERY_LENGTH_FOR_GLOBAL_SEARCH = 4;
const LESS_LIST_ITEMS_AMOUNT = 5;
const INTERSECTION_THROTTLE = 200;

const runThrottled = throttle((cb) => cb(), 500, false);

const ChatResults: FC<OwnProps & StateProps> = ({
  searchQuery,
  searchDate,
  dateSearchQuery,
  currentUserId,
  contactIds,
  accountPeerIds,
  globalPeerIds,
  foundIds,
  globalMessagesByChatId,
  fetchingStatus,
  onReset,
  onSearchDateSelect,
}) => {
  const {
    openChat, addRecentlyFoundChatId, searchMessagesGlobal,
    setGlobalSearchChatId,
  } = getActions();

  const containerRef = useRef<HTMLDivElement>();
  const chatSelectionRef = useRef<HTMLDivElement>();

  const lang = useLang();

  const { isMobile } = useAppLayout();
  const [shouldShowMoreLocal, setShouldShowMoreLocal] = useState<boolean>(false);
  const [shouldShowMoreGlobal, setShouldShowMoreGlobal] = useState<boolean>(false);
  const [searchContext, setSearchContext] = useState<ApiMessageSearchContext>('all');
  const ref = useRef<HTMLDivElement>();

  const handleLoadMore = useCallback(({ direction }: { direction: LoadMoreDirection }) => {
    if (direction === LoadMoreDirection.Backwards) {
      runThrottled(() => {
        searchMessagesGlobal({
          type: 'text',
          context: searchContext,
        });
      });
    }
  // eslint-disable-next-line react-hooks-static-deps/exhaustive-deps -- `searchQuery` is required to prevent infinite message loading
  }, [searchQuery, searchContext]);

  const handleChatClick = useCallback(
    (id: string) => {
      openChat({ id, shouldReplaceHistory: true });

      if (id !== currentUserId) {
        addRecentlyFoundChatId({ id });
      }

      if (!isMobile) {
        onReset();
      }
    },
    [openChat, currentUserId, isMobile, addRecentlyFoundChatId, onReset],
  );

  const handlePickerItemClick = useCallback((id: string) => {
    setGlobalSearchChatId({ id });
  }, [setGlobalSearchChatId]);

  function getSearchContextCaption(context: ApiMessageSearchContext) {
    if (context === 'users') return lang('PrivateChatsSearchContext');
    if (context === 'groups') return lang('GroupChatsSearchContext');
    if (context === 'channels') return lang('ChannelsSearchContext');
    return lang('AllChatsSearchContext');
  }

  const {
    isContextMenuOpen, contextMenuAnchor, handleContextMenu,
    handleContextMenuClose, handleContextMenuHide,
  } = useContextMenuHandlers(ref);

  const getRootElement = useLastCallback(() => ref.current!);
  const getMenuElement = useLastCallback(() => ref.current!.querySelector('.chatResultsContextMenu .bubble'));
  const getTriggerElement = useLastCallback(() => ref.current!.querySelector('.menuTrigger'));

  const handleClickContext = useLastCallback((e: React.MouseEvent): void => {
    handleContextMenu(e);
  });

  const itemPlaceholderClass = buildClassName('icon', 'iconPlaceholder');

  function renderContextMenu() {
    return (
      <Menu
        isOpen={isContextMenuOpen}
        anchor={contextMenuAnchor}
        getTriggerElement={getTriggerElement}
        getRootElement={getRootElement}
        getMenuElement={getMenuElement}
        className="chatResultsContextMenu"
        onClose={handleContextMenuClose}
        onCloseAnimationEnd={handleContextMenuHide}
        autoClose
      >
        <>
          <MenuItem
            icon={searchContext === 'all' ? 'check' : undefined}
            customIcon={searchContext !== 'all' ? <i className={itemPlaceholderClass} /> : undefined}

            onClick={() => setSearchContext('all')}
          >
            {getSearchContextCaption('all')}
          </MenuItem>
          <MenuItem
            icon={searchContext === 'users' ? 'check' : undefined}
            customIcon={searchContext !== 'users' ? <i className={itemPlaceholderClass} /> : undefined}

            onClick={() => setSearchContext('users')}
          >
            {getSearchContextCaption('users')}
          </MenuItem>
          <MenuItem
            icon={searchContext === 'groups' ? 'check' : undefined}
            customIcon={searchContext !== 'groups' ? <i className={itemPlaceholderClass} /> : undefined}

            onClick={() => setSearchContext('groups')}
          >
            {getSearchContextCaption('groups')}
          </MenuItem>
          <MenuItem
            icon={searchContext === 'channels' ? 'check' : undefined}
            customIcon={searchContext !== 'channels' ? <i className={itemPlaceholderClass} /> : undefined}

            onClick={() => setSearchContext('channels')}
          >
            {getSearchContextCaption('channels')}
          </MenuItem>
        </>
      </Menu>
    );
  }

  const localResults = useMemo(() => {
    if (!searchQuery || (searchQuery.startsWith('@') && searchQuery.length < 2)) {
      return MEMO_EMPTY_ARRAY;
    }

    const orderedChatIds = getOrderedIds(ALL_FOLDER_ID) ?? [];
    const localChatIds = filterPeersByQuery({ ids: orderedChatIds, query: searchQuery, type: 'chat' });

    const contactIdsWithMe = [
      ...(currentUserId ? [currentUserId] : []),
      ...(contactIds || []),
    ];

    const localContactIds = filterPeersByQuery({
      ids: contactIdsWithMe, query: searchQuery, type: 'user',
    });

    const localPeerIds = [
      ...localContactIds,
      ...localChatIds,
    ];

    return unique([
      ...sortChatIds(localPeerIds, undefined, currentUserId ? [currentUserId] : undefined),
      ...sortChatIds(accountPeerIds || []),
    ]);
  }, [searchQuery, currentUserId, contactIds, accountPeerIds]);

  useHorizontalScroll(chatSelectionRef, !localResults.length, true);

  const globalResults = useMemo(() => {
    if (!searchQuery || searchQuery.length < MIN_QUERY_LENGTH_FOR_GLOBAL_SEARCH || !globalPeerIds) {
      return MEMO_EMPTY_ARRAY;
    }

    // No need for expensive global updates, so we avoid them
    const chatsById = getGlobal().chats.byId;

    return sortChatIds(globalPeerIds, true);
  }, [globalPeerIds, searchQuery]);

  const foundMessages = useMemo(() => {
    if ((!searchQuery && !searchDate) || !foundIds || foundIds.length === 0) {
      return MEMO_EMPTY_ARRAY;
    }

    // No need for expensive global updates, so we avoid them
    const chatsById = getGlobal().chats.byId;

    return foundIds
      .map((id) => {
        const [chatId, messageId] = parseSearchResultKey(id);
        const chat = chatsById[chatId];
        if (!chat) return undefined;

        return globalMessagesByChatId?.[chatId]?.byId[messageId];
      })
      .filter(Boolean);
  }, [searchQuery, searchDate, foundIds, globalMessagesByChatId]);

  useEffect(() => {
    if (!searchQuery) return;
    searchMessagesGlobal({
      type: 'text',
      context: searchContext,
      shouldResetResultsByType: true,
      shouldCheckFetchingMessagesStatus: true,
    });
    // eslint-disable-next-line react-hooks-static-deps/exhaustive-deps
  }, [searchContext]);

  const handleClickShowMoreLocal = useCallback(() => {
    setShouldShowMoreLocal(!shouldShowMoreLocal);
  }, [shouldShowMoreLocal]);

  const handleClickShowMoreGlobal = useCallback(() => {
    setShouldShowMoreGlobal(!shouldShowMoreGlobal);
  }, [shouldShowMoreGlobal]);

  function renderFoundMessage(message: ApiMessage) {
    const chatsById = getGlobal().chats.byId;

    const text = renderMessageSummary(lang, message);
    const chat = chatsById[message.chatId];

    if (!text || !chat) {
      return undefined;
    }

    return (
      <ChatMessage
        chatId={message.chatId}
        message={message}
        searchQuery={searchQuery}
      />
    );
  }

  const actualFoundIds = foundMessages;

  const nothingFound = searchContext === 'all' && fetchingStatus && !fetchingStatus.chats && !fetchingStatus.messages
    && !localResults.length && !globalResults.length && !actualFoundIds.length;
  const isMessagesFetching = fetchingStatus?.messages;

  const shouldRenderTopPeers = !searchQuery && !searchDate;

  useIntersectionObserver({
    rootRef: containerRef,
    throttleMs: INTERSECTION_THROTTLE,
    isDisabled: !shouldRenderTopPeers,
  });

  if (shouldRenderTopPeers) {
    return <RecentContacts onReset={onReset} />;
  }

  const shouldRenderMessagesSection = searchContext === 'all' ? Boolean(actualFoundIds.length) : true;

  return (
    <InfiniteScroll
      ref={containerRef}
      className="LeftSearch--content custom-scroll"
      items={actualFoundIds}
      onLoadMore={handleLoadMore}
      // To prevent scroll jumps caused by delayed local results rendering
      noScrollRestoreOnTop
      noFastList
    >
      {dateSearchQuery && (
        <div className="chat-selection no-scrollbar">
          <DateSuggest
            searchDate={dateSearchQuery}
            onSelect={onSearchDateSelect}
          />
        </div>
      )}
      {nothingFound && (
        <NothingFound
          withSticker
          text={lang('ChatListSearchNoResults')}
          description={lang('ChatListSearchNoResultsDescription')}
        />
      )}
      {Boolean(localResults.length) && (
        <div
          className="chat-selection no-scrollbar"
          dir={lang.isRtl ? 'rtl' : undefined}
          ref={chatSelectionRef}
        >
          {localResults.map((id) => (
            <PeerChip
              peerId={id}
              className="left-search-local-suggestion"
              onClick={handlePickerItemClick}
              clickArg={id}
            />
          ))}
        </div>
      )}
      {Boolean(localResults.length) && (
        <div className="search-section">
          <h3 className="section-heading" dir={lang.isRtl ? 'auto' : undefined}>
            {localResults.length > LESS_LIST_ITEMS_AMOUNT && (
              <Link className="Link" onClick={handleClickShowMoreLocal}>
                {lang(shouldShowMoreLocal ? 'ChatListSearchShowLess' : 'ChatListSearchShowMore')}
              </Link>
            )}
            {lang('DialogListSearchSectionDialogs')}
          </h3>
          {localResults.map((id, index) => {
            if (!shouldShowMoreLocal && index >= LESS_LIST_ITEMS_AMOUNT) {
              return undefined;
            }

            return (
              <LeftSearchResultChat
                withOpenAppButton
                chatId={id}
                onClick={handleChatClick}
              />
            );
          })}
        </div>
      )}
      {Boolean(globalResults.length) && (
        <div className="search-section">
          <h3 className="section-heading" dir={lang.isRtl ? 'auto' : undefined}>
            {globalResults.length > LESS_LIST_ITEMS_AMOUNT && (
              <Link className="Link" onClick={handleClickShowMoreGlobal}>
                {lang(shouldShowMoreGlobal ? 'ChatListSearchShowLess' : 'ChatListSearchShowMore')}
              </Link>
            )}
            {lang('DialogListSearchSectionGlobal')}
          </h3>
          {globalResults.map((id, index) => {
            if (!shouldShowMoreGlobal && index >= LESS_LIST_ITEMS_AMOUNT) {
              return undefined;
            }

            return (
              <LeftSearchResultChat
                chatId={id}
                withUsername
                onClick={handleChatClick}
              />
            );
          })}
        </div>
      )}
      <div className="menuOwner" ref={ref}>
        {renderContextMenu()}
        {shouldRenderMessagesSection && (
          <div className="search-section">
            <h3 className="section-heading" dir={lang.isRtl ? 'auto' : undefined}>
              <Link className="Link menuTrigger dropDownLink" onClick={handleClickContext}>
                {lang('SearchContextCaption', {
                  type: getSearchContextCaption(searchContext),
                }, {
                  withNodes: true,
                })}

                <Transition
                  name="fade"
                  shouldCleanup
                  activeKey={Number(isMessagesFetching)}
                  className="iconContainer"
                  slideClassName="iconContainerSlide"
                >
                  {isMessagesFetching && (<Loading />)}
                  {!isMessagesFetching && <Icon name="down" />}
                </Transition>
              </Link>
              {lang('SearchMessages')}
            </h3>
            {actualFoundIds.map(renderFoundMessage)}
          </div>
        )}
      </div>
    </InfiniteScroll>
  );
};

export default memo(withGlobal<OwnProps>(
  (global): Complete<StateProps> => {
    const { userIds: contactIds } = global.contactList || {};
    const {
      currentUserId, messages,
    } = global;

    if (!contactIds) {
      return {} as Complete<StateProps>;
    }

    const {
      fetchingStatus, globalResults, localResults, resultsByType,
    } = selectTabState(global).globalSearch;
    const { peerIds: globalPeerIds } = globalResults || {};
    const { peerIds: accountPeerIds } = localResults || {};
    const { byChatId: globalMessagesByChatId } = messages;
    const foundIds = resultsByType?.text?.foundIds;

    return {
      currentUserId,
      contactIds,
      accountPeerIds,
      globalPeerIds,
      foundIds,
      globalMessagesByChatId,
      fetchingStatus,
    };
  },
)(ChatResults));
