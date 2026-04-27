import { memo, useEffect, useMemo } from '@teact';
import { getActions, withGlobal } from '../../../global';

import type { ApiUser } from '../../../api/types';

import { fetchAdminChatExport, fetchAdminUserExport } from '../../../api/saturn/methods/admin';
import { getUserFullName } from '../../../global/helpers';
import { selectTabState, selectUser } from '../../../global/selectors';
import buildClassName from '../../../util/buildClassName';

import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import Avatar from '../../common/Avatar';
import Modal from '../../ui/Modal';

import styles from './CompliancePanel.module.scss';

export type OwnProps = {
  isOpen?: boolean;
};

type StateProps = {
  saturnRole?: 'superadmin' | 'compliance' | 'admin' | 'member';
  selectedUserId?: string;
  selectedChatId?: string;
  selectedUser?: ApiUser;
  userIds?: string[];
  usersById: Record<string, ApiUser>;
};

const CompliancePanel = ({
  isOpen,
  saturnRole,
  selectedUserId,
  selectedChatId,
  selectedUser,
  userIds,
  usersById,
}: OwnProps & StateProps) => {
  const {
    closeCompliancePanel, selectComplianceUser, selectComplianceChat, loadContactList,
    showNotification,
  } = getActions();

  const lang = useLang();

  // Feature gate — compliance/superadmin only. Defense-in-depth; backend enforces too.
  const hasAccess = saturnRole === 'compliance' || saturnRole === 'superadmin';
  const shouldRender = Boolean(isOpen && hasAccess);

  useEffect(() => {
    if (!shouldRender) return;
    if (!userIds || userIds.length === 0) {
      loadContactList();
    }
  }, [shouldRender, userIds, loadContactList]);

  const handleClose = useLastCallback(() => {
    closeCompliancePanel();
  });

  const handleSelectUser = useLastCallback((userId: string) => {
    selectComplianceUser({ userId });
  });

  const handleBack = useLastCallback(() => {
    if (selectedChatId) {
      selectComplianceUser({ userId: selectedUserId! });
    } else if (selectedUserId) {
      selectComplianceUser({ userId: '' });
    }
  });

  const handleExportUser = useLastCallback(async () => {
    if (!selectedUserId) return;
    const res = await fetchAdminUserExport(selectedUserId);
    if (!res.ok || !res.body) {
      showNotification({ message: { key: 'ComplianceExportFailed' } });
      return;
    }
    const reader = res.body.getReader();
    const chunks: Uint8Array[] = [];
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      chunks.push(value);
    }
    // @ts-expect-error TODO(phase-8D-cleanup): Uint8Array<ArrayBufferLike> vs BlobPart variance
    const objectUrl = URL.createObjectURL(new Blob(chunks, { type: 'application/x-ndjson' }));
    const anchor = document.createElement('a');
    anchor.href = objectUrl;
    anchor.download = `user-${selectedUserId}.ndjson`;
    document.body.appendChild(anchor);
    anchor.click();
    document.body.removeChild(anchor);
    URL.revokeObjectURL(objectUrl);
  });

  const handleExportChat = useLastCallback(async () => {
    if (!selectedChatId) return;
    const res = await fetchAdminChatExport(selectedChatId);
    if (!res.ok || !res.body) {
      showNotification({ message: { key: 'ComplianceExportFailed' } });
      return;
    }
    const reader = res.body.getReader();
    const chunks: Uint8Array[] = [];
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      chunks.push(value);
    }
    // @ts-expect-error TODO(phase-8D-cleanup): Uint8Array<ArrayBufferLike> vs BlobPart variance
    const objectUrl = URL.createObjectURL(new Blob(chunks, { type: 'application/x-ndjson' }));
    const anchor = document.createElement('a');
    anchor.href = objectUrl;
    anchor.download = `chat-${selectedChatId}.ndjson`;
    document.body.appendChild(anchor);
    anchor.click();
    document.body.removeChild(anchor);
    URL.revokeObjectURL(objectUrl);
  });

  const sortedUsers = useMemo(() => {
    if (!userIds) return [];
    return userIds
      .map((id) => usersById[id])
      .filter((u): u is ApiUser => Boolean(u) && !u.isSelf)
      .sort((a, b) => {
        const nameA = getUserFullName(a) || '';
        const nameB = getUserFullName(b) || '';
        return nameA.localeCompare(nameB);
      });
  }, [userIds, usersById]);

  if (!shouldRender) return undefined;

  const showingChat = Boolean(selectedUserId && selectedChatId);
  const showingUserChats = Boolean(selectedUserId && !selectedChatId);

  return (
    <Modal
      isOpen={isOpen}
      onClose={handleClose}
      className={styles.root}
      dialogClassName={styles.dialog}
      contentClassName={styles.content}
      title={lang('CompliancePanelTitle')}
      hasCloseButton
    >
      <div className={styles.banner} role="alert">
        <i className="icon icon-lock" aria-hidden />
        <span>{lang('ComplianceBannerNotice')}</span>
      </div>

      <div className={styles.body}>
        <aside className={styles.sidebar}>
          <div className={styles.sidebarHeader}>{lang('ComplianceAllEmployees')}</div>
          <div className={styles.userList}>
            {sortedUsers.length === 0 && (
              <div className={styles.empty}>{lang('Loading')}</div>
            )}
            {sortedUsers.map((user) => (
              <button
                type="button"
                key={user.id}
                className={buildClassName(
                  styles.userRow,
                  selectedUserId === user.id && styles.userRowActive,
                )}
                onClick={() => handleSelectUser(user.id)}
              >
                <Avatar peer={user} size="small" />
                <span className={styles.userName}>
                  {getUserFullName(user) || user.usernames?.[0]?.username || user.id}
                </span>
              </button>
            ))}
          </div>
        </aside>

        <main className={styles.main}>
          {!selectedUserId && (
            <div className={styles.placeholder}>
              {lang('ComplianceSelectEmployee')}
            </div>
          )}

          {showingUserChats && (
            <div className={styles.chatPanel}>
              <header className={styles.chatPanelHeader}>
                <button type="button" className={styles.backBtn} onClick={handleBack}>
                  <i className="icon icon-back" aria-hidden />
                </button>
                <Avatar peer={selectedUser} size="small" />
                <div className={styles.chatPanelTitle}>
                  {selectedUser ? getUserFullName(selectedUser) : selectedUserId}
                </div>
              </header>
              <div className={styles.placeholder}>
                {lang('ComplianceChatsPending')}
              </div>
              <button type="button" className={styles.exportBtn} onClick={handleExportUser}>
                {lang('ComplianceExportUser')}
              </button>
            </div>
          )}

          {showingChat && (
            <div className={styles.chatPanel}>
              <header className={styles.chatPanelHeader}>
                <button type="button" className={styles.backBtn} onClick={handleBack}>
                  <i className="icon icon-back" aria-hidden />
                </button>
                <div className={styles.chatPanelTitle}>
                  {lang('ComplianceChatView')}
                </div>
              </header>
              <div className={styles.placeholder}>
                {lang('ComplianceMessagesPending')}
              </div>
              <button type="button" className={styles.exportBtn} onClick={handleExportChat}>
                {lang('ComplianceExportChat')}
              </button>
            </div>
          )}
        </main>
      </div>
    </Modal>
  );
};

export default memo(withGlobal<OwnProps>(
  (global): Complete<StateProps> => {
    const tabState = selectTabState(global);
    const panel = tabState.compliancePanel;
    const selectedUserId = panel?.userId || undefined;
    const selectedChatId = panel?.chatId || undefined;

    return {
      saturnRole: global.saturnRole,
      selectedUserId,
      selectedChatId,
      selectedUser: selectedUserId ? selectUser(global, selectedUserId) : undefined,
      userIds: global.contactList?.userIds,
      usersById: global.users.byId,
    };
  },
)(CompliancePanel));

// Export type-safe helper for the Complete<> return above.
export type { StateProps as ComplianceStateProps };
