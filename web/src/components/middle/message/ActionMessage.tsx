import {
  memo, useEffect, useMemo, useRef,
  useUnmountCleanup,
} from '../../../lib/teact/teact';
import { getActions, withGlobal } from '../../../global';

import type { ApiMessageAction } from '../../../api/types/messageActions';
import type {
  FocusDirection,
  ScrollTargetPosition,
  ThreadId,
} from '../../../types';
import type { Signal } from '../../../util/signals';
import {
  type ApiMessage,
  type ApiPeer,
  MAIN_THREAD_ID,
} from '../../../api/types';
import { MediaViewerOrigin } from '../../../types';

import { MESSAGE_APPEARANCE_DELAY } from '../../../config';
import { getMessageHtmlId } from '../../../global/helpers';
import { getMessageReplyInfo } from '../../../global/helpers/replies';
import {
  selectActionMessageBg,
  selectChat,
  selectChatMessage,
  selectIsCurrentUserFrozen,
  selectIsCurrentUserPremium,
  selectIsInSelectMode,
  selectIsMessageFocused,
  selectSender,
  selectTabState,
} from '../../../global/selectors';
import { selectThreadReadState } from '../../../global/selectors/threads';
import { IS_TAURI } from '../../../util/browser/globalEnvironment';
import { IS_ANDROID, IS_FLUID_BACKGROUND_SUPPORTED } from '../../../util/browser/windowEnvironment';
import buildClassName from '../../../util/buildClassName';
import { isLocalMessageId } from '../../../util/keys/messageKey';
import { isElementInViewport } from '../../../util/visibility/isElementInViewport';
import { preventMessageInputBlur } from '../helpers/preventMessageInputBlur';

import useAppLayout from '../../../hooks/useAppLayout';
import useContextMenuHandlers from '../../../hooks/useContextMenuHandlers';
import useEnsureMessage from '../../../hooks/useEnsureMessage';
import useFlag from '../../../hooks/useFlag';
import { type ObserveFn, useOnIntersect } from '../../../hooks/useIntersectionObserver';
import useLastCallback from '../../../hooks/useLastCallback';
import useShowTransition from '../../../hooks/useShowTransition';
import useFluidBackgroundFilter from './hooks/useFluidBackgroundFilter';
import useFocusMessageListElement from './hooks/useFocusMessageListElement';

import ActionMessageText from './ActionMessageText';
import SuggestedPhoto from './actions/SuggestedPhoto';
import SuggestedPostApproval from './actions/SuggestedPostApproval';
import SuggestedPostBalanceTooLow from './actions/SuggestedPostBalanceTooLow';
import SuggestedPostRejected from './actions/SuggestedPostRejected';
import ContextMenuContainer from './ContextMenuContainer';
import Reactions from './reactions/Reactions';
import styles from './ActionMessage.module.scss';

type OwnProps = {
  message: ApiMessage;
  threadId: ThreadId;
  appearanceOrder: number;
  isJustAdded?: boolean;
  isLastInList?: boolean;
  memoFirstUnreadIdRef?: { current: number | undefined };
  getIsMessageListReady?: Signal<boolean>;
  observeIntersectionForBottom?: ObserveFn;
  observeIntersectionForLoading?: ObserveFn;
  observeIntersectionForPlaying?: ObserveFn;
  onMessageUnmount?: (messageId: number) => void;
};

type StateProps = {
  sender?: ApiPeer;
  currentUserId?: string;
  isInsideTopic?: boolean;
  isFocused?: boolean;
  focusDirection?: FocusDirection;
  noFocusHighlight?: boolean;
  replyMessage?: ApiMessage;
  actionMessageBg?: string;
  isCurrentUserPremium?: boolean;
  isInSelectMode?: boolean;
  hasUnreadReaction?: boolean;
  isResizingContainer?: boolean;
  scrollTargetPosition?: ScrollTargetPosition;
  isAccountFrozen?: boolean;
};

const SINGLE_LINE_ACTIONS = new Set<ApiMessageAction['type']>([
  'pinMessage',
  'chatEditPhoto',
  'chatDeletePhoto',
  'todoCompletions',
  'todoAppendTasks',
  'unsupported',
]);
const HIDDEN_TEXT_ACTIONS = new Set<ApiMessageAction['type']>([
  'suggestProfilePhoto', 'suggestedPostApproval']);

