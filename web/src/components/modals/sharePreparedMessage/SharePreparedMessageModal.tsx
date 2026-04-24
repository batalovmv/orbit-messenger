import {
  type FC,
  memo, useEffect,
} from '../../../lib/teact/teact';
import {
  getActions, getGlobal,
} from '../../../global';

import type { TabState } from '../../../global/types';
import type { ThreadId } from '../../../types';
import { MAIN_THREAD_ID } from '../../../api/types';

import { getPeerTitle } from '../../../global/helpers/peers';
import {
  selectPeer,
} from '../../../global/selectors';

import useFlag from '../../../hooks/useFlag';
import useLastCallback from '../../../hooks/useLastCallback';
import useOldLang from '../../../hooks/useOldLang';

import RecipientPicker from '../../common/RecipientPicker';

export type OwnProps = {
  modal: TabState['sharePreparedMessageModal'];
};

export type SendParams = {
  peerName?: string;
  starsForSendMessage: number;
};

const SharePreparedMessageModal: FC<OwnProps> = ({
  modal,
}) => {
  const {
    closeSharePreparedMessageModal,
    sendInlineBotResult,
    sendWebAppEvent,
    showNotification,
    updateSharePreparedMessageModalSendArgs,
  } = getActions();
  const lang = useOldLang();
  const isOpen = Boolean(modal);

  const [isShown, markIsShown, unmarkIsShown] = useFlag();

  useEffect(() => {
    if (isOpen) {
      markIsShown();
    }
  }, [isOpen, markIsShown]);

  const {
    message, filter, webAppKey, pendingSendArgs,
  } = modal || {};

  const handleClose = useLastCallback(() => {
    closeSharePreparedMessageModal();
    if (webAppKey) {
      sendWebAppEvent({
        webAppKey,
        event: {
          eventType: 'prepared_message_failed',
          eventData: { error: 'USER_DECLINED' },
        },
      });
    }
  });

  const handleSend = useLastCallback((id: string, threadId?: ThreadId) => {
    if (message && webAppKey) {
      const global = getGlobal();
      const peer = selectPeer(global, id);
      sendInlineBotResult({
        chatId: id,
        threadId: threadId || MAIN_THREAD_ID,
        id: message.result.id,
        queryId: message.result.queryId,
      });
      showNotification({
        message: lang('BotSharedToOne', getPeerTitle(lang, peer!)),
      });
      sendWebAppEvent({
        webAppKey,
        event: {
          eventType: 'prepared_message_sent',
        },
      });
      closeSharePreparedMessageModal();
      updateSharePreparedMessageModalSendArgs({ args: undefined });
    }
  });

  const handleSelectRecipient = useLastCallback((id: string, threadId?: ThreadId) => {
    updateSharePreparedMessageModalSendArgs({ args: { peerId: id, threadId } });
  });

  useEffect(() => {
    if (pendingSendArgs) {
      handleSend(pendingSendArgs.peerId, pendingSendArgs.threadId);
    }
  }, [pendingSendArgs]);

  if (!isOpen && !isShown) {
    return undefined;
  }

  return (
    <RecipientPicker
      isOpen={isOpen}
      searchPlaceholder={lang('Search')}
      filter={filter}
      onSelectRecipient={handleSelectRecipient}
      onClose={handleClose}
      onCloseAnimationEnd={unmarkIsShown}
      isLowStackPriority
    />
  );
};

export default memo(SharePreparedMessageModal);
