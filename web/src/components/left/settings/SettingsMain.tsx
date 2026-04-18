import type { FC } from '../../../lib/teact/teact';
import { memo, useEffect } from '../../../lib/teact/teact';
import { getActions, withGlobal } from '../../../global';

import { SettingsScreens } from '../../../types';

import {
  selectIsPremiumPurchaseBlocked,
} from '../../../global/selectors';
import { callApi } from '../../../api/saturn';

import useFlag from '../../../hooks/useFlag';
import useHistoryBack from '../../../hooks/useHistoryBack';
import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import ChatExtra from '../../common/profile/ChatExtra';
import ProfileInfo from '../../common/profile/ProfileInfo';
import ConfirmDialog from '../../ui/ConfirmDialog';
import ListItem from '../../ui/ListItem';

type OwnProps = {
  isActive?: boolean;
  onReset: () => void;
};

type StateProps = {
  sessionCount: number;
  currentUserId?: string;
  canBuyPremium?: boolean;
  isSaturnAdmin?: boolean;
};

const SettingsMain: FC<OwnProps & StateProps> = ({
  isActive,
  currentUserId,
  sessionCount,
  canBuyPremium,
  isSaturnAdmin,
  onReset,
}) => {
  const {
    loadMoreProfilePhotos,
    openSupportChat,
    openSettingsScreen,
    showNotification,
  } = getActions();

  const [isSupportDialogOpen, , closeSupportDialog] = useFlag(false);

  const lang = useLang();

  useEffect(() => {
    if (currentUserId) {
      loadMoreProfilePhotos({ peerId: currentUserId, isPreload: true });
    }
  }, [currentUserId]);

  useHistoryBack({
    isActive,
    onBack: onReset,
  });

  const handleOpenSupport = useLastCallback(() => {
    openSupportChat();
    closeSupportDialog();
  });

  const handleCreateInvite = useLastCallback(async () => {
    try {
      const result = await callApi('createAuthInvite', { role: 'member', maxUses: 1 });
      if (!result) return;
      await navigator.clipboard.writeText(result.code);
      showNotification({ message: `${lang('AdminInviteCreated')}: ${result.code}` });
    } catch {
      showNotification({ message: lang('AdminInviteError') });
    }
  });

  return (
    <div className="settings-content custom-scroll">
      <div className="settings-main-menu self-profile">
        {currentUserId && (
          <ProfileInfo
            peerId={currentUserId}
            canPlayVideo={Boolean(isActive)}
            isForSettings
          />
        )}
        {currentUserId && (
          <ChatExtra
            chatOrUserId={currentUserId}
            isInSettings
          />
        )}
      </div>
      <div className="settings-main-menu">
        <ListItem
          icon="settings"
          narrow

          onClick={() => openSettingsScreen({ screen: SettingsScreens.General })}
        >
          {lang('TelegramGeneralSettingsViewController')}
        </ListItem>
        <ListItem
          icon="animations"
          narrow

          onClick={() => openSettingsScreen({ screen: SettingsScreens.Performance })}
        >
          {lang('MenuAnimations')}
        </ListItem>
        <ListItem
          icon="unmute"
          narrow

          onClick={() => openSettingsScreen({ screen: SettingsScreens.Notifications })}
        >
          {lang('Notifications')}
        </ListItem>
        <ListItem
          icon="data"
          narrow

          onClick={() => openSettingsScreen({ screen: SettingsScreens.DataStorage })}
        >
          {lang('DataSettings')}
        </ListItem>
        <ListItem
          icon="lock"
          narrow

          onClick={() => openSettingsScreen({ screen: SettingsScreens.Privacy })}
        >
          {lang('PrivacySettings')}
        </ListItem>
        <ListItem
          icon="folder"
          narrow

          onClick={() => openSettingsScreen({ screen: SettingsScreens.Folders })}
        >
          {lang('Filters')}
        </ListItem>
        <ListItem
          icon="active-sessions"
          narrow

          onClick={() => openSettingsScreen({ screen: SettingsScreens.ActiveSessions })}
        >
          {lang('SessionsTitle')}
          {sessionCount > 0 && (<span className="settings-item__current-value">{sessionCount}</span>)}
        </ListItem>
        <ListItem
          icon="language"
          narrow

          onClick={() => openSettingsScreen({ screen: SettingsScreens.Language })}
        >
          {lang('Language')}
          <span className="settings-item__current-value">{lang.languageInfo.nativeName}</span>
        </ListItem>
        <ListItem
          icon="stickers"
          narrow

          onClick={() => openSettingsScreen({ screen: SettingsScreens.Stickers })}
        >
          {lang('MenuStickers')}
        </ListItem>
        <ListItem
          icon="bots"
          narrow

          onClick={() => openSettingsScreen({ screen: SettingsScreens.AiUsage })}
        >
          {lang('AiUsageTitle')}
        </ListItem>
      </div>
      {isSaturnAdmin && (
        <div className="settings-main-menu">
          <ListItem
            icon="link"
            narrow
            onClick={handleCreateInvite}
          >
            {lang('AdminCreateInvite')}
          </ListItem>
          <ListItem
            icon="bots"
            narrow
            onClick={() => openSettingsScreen({ screen: SettingsScreens.BotManagement })}
          >
            {lang('BotManagement')}
          </ListItem>
          <ListItem
            icon="channel"
            narrow
            onClick={() => openSettingsScreen({ screen: SettingsScreens.Integrations })}
          >
            {lang('Integrations')}
          </ListItem>
        </div>
      )}
      <ConfirmDialog
        isOpen={isSupportDialogOpen}
        confirmLabel={lang('OK')}
        title={lang('AskAQuestion')}
        textParts={lang('MenuAskText', undefined, { withNodes: true, renderTextFilters: ['br'] })}
        confirmHandler={handleOpenSupport}
        onClose={closeSupportDialog}
      />
    </div>
  );
};

export default memo(withGlobal<OwnProps>(
  (global): Complete<StateProps> => {
    const { currentUserId } = global;

    return {
      sessionCount: global.activeSessions.orderedHashes.length,
      currentUserId,
      canBuyPremium: !selectIsPremiumPurchaseBlocked(global),
      isSaturnAdmin: global.saturnRole === 'admin' || global.saturnRole === 'superadmin',
    };
  },
)(SettingsMain));