const ActionMessage = ({
  message,
  threadId,
  sender,
  currentUserId,
  appearanceOrder,
  isJustAdded,
  isLastInList,
  memoFirstUnreadIdRef,
  getIsMessageListReady,
  isInsideTopic,
  isFocused,
  focusDirection,
  noFocusHighlight,
  replyMessage,
  actionMessageBg,
  isCurrentUserPremium,
  isInSelectMode,
  hasUnreadReaction,
  isResizingContainer,
  scrollTargetPosition,
  isAccountFrozen,
  observeIntersectionForBottom,
  observeIntersectionForLoading,
  observeIntersectionForPlaying,
  onMessageUnmount,
}: OwnProps & StateProps) => {
  const {
    requestConfetti,
    openMediaViewer,
    checkGiftCode,
    openPremiumModal,
    animateUnreadReaction,
    markMentionsRead,
    focusMessage,
  } = getActions();

  const ref = useRef<HTMLDivElement>();

  const { id, chatId } = message;
  const action = message.content?.action;
  if (!action) return undefined;
  const isLocal = isLocalMessageId(id);

  const isTextHidden = HIDDEN_TEXT_ACTIONS.has(action.type);
  const isSingleLine = SINGLE_LINE_ACTIONS.has(action.type);
  const isFluidMultiline = IS_FLUID_BACKGROUND_SUPPORTED && !isSingleLine;
  const isClickableText = action.type === 'suggestedPostSuccess';

  const messageReplyInfo = getMessageReplyInfo(message);
  const { replyToMsgId, replyToPeerId } = messageReplyInfo || {};

  const withServiceReactions = Boolean(message.areReactionsPossible && message?.reactions?.results?.length);

  const shouldSkipRender = isInsideTopic && action.type === 'topicCreate';

  const { isTouchScreen } = useAppLayout();

  useOnIntersect(ref, !shouldSkipRender ? observeIntersectionForBottom : undefined);

  useEnsureMessage(
    replyToPeerId || chatId,
    replyToMsgId,
    replyMessage,
    id,
  );
  useFocusMessageListElement({
    elementRef: ref,
    isFocused,
    focusDirection,
    noFocusHighlight,
    isResizingContainer,
    isJustAdded,
    scrollTargetPosition,
  });

  const {
    isContextMenuOpen, contextMenuAnchor,
    handleBeforeContextMenu, handleContextMenu,
    handleContextMenuClose, handleContextMenuHide,
  } = useContextMenuHandlers(
    ref,
    (isTouchScreen && isInSelectMode) || isAccountFrozen,
    !IS_TAURI,
    IS_ANDROID,
    getIsMessageListReady,
  );
  const isContextMenuShown = contextMenuAnchor !== undefined;

  const handleMouseDown = (e: React.MouseEvent<HTMLDivElement, MouseEvent>) => {
    preventMessageInputBlur(e);
    handleBeforeContextMenu(e);
  };

  const noAppearanceAnimation = appearanceOrder <= 0;
  const [isShown, markShown] = useFlag(noAppearanceAnimation);
  useEffect(() => {
    if (noAppearanceAnimation) {
      return;
    }

    setTimeout(markShown, appearanceOrder * MESSAGE_APPEARANCE_DELAY);
  }, [appearanceOrder, markShown, noAppearanceAnimation]);

  const { ref: refWithTransition } = useShowTransition({
    isOpen: isShown,
    noOpenTransition: noAppearanceAnimation,
    noCloseTransition: true,
    className: false,
    ref,
  });

  useUnmountCleanup(() => {
    onMessageUnmount?.(id);
  });

  useEffect(() => {
    const bottomMarker = ref.current;
    if (!bottomMarker || !isElementInViewport(bottomMarker)) return;

    if (hasUnreadReaction) {
      animateUnreadReaction({ chatId, messageIds: [id] });
    }

    if (message.hasUnreadMention) {
      markMentionsRead({ chatId, messageIds: [id] });
    }
  }, [hasUnreadReaction, chatId, id, animateUnreadReaction, message.hasUnreadMention]);

  useEffect(() => {
    if (action.type !== 'giftPremium') return;
    if ((memoFirstUnreadIdRef?.current && id >= memoFirstUnreadIdRef.current) || isLocal) {
      requestConfetti({});
    }
  }, [action.type, id, isLocal, memoFirstUnreadIdRef]);

  const fluidBackgroundStyle = useFluidBackgroundFilter(isFluidMultiline ? actionMessageBg : undefined);

  const handleKeyDown = useLastCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      handleClick();
    }
  });

  const handleClick = useLastCallback(() => {
    switch (action.type) {
      case 'chatEditPhoto': {
        openMediaViewer({
          chatId: message.chatId,
          messageId: message.id,
          threadId,
          origin: MediaViewerOrigin.ChannelAvatar,
        });
        break;
      }

      case 'giftCode': {
        checkGiftCode({ slug: action.slug, message: { chatId: message.chatId, messageId: message.id } });
        break;
      }

      case 'giftPremium': {
        openPremiumModal({
          isGift: true,
          fromUserId: sender?.id,
          toUserId: sender && sender.id === currentUserId ? chatId : currentUserId,
          daysAmount: action.days,
        });
        break;
      }

      case 'channelJoined': {
        break;
      }

      case 'suggestedPostApproval': {
        const replyInfo = getMessageReplyInfo(message);
        if (replyInfo?.type === 'message' && replyInfo.replyToMsgId) {
          focusMessage({
            chatId: message.chatId,
            threadId,
            messageId: replyInfo.replyToMsgId,
          });
        }
        break;
      }

      case 'suggestedPostSuccess': {
        const replyInfo = getMessageReplyInfo(message);
        if (replyInfo?.type === 'message' && replyInfo.replyToMsgId) {
          focusMessage({
            chatId: message.chatId,
            threadId,
            messageId: replyInfo.replyToMsgId,
          });
        }
        break;
      }
    }
  });

  const fullContent = useMemo(() => {
    switch (action.type) {
      case 'suggestProfilePhoto':
        return (
          <SuggestedPhoto
            message={message}
            action={action}
            observeIntersection={observeIntersectionForLoading}
          />
        );

      case 'suggestedPostApproval':
        if (action.isBalanceTooLow) {
          return (
            <SuggestedPostBalanceTooLow
              message={message}
              action={action}
              onClick={handleClick}
            />
          );
        }
        return action.isRejected ? (
          <SuggestedPostRejected
            message={message}
            action={action}
            onClick={handleClick}
          />
        ) : (
          <SuggestedPostApproval
            message={message}
            action={action}
            onClick={handleClick}
          />
        );

      default:
        return undefined;
    }
  }, [
    action, message, observeIntersectionForLoading,
  ]);

  if ((isInsideTopic && action.type === 'topicCreate') || action.type === 'phoneCall') {
    return undefined;
  }

  return (
    <div
      ref={refWithTransition}
      id={getMessageHtmlId(id)}
      className={buildClassName(
        'ActionMessage',
        'message-list-item',
        styles.root,
        isSingleLine && styles.singleLine,
        isFluidMultiline && styles.fluidMultiline,
        fullContent && styles.hasFullContent,
        isFocused && !noFocusHighlight && 'focused',
        isContextMenuShown && 'has-menu-open',
        isLastInList && 'last-in-list',
      )}
      data-message-id={message.id}
      data-is-pinned={message.isPinned || undefined}
      data-has-unread-mention={message.hasUnreadMention || undefined}
      data-has-unread-reaction={hasUnreadReaction || undefined}
      onMouseDown={handleMouseDown}
      onContextMenu={handleContextMenu}
    >
      {!isTextHidden && (
        <>
          {isFluidMultiline && (
            <div className={buildClassName(
              styles.inlineWrapper,
              isClickableText && styles.hoverable,
            )}
            >
              <span className={styles.fluidBackground} style={fluidBackgroundStyle}>
                <ActionMessageText message={message} isInsideTopic={isInsideTopic} />
              </span>
            </div>
          )}
          <div className={buildClassName(
            styles.inlineWrapper,
            isClickableText && styles.hoverable,
          )}
          >
            <span
              className={styles.textContent}
              role="button"
              tabIndex={0}
              onClick={handleClick}
              onKeyDown={handleKeyDown}
            >
              <ActionMessageText message={message} isInsideTopic={isInsideTopic} />
            </span>
          </div>
        </>
      )}
      {fullContent && (
        <div className={styles.contentWrapper}>
          {fullContent}
        </div>
      )}
      {contextMenuAnchor && (
        <ContextMenuContainer
          isOpen={isContextMenuOpen}
          anchor={contextMenuAnchor}
          message={message}
          messageListType="thread"
          className={styles.contextContainer}
          onClose={handleContextMenuClose}
          onCloseAnimationEnd={handleContextMenuHide}
        />
      )}
      {withServiceReactions && (
        <Reactions
          isOutside
          message={message}
          threadId={threadId}
          observeIntersection={observeIntersectionForPlaying}
          isCurrentUserPremium={isCurrentUserPremium}
          isAccountFrozen={isAccountFrozen}
        />
      )}
    </div>
  );
};

