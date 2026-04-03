import type { MouseEvent as ReactMouseEvent, TouchEvent as ReactTouchEvent } from 'react';
import {
  memo,
  useEffect,
  useRef,
  useState,
} from '../../lib/teact/teact';
import { getActions, withGlobal } from '../../global';

import type { ApiChat, ApiMessage, ApiPeer } from '../../api/types';
import type { InAppNotificationBanner } from '../../global/types';
import type { ThreadId } from '../../types';
import { MAIN_THREAD_ID } from '../../api/types';

import { getChatTitle } from '../../global/helpers/chats';
import { getMessageSenderName, getPeerFullTitle } from '../../global/helpers/peers';
import {
  selectChat,
  selectChatMessage,
  selectIsChatWithSelf,
  selectSender,
} from '../../global/selectors';
import { selectThreadIdFromMessage } from '../../global/selectors/threads';
import buildClassName from '../../util/buildClassName';
import getPointerPosition from '../../util/events/getPointerPosition';

import useFlag from '../../hooks/useFlag';
import useLang from '../../hooks/useLang';
import useLastCallback from '../../hooks/useLastCallback';

import Avatar from '../common/Avatar';
import Icon from '../common/icons/Icon';
import MessageSummary from '../common/MessageSummary';

import styles from './NotificationBanner.module.scss';

type OwnProps = {
  banner: InAppNotificationBanner;
};

type StateProps = {
  chat?: ApiChat;
  message?: ApiMessage;
  sender?: ApiPeer;
  threadId: ThreadId;
  isSelfChat: boolean;
};

const AUTO_HIDE_DELAY = 5000;
const CLOSE_ANIMATION_DURATION = 180;
const SWIPE_DISMISS_THRESHOLD = 96;

