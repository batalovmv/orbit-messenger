import { memo, useEffect, useState } from '../../../lib/teact/teact';
import { getActions } from '../../../global';

import type { SaturnBot } from '../../../api/saturn/types';

import {
  deleteBot, fetchBots, installBot, rotateToken,
} from '../../../api/saturn/methods/bots';
import { addChatMembers } from '../../../api/saturn/methods/chats';

import useFlag from '../../../hooks/useFlag';
import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import Icon from '../../common/icons/Icon';
import RecipientPicker from '../../common/RecipientPicker';
import Button from '../../ui/Button';
import ConfirmDialog from '../../ui/ConfirmDialog';
import Spinner from '../../ui/Spinner';
import BotEditPanel from './BotEditPanel';
import BotListCard from './BotListCard';
import CreateBotWizard from './CreateBotWizard';

import styles from './BotListCard.module.scss';

type BotWithToken = SaturnBot & { token?: string };

const SettingsBotManagement = () => {
  const { showNotification } = getActions();
  const lang = useLang();

  const [bots, setBots] = useState<SaturnBot[]>([]);
  const [isLoading, markLoading, unmarkLoading] = useFlag(false);
  const [isCreateOpen, openCreate, closeCreate] = useFlag(false);
  const [isDeleteOpen, openDelete, closeDelete] = useFlag(false);
  const [editingBot, setEditingBot] = useState<BotWithToken | undefined>();
  const [deletingBotId, setDeletingBotId] = useState<string | undefined>();

  const [isGroupPickerOpen, openGroupPicker, closeGroupPicker] = useFlag(false);

  const loadBots = useLastCallback(async () => {
    markLoading();
    try {
      const result = await fetchBots();
      if (result?.data) {
        setBots(result.data);
      }
    } finally {
      unmarkLoading();
    }
  });

  useEffect(() => {
    loadBots();
  }, [loadBots]);

  const handleWizardCreated = useLastCallback((_bot: SaturnBot) => {
    showNotification({ message: lang('BotCreated') });
    loadBots();
  });

  const handleWizardInstallRequest = useLastCallback((bot: BotWithToken) => {
    setEditingBot(bot);
    openGroupPicker();
  });

  const handleDelete = useLastCallback(async () => {
    if (!deletingBotId) return;
    try {
      await deleteBot(deletingBotId);
      showNotification({ message: lang('BotDeleted') });
      closeDelete();
      setDeletingBotId(undefined);
      if (editingBot?.id === deletingBotId) {
        setEditingBot(undefined);
      }
      loadBots();
    } catch (e) {
      showNotification({ message: e instanceof Error ? e.message : String(e) });
    }
  });

  const handleRotateToken = useLastCallback(async (botId: string) => {
    try {
      const result = await rotateToken(botId);
      if (result?.token && editingBot) {
        setEditingBot({ ...editingBot, token: result.token });
        showNotification({ message: lang('TokenRotated') });
      }
    } catch (e) {
      showNotification({ message: e instanceof Error ? e.message : String(e) });
    }
  });

  const handleInstallToGroup = useLastCallback(async (chatId: string) => {
    if (!editingBot) return;
    closeGroupPicker();
    try {
      await installBot(editingBot.id, chatId, 15);
      await addChatMembers({ chatId, userIds: [editingBot.user_id] });
      showNotification({ message: lang('BotAddedToGroup') });
    } catch (e) {
      showNotification({ message: e instanceof Error ? e.message : String(e) });
    }
  });

  const handleSaved = useLastCallback((updated: SaturnBot) => {
    setEditingBot((prev) => (prev ? { ...prev, ...updated } : prev));
    loadBots();
  });

  const handleOpenEdit = useLastCallback((bot: SaturnBot) => {
    setEditingBot(bot);
  });

  const handleCloseEdit = useLastCallback(() => {
    setEditingBot(undefined);
  });

  const handleConfirmDelete = useLastCallback((botId: string) => {
    setDeletingBotId(botId);
    openDelete();
  });

  if (editingBot) {
    return (
      <div className="settings-content custom-scroll">
        <BotEditPanel
          bot={editingBot}
          onClose={handleCloseEdit}
          onSaved={handleSaved}
          onConfirmDelete={handleConfirmDelete}
          onInstallToGroup={openGroupPicker}
          onRotateToken={handleRotateToken}
        />
        <RecipientPicker
          isOpen={isGroupPickerOpen}
          searchPlaceholder={lang('Search')}
          filter={['groups', 'chats']}
          onSelectRecipient={handleInstallToGroup}
          onClose={closeGroupPicker}
        />
        <ConfirmDialog
          isOpen={isDeleteOpen}
          onClose={closeDelete}
          confirmHandler={handleDelete}
          title={lang('DeleteBot')}
          textParts={lang('AreYouSure')}
          confirmIsDestructive
        />
      </div>
    );
  }

  const showEmptyState = !isLoading && bots.length === 0;

  return (
    <div className="settings-content custom-scroll">
      {!showEmptyState && (
        <div className="settings-item">
          <Button onClick={openCreate} color="primary" size="smaller">
            {lang('CreateBot')}
          </Button>
        </div>
      )}

      {isLoading && <Spinner />}

      {showEmptyState && (
        <div className={styles.empty}>
          <span className={styles.emptyIconBox}>
            <Icon name="bot-commands-filled" />
          </span>
          <h3 className={styles.emptyTitle}>{lang('BotListEmptyTitle')}</h3>
          <p className={styles.emptySubtitle}>{lang('BotListEmptySubtitle')}</p>
          <Button color="primary" className={styles.emptyCta} onClick={openCreate}>
            {lang('BotListEmptyCta')}
          </Button>
        </div>
      )}

      {!showEmptyState && (
        <div className="settings-main-menu">
          {bots.map((bot) => (
            <BotListCard key={bot.id} bot={bot} onClick={handleOpenEdit} />
          ))}
        </div>
      )}

      <CreateBotWizard
        isOpen={isCreateOpen}
        onClose={closeCreate}
        onCreated={handleWizardCreated}
        onInstallRequest={handleWizardInstallRequest}
      />

      <ConfirmDialog
        isOpen={isDeleteOpen}
        onClose={closeDelete}
        confirmHandler={handleDelete}
        title={lang('DeleteBot')}
        textParts={lang('AreYouSure')}
        confirmIsDestructive
      />
    </div>
  );
};

export default memo(SettingsBotManagement);