export default memo(withGlobal<OwnProps>(
  (global, { message, threadId }): Complete<StateProps> => {
    const tabState = selectTabState(global);

    const chat = selectChat(global, message.chatId);

    const sender = selectSender(global, message);

    const isInsideTopic = chat?.isForum && threadId !== MAIN_THREAD_ID;

    const { replyToMsgId, replyToPeerId } = getMessageReplyInfo(message) || {};
    const replyMessage = replyToMsgId
      ? selectChatMessage(global, replyToPeerId || message.chatId, replyToMsgId) : undefined;

    const isFocused = threadId ? selectIsMessageFocused(global, message, threadId) : false;
    const {
      direction: focusDirection,
      noHighlight: noFocusHighlight,
      isResizingContainer, scrollTargetPosition,
    } = (isFocused && tabState.focusedMessage) || {};

    const isCurrentUserPremium = selectIsCurrentUserPremium(global);

    const readState = selectThreadReadState(global, message.chatId, threadId);
    const hasUnreadReaction = readState?.unreadReactions?.includes(message.id);
    const isAccountFrozen = selectIsCurrentUserFrozen(global);

    return {
      sender,
      currentUserId: global.currentUserId,
      isCurrentUserPremium,
      isFocused,
      focusDirection,
      noFocusHighlight,
      isInsideTopic,
      replyMessage,
      isInSelectMode: selectIsInSelectMode(global),
      actionMessageBg: selectActionMessageBg(global),
      hasUnreadReaction,
      isResizingContainer,
      scrollTargetPosition,
      isAccountFrozen,
    };
  },
)(ActionMessage));