const NotificationBanner = ({
  banner,
  chat,
  message,
  sender,
  threadId,
  isSelfChat,
}: OwnProps & StateProps) => {
  const { dismissNotificationBanner, focusMessage } = getActions();

  const lang = useLang();

  const [offsetX, setOffsetX] = useState(0);
  const [isClosing, startClosing] = useFlag();
  const [isDragging, startDragging, stopDragging] = useFlag();

  const timerRef = useRef<number>();
  const originXRef = useRef(0);
  const offsetXRef = useRef(0);
  const hasMovedRef = useRef(false);

  const clearDismissTimer = useLastCallback(() => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = undefined;
    }
  });

  const handleDismiss = useLastCallback(() => {
    clearDismissTimer();
    startClosing();

    window.setTimeout(() => {
      dismissNotificationBanner({ localId: banner.localId });
    }, CLOSE_ANIMATION_DURATION);
  });

  const resetPosition = useLastCallback(() => {
    offsetXRef.current = 0;
    setOffsetX(0);
  });

  const handlePointerMove = useLastCallback((e: MouseEvent | TouchEvent) => {
    if (!isDragging) {
      return;
    }

    const { x } = getPointerPosition(e);
    const nextOffset = x - originXRef.current;

    if (Math.abs(nextOffset) >= 4) {
      hasMovedRef.current = true;
    }

    offsetXRef.current = nextOffset;
    setOffsetX(nextOffset);
  });

  const handlePointerUp = useLastCallback(() => {
    if (!isDragging) {
      return;
    }

    stopDragging();

    window.removeEventListener('mousemove', handlePointerMove);
    window.removeEventListener('touchmove', handlePointerMove);
    window.removeEventListener('mouseup', handlePointerUp);
    window.removeEventListener('touchend', handlePointerUp);
    window.removeEventListener('touchcancel', handlePointerUp);

    if (Math.abs(offsetXRef.current) >= SWIPE_DISMISS_THRESHOLD) {
      handleDismiss();
      return;
    }

    resetPosition();
  });

  const handlePointerDown = useLastCallback((e: ReactMouseEvent<HTMLDivElement> | ReactTouchEvent<HTMLDivElement>) => {
    if (isClosing) {
      return;
    }

    clearDismissTimer();
    hasMovedRef.current = false;

    const { x } = getPointerPosition(e);
    originXRef.current = x - offsetXRef.current;

    startDragging();
    window.addEventListener('mousemove', handlePointerMove);
    window.addEventListener('touchmove', handlePointerMove);
    window.addEventListener('mouseup', handlePointerUp);
    window.addEventListener('touchend', handlePointerUp);
    window.addEventListener('touchcancel', handlePointerUp);
  });

  const handleBannerClick = useLastCallback(() => {
    if (!chat || !message || isClosing || hasMovedRef.current) {
      return;
    }

    focusMessage({
      chatId: chat.id,
      threadId,
      messageId: message.id,
      shouldReplaceHistory: true,
    });

    handleDismiss();
  });

  const handleCloseButtonClick = useLastCallback((e: ReactMouseEvent<HTMLButtonElement>) => {
    e.stopPropagation();
    handleDismiss();
  });

  const handleCloseButtonPointerDown = useLastCallback((
    e: ReactMouseEvent<HTMLButtonElement> | ReactTouchEvent<HTMLButtonElement>,
  ) => {
    e.stopPropagation();
  });

  useEffect(() => {
    return () => {
      clearDismissTimer();
      window.removeEventListener('mousemove', handlePointerMove);
      window.removeEventListener('touchmove', handlePointerMove);
      window.removeEventListener('mouseup', handlePointerUp);
      window.removeEventListener('touchend', handlePointerUp);
      window.removeEventListener('touchcancel', handlePointerUp);
    };
  }, [clearDismissTimer, handlePointerMove, handlePointerUp]);

  useEffect(() => {
    if (!chat || !message || isClosing || isDragging) {
      return undefined;
    }

    timerRef.current = window.setTimeout(handleDismiss, AUTO_HIDE_DELAY);

    return clearDismissTimer;
  }, [banner.localId, chat, clearDismissTimer, handleDismiss, isClosing, isDragging, message]);

  if (!chat || !message) {
    return undefined;
  }

  const renderPeer = sender || chat;
  const chatTitle = getChatTitle(lang, chat, isSelfChat);
  const senderTitle = sender
    ? (getMessageSenderName(lang, chat.id, sender) || getPeerFullTitle(lang, sender))
    : chatTitle;
  const shouldRenderChatTitle = Boolean(senderTitle && senderTitle !== chatTitle);
  const swipeProgress = Math.min(Math.abs(offsetX) / SWIPE_DISMISS_THRESHOLD, 1);
  const opacity = isClosing ? 0 : Math.max(0.45, 1 - swipeProgress * 0.6);
  const translateY = isClosing ? -8 : 0;
  const scale = isClosing ? 0.96 : 1;

  return (
    <div
      className={buildClassName(
        styles.root,
        isDragging && styles.isDragging,
        isClosing && styles.isClosing,
      )}
      style={`transform: translateX(${offsetX}px) translateY(${translateY}px) scale(${scale}); opacity: ${opacity};`}
      onClick={handleBannerClick}
      onMouseDown={handlePointerDown}
      onTouchStart={handlePointerDown}
    >
      <div className={styles.avatar}>
        <Avatar peer={renderPeer} size="medium" />
      </div>

      <div className={styles.content}>
        <div className={styles.sender}>{senderTitle}</div>
        {shouldRenderChatTitle && (
          <div className={styles.chat}>{lang('NotificationBannerChat', { chat: chatTitle })}</div>
        )}
        <div className={styles.preview}>
          <MessageSummary message={message} truncateLength={50} noEmoji />
        </div>
      </div>

      <button
        type="button"
        aria-label={lang('AriaDismissNotificationBanner')}
        className={styles.closeButton}
        onClick={handleCloseButtonClick}
        onMouseDown={handleCloseButtonPointerDown}
        onTouchStart={handleCloseButtonPointerDown}
      >
        <Icon name="close" className={styles.closeIcon} />
      </button>
    </div>
  );
};

export default memo(withGlobal<OwnProps>(
  (global, { banner }): Complete<StateProps> => {
    const chat = selectChat(global, banner.chatId);
    const message = selectChatMessage(global, banner.chatId, banner.messageId);

    return {
      chat,
      message,
      sender: message ? selectSender(global, message) : undefined,
      threadId: message ? selectThreadIdFromMessage(global, message) : MAIN_THREAD_ID,
      isSelfChat: selectIsChatWithSelf(global, banner.chatId),
    };
  },
)(NotificationBanner));
