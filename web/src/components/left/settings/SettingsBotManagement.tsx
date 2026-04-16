import { memo, useEffect, useState } from '../../../lib/teact/teact';
import { getActions } from '../../../global';

import type { SaturnBot, SaturnBotCreateResponse } from '../../../api/saturn/types';

import { copyTextToClipboard } from '../../../util/clipboard';

import useFlag from '../../../hooks/useFlag';
import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import Button from '../../ui/Button';
import ConfirmDialog from '../../ui/ConfirmDialog';
import InputText from '../../ui/InputText';
import ListItem from '../../ui/ListItem';
import Modal from '../../ui/Modal';
import RecipientPicker from '../../common/RecipientPicker';
import Spinner from '../../ui/Spinner';

import {
  createBot, deleteBot, fetchBots, installBot, rotateToken, updateBot,
} from '../../../api/saturn/methods/bots';
import { addChatMembers } from '../../../api/saturn/methods/chats';

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

  // Create form
  const [newUsername, setNewUsername] = useState('');
  const [newDisplayName, setNewDisplayName] = useState('');
  const [newDescription, setNewDescription] = useState('');

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

  const handleCreate = useLastCallback(async () => {
    if (!newUsername.trim() || !newDisplayName.trim()) return;
    try {
      const result = await createBot({
        username: newUsername.trim(),
        display_name: newDisplayName.trim(),
        description: newDescription.trim() || undefined,
      }) as SaturnBotCreateResponse;

      if (result?.bot) {
        setEditingBot({ ...result.bot, token: result.token });
        showNotification({ message: lang('BotCreated') });
        closeCreate();
        setNewUsername('');
        setNewDisplayName('');
        setNewDescription('');
        loadBots();
      }
    } catch (e) {
      showNotification({ message: String(e) });
    }
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
      showNotification({ message: String(e) });
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
      showNotification({ message: String(e) });
    }
  });

  const handleCopyToken = useLastCallback((token: string) => {
    copyTextToClipboard(token);
    showNotification({ message: lang('ExactTextCopied', token.substring(0, 20) + '...') });
  });

  const handleInstallToGroup = useLastCallback(async (chatId: string) => {
    if (!editingBot) return;
    closeGroupPicker();
    try {
      await installBot(editingBot.id, chatId, 15);
      await addChatMembers({ chatId, userIds: [editingBot.user_id] });
      showNotification({ message: lang('BotAddedToGroup') });
    } catch (e) {
      showNotification({ message: String(e) });
    }
  });

  const handleSaveBot = useLastCallback(async () => {
    if (!editingBot) return;
    try {
      await updateBot(editingBot.id, {
        display_name: editingBot.display_name,
        description: editingBot.description || undefined,
        short_description: editingBot.short_description || undefined,
        webhook_url: editingBot.webhook_url || undefined,
      });
      loadBots();
    } catch (e) {
      showNotification({ message: String(e) });
    }
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
        <div className="settings-item">
          <h4>{editingBot.username}</h4>
          <InputText
            label={lang('BotDisplayName')}
            value={editingBot.display_name}
            onChange={(e) => setEditingBot({
              ...editingBot, display_name: (e.target as HTMLInputElement).value,
            })}
          />
          <InputText
            label={lang('BotDescription')}
            value={editingBot.description || ''}
            onChange={(e) => setEditingBot({
              ...editingBot, description: (e.target as HTMLInputElement).value,
            })}
          />
          <InputText
            label={lang('BotShortDescription')}
            value={editingBot.short_description || ''}
            onChange={(e) => setEditingBot({
              ...editingBot, short_description: (e.target as HTMLInputElement).value,
            })}
          />
          <InputText
            label={lang('BotWebhookUrl')}
            value={editingBot.webhook_url || ''}
            onChange={(e) => setEditingBot({
              ...editingBot, webhook_url: (e.target as HTMLInputElement).value,
            })}
          />
          {editingBot.token && (
            <div className="settings-item">
              <p className="settings-item-description">{lang('BotToken')}</p>
              <code style="word-break: break-all; font-size: 0.75rem">{editingBot.token}</code>
              <Button size="smaller" onClick={() => handleCopyToken(editingBot.token!)}>
                Copy
              </Button>
            </div>
          )}
          <div className="settings-item-footer">
            <Button onClick={handleSaveBot}>{lang('Save')}</Button>
            <Button onClick={openGroupPicker} color="translucent">
              {lang('BotAddToGroup')}
            </Button>
            <Button onClick={() => handleRotateToken(editingBot.id)} color="translucent">
              {lang('BotRotateToken')}
            </Button>
            <Button onClick={() => handleConfirmDelete(editingBot.id)} color="danger">
              {lang('DeleteBot')}
            </Button>
            <Button onClick={handleCloseEdit} color="translucent">
              {lang('Back')}
            </Button>
          </div>
        </div>
        <RecipientPicker
          isOpen={isGroupPickerOpen}
          searchPlaceholder={lang('Search')}
          filter={['groups', 'chats']}
          onSelectRecipient={handleInstallToGroup}
          onClose={closeGroupPicker}
        />
      </div>
    );
  }

  return (
    <div className="settings-content custom-scroll">
      <div className="settings-item">
        <Button onClick={openCreate} color="primary" size="smaller">
          {lang('CreateBot')}
        </Button>
      </div>

      {isLoading && <Spinner />}

      <div className="settings-main-menu">
        {bots.map((bot) => (
          <ListItem
            key={bot.id}
            narrow
            secondaryIcon="next"
            onClick={() => handleOpenEdit(bot)}
            contextActions={[{
              title: lang('DeleteBot'),
              icon: 'delete',
              handler: () => handleConfirmDelete(bot.id),
              destructive: true,
            }]}
          >
            <span className="title">{bot.display_name}</span>
            <span className="subtitle">@{bot.username}</span>
          </ListItem>
        ))}
      </div>

      <Modal isOpen={isCreateOpen} onClose={closeCreate} title={lang('CreateBot')}>
        <div className="settings-item">
          <InputText
            label={lang('BotUsername')}
            value={newUsername}
            onChange={(e) => setNewUsername((e.target as HTMLInputElement).value)}
          />
          <InputText
            label={lang('BotDisplayName')}
            value={newDisplayName}
            onChange={(e) => setNewDisplayName((e.target as HTMLInputElement).value)}
          />
          <InputText
            label={lang('BotDescription')}
            value={newDescription}
            onChange={(e) => setNewDescription((e.target as HTMLInputElement).value)}
          />
          <Button onClick={handleCreate} color="primary">
            {lang('CreateBot')}
          </Button>
        </div>
      </Modal>

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
