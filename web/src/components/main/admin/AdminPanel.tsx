// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import {
  memo, useEffect, useMemo, useState,
} from '@teact';
import { getActions, withGlobal } from '../../../global';

import type {
  AdminFlag, AdminInvite, AdminSession, AdminUser, AuditEntry, DefaultChatsPreview,
} from '../../../api/saturn/methods/admin';
import type { AuditQuery, PushTestReport } from '../../../api/saturn/methods/admin';
import type { GlobalState } from '../../../global/types';
import type { IconName } from '../../../types/icons';
import type { TabWithProperties } from '../../ui/TabList';

import { selectTabState } from '../../../global/selectors';
import buildClassName from '../../../util/buildClassName';
import {
  AUDIT_ACTIONS, AUDIT_EXPORT_HARD_CAP, AUDIT_TARGET_TYPES,
  backfillDefaultChats, changeAdminUserRole, createAdminInvite, deactivateAdminUser, fetchAdminFlags,
  fetchAdminInvites, fetchAdminUserExport, fetchAdminUsers, fetchAdminUserSessions, fetchAuditLog,
  fetchAuditLogExport, fetchDefaultChatsPreview, reactivateAdminUser, revokeAdminInvite,
  revokeAdminUserSession, revokeAllAdminUserSessions,
  sendAdminTestPush,
  setAdminFlag, setAdminMaintenance,
} from '../../../api/saturn/methods/admin';
import { localizeAdminError } from './adminErrors';

import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import ListItem from '../../ui/ListItem';
import Modal from '../../ui/Modal';
import Switcher from '../../ui/Switcher';
import TabList from '../../ui/TabList';
import { MaintenanceBannerView } from '../MaintenanceBanner';

import styles from './AdminPanel.module.scss';

export type OwnProps = {
  isOpen?: boolean;
};

type AdminTab = 'users' | 'flags' | 'maintenance' | 'audit' | 'welcome' | 'push';

type StateProps = {
  saturnRole?: GlobalState['saturnRole'];
  currentUserId?: string;
  tab?: AdminTab;
};

const AUDIT_PAGE_SIZE = 50;
const AUDIT_SEARCH_DEBOUNCE_MS = 300;

const tabLangKey = (tab: AdminTab) => {
  switch (tab) {
    case 'users': return 'AdminTabUsers';
    case 'flags': return 'AdminTabFeatureFlags';
    case 'maintenance': return 'AdminTabMaintenance';
    case 'audit': return 'AdminTabAuditLog';
    case 'welcome': return 'AdminTabWelcome';
    case 'push': return 'AdminTabPushInspector';
  }
};

const tabShortLangKey = (tab: AdminTab) => {
  switch (tab) {
    case 'users': return 'AdminTabUsersShort';
    case 'flags': return 'AdminTabFeatureFlagsShort';
    case 'maintenance': return 'AdminTabMaintenanceShort';
    case 'audit': return 'AdminTabAuditLogShort';
    case 'welcome': return 'AdminTabWelcomeShort';
    case 'push': return 'AdminTabPushInspectorShort';
  }
};

// flagDescription resolves the per-flag description with three priorities:
//  1. Frontend localization key `AdminFlagDesc_<flag.key>` — editorial copy
//     for the active UI locale (see fallback.strings / fallback.ru.strings).
//  2. Backend `flag.description` from pkg/featureflags/registry.go — used
//     as a fallback for unknown / future keys, so adding a flag without
//     touching the strings file still shows the operator-doc text.
//  3. undefined — caller hides the description row entirely.
//
// Missing keys make lang() return the key itself (oldLangProvider.ts:245),
// hence the explicit `localized !== key` guard. We accept the `as` cast
// because LangFn is exhaustively typed by the generated key union and we're
// constructing the key dynamically from data.
function flagDescription(
  lang: ReturnType<typeof useLang>,
  flag: AdminFlag,
): string | undefined {
  const key = `AdminFlagDesc_${flag.key}`;
  const localized = (lang as unknown as (k: string) => string)(key);
  if (localized && localized !== key) return localized;
  return flag.description || undefined;
}

// Tab visibility mirrors the backend permission split in pkg/permissions:
// admin/superadmin have SysManageSettings (flags + maintenance + welcome)
// and SysViewAuditLog (audit). compliance is audit-only — no write access
// to system settings.
const tabsForRole = (role?: GlobalState['saturnRole']): readonly AdminTab[] => {
  // Runtime audit 2026-04-29: the generic feature-flag tab had no standalone
  // actionable controls. `maintenance_mode` has a richer dedicated tab, while
  // the E2E/group-call/screen-share flags are currently not wired to product
  // behavior. Keep operators on real controls only.
  if (role === 'superadmin') return ['users', 'maintenance', 'welcome', 'push', 'audit'];
  if (role === 'admin') return ['users', 'maintenance', 'welcome', 'audit'];
  if (role === 'compliance') return ['audit'];
  return [];
};

const AdminPanel = ({
  isOpen, saturnRole, currentUserId, tab,
}: OwnProps & StateProps) => {
  const { closeAdminPanel, selectAdminTab } = getActions();
  const lang = useLang();

  const visibleTabs = useMemo(() => tabsForRole(saturnRole), [saturnRole]);
  const hasAccess = visibleTabs.length > 0;

  const activeTab: AdminTab = tab && visibleTabs.includes(tab) ? tab : visibleTabs[0] ?? 'audit';
  const activeTabIdx = Math.max(0, visibleTabs.indexOf(activeTab));

  // The shared TabList expects `id: number`. We use the index into visibleTabs
  // both as id and as the click-arg, then map back to the AdminTab string in
  // handleSwitchTab. lang() runs on every render anyway — no extra deps.
  const tabListItems: TabWithProperties[] = useMemo(
    () => visibleTabs.map((t, i) => ({
      id: i,
      title: (
        <span className={styles.tabTitle}>
          <span className={styles.tabTitleFull}>{lang(tabLangKey(t))}</span>
          <span className={styles.tabTitleCompact}>{lang(tabShortLangKey(t))}</span>
        </span>
      ),
    })),
    [visibleTabs, lang],
  );

  const handleSwitchTab = useLastCallback((idx: number) => {
    const next = visibleTabs[idx];
    if (next) selectAdminTab({ tab: next });
  });

  const handleClose = useLastCallback(() => closeAdminPanel());

  if (!hasAccess) return undefined;

  return (
    <Modal
      isOpen={isOpen}
      onClose={handleClose}
      className={styles.root}
      dialogClassName={styles.dialog}
      contentClassName={styles.content}
      title={lang('AdminPanelTitle')}
      hasCloseButton
    >
      <TabList
        tabs={tabListItems}
        activeTab={activeTabIdx}
        onSwitchTab={handleSwitchTab}
        className={styles.tabs}
      />
      <div className={styles.body}>
        {activeTab === 'users' && <UsersTab role={saturnRole} currentUserId={currentUserId} />}
        {activeTab === 'flags' && <FlagsTab />}
        {activeTab === 'maintenance' && <MaintenanceTab />}
        {activeTab === 'welcome' && <WelcomeTab role={saturnRole} />}
        {activeTab === 'push' && <PushInspectorTab />}
        {activeTab === 'audit' && <AuditTab role={saturnRole} />}
      </div>
    </Modal>
  );
};

// ===========================================================================
// Users tab
// ===========================================================================
//
// This is the daily admin workspace for a 150+ person internal messenger:
// find a user, understand their account state, block/reactivate access, adjust
// system role when allowed, and revoke stale sessions without leaving the UI.

const ADMIN_ROLE_OPTIONS = ['member', 'admin', 'compliance', 'superadmin'] as const;

type AdminRoleValue = typeof ADMIN_ROLE_OPTIONS[number];

type UsersTabProps = {
  role?: GlobalState['saturnRole'];
  currentUserId?: string;
};

type PendingUserAction =
  | { type: 'deactivate'; user: AdminUser }
  | { type: 'reactivate'; user: AdminUser }
  | { type: 'role'; user: AdminUser; nextRole: AdminRoleValue }
  | { type: 'session'; user: AdminUser; session: AdminSession }
  | { type: 'all-sessions'; user: AdminUser };

const normalizeAdminRole = (role?: string): AdminRoleValue | undefined => {
  const normalized = role?.trim().toLowerCase();
  return ADMIN_ROLE_OPTIONS.includes(normalized as AdminRoleValue)
    ? normalized as AdminRoleValue
    : undefined;
};

const roleRank = (role?: string) => {
  switch (normalizeAdminRole(role)) {
    case 'superadmin': return 4;
    case 'compliance': return 3;
    case 'admin': return 2;
    case 'member': return 1;
    default: return 0;
  }
};

const adminRoleLabel = (lang: ReturnType<typeof useLang>, role: string) => {
  switch (normalizeAdminRole(role)) {
    case 'superadmin': return lang('AdminUserRoleSuperadmin');
    case 'compliance': return lang('AdminUserRoleCompliance');
    case 'admin': return lang('AdminUserRoleAdmin');
    case 'member': return lang('AdminUserRoleMember');
    default: return role;
  }
};

const formatAdminDate = (value?: string) => {
  if (!value) return '—';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  return d.toLocaleString();
};

const shortAdminId = (id: string) => (id.length > 12 ? `${id.slice(0, 8)}…${id.slice(-4)}` : id);

const canManageAdminUser = (
  actorRole: GlobalState['saturnRole'] | undefined,
  target: AdminUser,
  currentUserId?: string,
) => {
  if (!actorRole || target.id === currentUserId) return false;
  if (actorRole === 'superadmin') return true;
  return actorRole === 'admin' && roleRank(actorRole) > roleRank(target.role);
};

const UsersTab = ({ role, currentUserId }: UsersTabProps) => {
  const lang = useLang();
  const [query, setQuery] = useState('');
  const [debouncedQuery, setDebouncedQuery] = useState('');
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [selectedUser, setSelectedUser] = useState<AdminUser | undefined>();
  const [sessions, setSessions] = useState<AdminSession[]>([]);
  const [error, setError] = useState<string | undefined>();
  const [info, setInfo] = useState<string | undefined>();
  const [isBusy, setIsBusy] = useState(false);
  const [isSessionsBusy, setIsSessionsBusy] = useState(false);
  const [pendingAction, setPendingAction] = useState<PendingUserAction | undefined>();
  const [deactivateReason, setDeactivateReason] = useState('');

  useEffect(() => {
    const t = window.setTimeout(() => setDebouncedQuery(query.trim()), AUDIT_SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(t);
  }, [query]);

  const patchUser = useLastCallback((userId: string, patch: Partial<AdminUser>) => {
    setUsers((prev) => prev.map((u) => (u.id === userId ? { ...u, ...patch } : u)));
    setSelectedUser((prev) => (prev?.id === userId ? { ...prev, ...patch } : prev));
  });

  const reloadUsers = useLastCallback(async () => {
    setIsBusy(true);
    try {
      const list = await fetchAdminUsers({ q: debouncedQuery || undefined, limit: 80 });
      setUsers(list);
      setSelectedUser((prev) => {
        if (!list.length) return undefined;
        if (prev) {
          const updated = list.find((u) => u.id === prev.id);
          if (updated) return updated;
        }
        return list[0];
      });
      setError(undefined);
    } catch (e) {
      setError(localizeAdminError(lang, e, 'load failed'));
    } finally {
      setIsBusy(false);
    }
  });

  useEffect(() => {
    reloadUsers();
  }, [debouncedQuery, reloadUsers]);

  useEffect(() => {
    const selectedUserId = selectedUser?.id;
    if (!selectedUserId) {
      setSessions([]);
      return undefined;
    }

    let cancelled = false;
    setIsSessionsBusy(true);
    fetchAdminUserSessions(selectedUserId)
      .then((list) => {
        if (cancelled) return;
        setSessions(list);
      })
      .catch((e) => {
        if (cancelled) return;
        setSessions([]);
        setError(localizeAdminError(lang, e, 'load failed'));
      })
      .finally(() => {
        if (cancelled) return;
        setIsSessionsBusy(false);
      });

    return () => {
      cancelled = true;
    };
  }, [lang, selectedUser?.id]);

  const summary = useMemo(() => ({
    total: users.length,
    active: users.filter((u) => u.is_active).length,
    inactive: users.filter((u) => !u.is_active).length,
    privileged: users.filter((u) => roleRank(u.role) > roleRank('member')).length,
  }), [users]);

  const handleOpenAction = useLastCallback((action: PendingUserAction) => {
    setDeactivateReason('');
    setPendingAction(action);
  });

  const handleCloseAction = useLastCallback(() => {
    setPendingAction(undefined);
    setDeactivateReason('');
  });

  const handleConfirmAction = useLastCallback(async () => {
    if (!pendingAction) return;
    setIsBusy(true);
    setError(undefined);
    setInfo(undefined);

    try {
      if (pendingAction.type === 'deactivate') {
        await deactivateAdminUser(pendingAction.user.id, deactivateReason.trim());
        patchUser(pendingAction.user.id, {
          is_active: false,
          deactivated_at: new Date().toISOString(),
        });
        setInfo(lang('AdminUsersDeactivateSuccess'));
      } else if (pendingAction.type === 'reactivate') {
        await reactivateAdminUser(pendingAction.user.id);
        patchUser(pendingAction.user.id, {
          is_active: true,
          deactivated_at: undefined,
          deactivated_by: undefined,
        });
        setInfo(lang('AdminUsersReactivateSuccess'));
      } else if (pendingAction.type === 'role') {
        await changeAdminUserRole(pendingAction.user.id, pendingAction.nextRole);
        patchUser(pendingAction.user.id, { role: pendingAction.nextRole });
        setInfo(lang('AdminUsersRoleChangeSuccess'));
      } else if (pendingAction.type === 'session') {
        await revokeAdminUserSession(pendingAction.user.id, pendingAction.session.id);
        setSessions((prev) => prev.filter((s) => s.id !== pendingAction.session.id));
        setInfo(lang('AdminUsersSessionRevoked'));
      } else if (pendingAction.type === 'all-sessions') {
        await revokeAllAdminUserSessions(pendingAction.user.id);
        setSessions([]);
        setInfo(lang('AdminUsersAllSessionsRevoked'));
      }
      handleCloseAction();
    } catch (e) {
      setError(localizeAdminError(lang, e, 'update failed'));
    } finally {
      setIsBusy(false);
    }
  });

  const handleExportUser = useLastCallback(async () => {
    if (!selectedUser) return;
    setIsBusy(true);
    setError(undefined);
    try {
      const res = await fetchAdminUserExport(selectedUser.id);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      const stamp = new Date().toISOString().replace(/[:.]/g, '-');
      a.href = url;
      a.download = `orbit-user-${selectedUser.id}-${stamp}.ndjson`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      window.setTimeout(() => URL.revokeObjectURL(url), 0);
    } catch (e) {
      setError(lang('AdminUsersExportFailed', { error: (e as Error).message || 'unknown' }));
    } finally {
      setIsBusy(false);
    }
  });

  const canManageSelected = selectedUser ? canManageAdminUser(role, selectedUser, currentUserId) : false;
  const canChangeRole = role === 'superadmin' && selectedUser && selectedUser.id !== currentUserId;
  const canExportSelected = role === 'superadmin';
  const selectedRoleValue = normalizeAdminRole(selectedUser?.role);

  return (
    <div className={styles.tabBody}>
      <div className={styles.usersIntro}>{lang('AdminUsersIntro')}</div>
      <div className={buildClassName(styles.summaryGrid, styles.usersSummaryGrid)}>
        <div className={styles.summaryCard}>
          <span className={styles.summaryValue}>{summary.total}</span>
          <span className={styles.summaryLabel}>{lang('AdminUsersSummaryLoaded')}</span>
        </div>
        <div className={styles.summaryCard}>
          <span className={styles.summaryValue}>{summary.active}</span>
          <span className={styles.summaryLabel}>{lang('AdminUsersSummaryActive')}</span>
        </div>
        <div className={buildClassName(styles.summaryCard, summary.inactive > 0 && styles.summaryCardWarn)}>
          <span className={styles.summaryValue}>{summary.inactive}</span>
          <span className={styles.summaryLabel}>{lang('AdminUsersSummaryInactive')}</span>
        </div>
        <div className={styles.summaryCard}>
          <span className={styles.summaryValue}>{summary.privileged}</span>
          <span className={styles.summaryLabel}>{lang('AdminUsersSummaryPrivileged')}</span>
        </div>
      </div>

      <div className={styles.formRow}>
        <input
          type="text"
          className={styles.searchInput}
          value={query}
          maxLength={200}
          placeholder={lang('AdminUsersSearchPlaceholder')}
          onChange={(e) => setQuery((e.target as HTMLInputElement).value)}
        />
      </div>

      {error && <div className={styles.error}>{error}</div>}
      {info && <div className={styles.success}>{info}</div>}

      <div className={styles.usersLayout}>
        <div className={buildClassName('settings-item', 'no-border', styles.usersList)}>
          <h4 className="settings-item-header">{lang('AdminUsersListTitle')}</h4>
          {users.length === 0 && !isBusy && (
            <div className={styles.empty}>{lang('AdminUsersEmpty')}</div>
          )}
          {users.map((user) => {
            const isSelected = selectedUser?.id === user.id;
            return (
              <ListItem
                key={user.id}
                icon={user.is_active ? 'user' : 'delete-user'}
                className={buildClassName(
                  styles.userItem,
                  isSelected && styles.userItemSelected,
                  !user.is_active && styles.userItemInactive,
                )}
                multiline
                narrow
                ripple
                onClick={() => setSelectedUser(user)}
                rightElement={(
                  <span className={buildClassName(
                    styles.statusPill,
                    user.is_active ? styles.statusPillActive : styles.statusPillInactive,
                  )}
                  >
                    {lang(user.is_active ? 'AdminUsersStatusActive' : 'AdminUsersStatusInactive')}
                  </span>
                )}
              >
                <span className="title">{user.display_name || user.email}</span>
                <span className="subtitle">
                  {user.email}
                  {' '}
                  ·
                  {adminRoleLabel(lang, user.role)}
                </span>
              </ListItem>
            );
          })}
        </div>

        <div className={styles.userDetails}>
          {!selectedUser && (
            <div className={styles.empty}>{lang(isBusy ? 'Loading' : 'AdminUsersSelectEmpty')}</div>
          )}
          {selectedUser && (
            <>
              <div className={styles.userDetailsHeader}>
                <div className={styles.userDetailsTitle}>
                  <span>{selectedUser.display_name || selectedUser.email}</span>
                  <span className={styles.userDetailsEmail}>{selectedUser.email}</span>
                </div>
                <span className={buildClassName(
                  styles.statusPill,
                  selectedUser.is_active ? styles.statusPillActive : styles.statusPillInactive,
                )}
                >
                  {lang(selectedUser.is_active ? 'AdminUsersStatusActive' : 'AdminUsersStatusInactive')}
                </span>
              </div>

              <div className={styles.userDetailsGrid}>
                <span>{lang('AdminUsersFieldId')}</span>
                <code>{selectedUser.id}</code>
                <span>{lang('AdminUsersFieldUsername')}</span>
                <span>{selectedUser.username || '—'}</span>
                <span>{lang('AdminUsersFieldRole')}</span>
                <span>{adminRoleLabel(lang, selectedUser.role)}</span>
                <span>{lang('AdminUsersFieldStatus')}</span>
                <span>{selectedUser.status || '—'}</span>
                <span>{lang('AdminUsersFieldLastSeen')}</span>
                <span>{formatAdminDate(selectedUser.last_seen_at)}</span>
                <span>{lang('AdminUsersFieldCreated')}</span>
                <span>{formatAdminDate(selectedUser.created_at)}</span>
                {!selectedUser.is_active && (
                  <>
                    <span>{lang('AdminUsersFieldDeactivated')}</span>
                    <span>{formatAdminDate(selectedUser.deactivated_at)}</span>
                  </>
                )}
              </div>

              {canChangeRole && (
                <div className={styles.formRow}>
                  <span className={styles.formLabelText}>{lang('AdminUsersRoleSelectLabel')}</span>
                  <select
                    key={`${selectedUser.id}-${selectedRoleValue || selectedUser.role}`}
                    className={styles.auditFilterSelect}
                    value={selectedRoleValue || 'member'}
                    disabled={isBusy}
                    onChange={(e) => {
                      const nextRole = (e.target as HTMLSelectElement).value as AdminRoleValue;
                      if (nextRole !== selectedRoleValue) {
                        handleOpenAction({ type: 'role', user: selectedUser, nextRole });
                      }
                    }}
                  >
                    {ADMIN_ROLE_OPTIONS.map((r) => (
                      <option key={r} value={r} selected={r === selectedRoleValue}>
                        {adminRoleLabel(lang, r)}
                      </option>
                    ))}
                  </select>
                </div>
              )}

              <div className={styles.actions}>
                {selectedUser.is_active ? (
                  <button
                    type="button"
                    className={styles.dangerBtn}
                    disabled={!canManageSelected || isBusy}
                    onClick={() => handleOpenAction({ type: 'deactivate', user: selectedUser })}
                  >
                    {lang('AdminUsersDeactivate')}
                  </button>
                ) : (
                  <button
                    type="button"
                    className={styles.primaryBtn}
                    disabled={!canManageSelected || isBusy}
                    onClick={() => handleOpenAction({ type: 'reactivate', user: selectedUser })}
                  >
                    {lang('AdminUsersReactivate')}
                  </button>
                )}
                {canExportSelected && (
                  <button
                    type="button"
                    className={styles.secondaryBtn}
                    disabled={isBusy}
                    onClick={handleExportUser}
                  >
                    {lang('AdminUsersExportData')}
                  </button>
                )}
              </div>

              <div className={styles.userSessions}>
                <div className={styles.userSessionsHeader}>
                  <div className={styles.userSessionsTitle}>
                    <span className={styles.sectionTitle}>{lang('AdminUsersSessionsTitle')}</span>
                    <span className={styles.userSearchMeta}>
                      {isSessionsBusy ? lang('Loading') : lang('AdminUsersSessionsCount', { count: sessions.length })}
                    </span>
                  </div>
                  <button
                    type="button"
                    className={styles.dangerBtn}
                    disabled={!canManageSelected || isBusy || isSessionsBusy || sessions.length === 0}
                    onClick={() => handleOpenAction({ type: 'all-sessions', user: selectedUser })}
                  >
                    {lang('AdminUsersRevokeAllSessions')}
                  </button>
                </div>
                {sessions.length === 0 && !isSessionsBusy && (
                  <div className={styles.empty}>{lang('AdminUsersSessionsEmpty')}</div>
                )}
                {sessions.map((session) => (
                  <div key={session.id} className={styles.sessionRow}>
                    <div className={styles.sessionMain}>
                      <span className={styles.sessionTitle}>
                        {session.device_id
                          ? lang('AdminUsersSessionDevice', { id: shortAdminId(session.device_id) })
                          : lang('AdminUsersSessionUnknownDevice')}
                      </span>
                      <span className={styles.userSearchMeta}>
                        {session.ip_address || '—'}
                        {' '}
                        ·
                        {formatAdminDate(session.created_at)}
                      </span>
                      {session.user_agent && (
                        <span className={styles.sessionAgent}>{session.user_agent}</span>
                      )}
                    </div>
                    <button
                      type="button"
                      className={styles.secondaryBtn}
                      disabled={!canManageSelected || isBusy}
                      onClick={() => handleOpenAction({ type: 'session', user: selectedUser, session })}
                    >
                      {lang('AdminUsersRevokeSession')}
                    </button>
                  </div>
                ))}
              </div>
            </>
          )}
        </div>
      </div>

      {pendingAction && (
        <Modal
          isOpen
          onClose={handleCloseAction}
          title={lang('AdminUsersConfirmTitle')}
          hasCloseButton={!isBusy}
        >
          <div className={styles.confirmBody}>
            {pendingAction.type === 'deactivate' && (
              <>
                <p>{lang('AdminUsersConfirmDeactivate', { user: pendingAction.user.email })}</p>
                <textarea
                  value={deactivateReason}
                  maxLength={500}
                  rows={3}
                  placeholder={lang('AdminUsersDeactivateReasonPlaceholder')}
                  onChange={(e) => setDeactivateReason((e.target as HTMLTextAreaElement).value)}
                  className={styles.formTextarea}
                />
              </>
            )}
            {pendingAction.type === 'reactivate' && (
              <p>{lang('AdminUsersConfirmReactivate', { user: pendingAction.user.email })}</p>
            )}
            {pendingAction.type === 'role' && (
              <p>
                {lang('AdminUsersConfirmRole', {
                  user: pendingAction.user.email,
                  role: adminRoleLabel(lang, pendingAction.nextRole),
                })}
              </p>
            )}
            {pendingAction.type === 'session' && (
              <p>{lang('AdminUsersConfirmRevokeSession', { user: pendingAction.user.email })}</p>
            )}
            {pendingAction.type === 'all-sessions' && (
              <p>{lang('AdminUsersConfirmRevokeAllSessions', { user: pendingAction.user.email })}</p>
            )}
          </div>
          <div className={styles.actions}>
            <button
              type="button"
              className={styles.secondaryBtn}
              disabled={isBusy}
              onClick={handleCloseAction}
            >
              {lang('Cancel')}
            </button>
            <button
              type="button"
              className={pendingAction.type === 'deactivate' || pendingAction.type === 'session'
                || pendingAction.type === 'all-sessions'
                ? styles.dangerBtn
                : styles.primaryBtn}
              disabled={isBusy}
              onClick={handleConfirmAction}
            >
              {lang(isBusy ? 'Loading' : 'AdminUsersConfirmAction')}
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
};

// ===========================================================================
// Feature Flags tab
// ===========================================================================
//
// Flags are grouped by exposure and rendered as Telegram-style settings rows:
// icon + title/description + Switcher. Dangerous flags keep the existing
// confirmation modal when toggling ON and get an extra visual warning on the
// row itself.

// Human-readable section ordering for the flag list. Maps the registry's
// `exposure` field to the localized section header operators understand.
// Adding a new exposure means adding it here AND in the translation pack.
const FLAG_SECTION_ORDER: Array<{
  exposure: AdminFlag['exposure'] | 'unknown';
  labelKey: 'AdminFlagSectionUnauth' | 'AdminFlagSectionAuth' | 'AdminFlagSectionAdmin'
    | 'AdminFlagSectionServerOnly' | 'AdminFlagSectionUnknown';
}> = [
  { exposure: 'unauth', labelKey: 'AdminFlagSectionUnauth' },
  { exposure: 'auth', labelKey: 'AdminFlagSectionAuth' },
  { exposure: 'admin', labelKey: 'AdminFlagSectionAdmin' },
  { exposure: 'server_only', labelKey: 'AdminFlagSectionServerOnly' },
  { exposure: 'unknown', labelKey: 'AdminFlagSectionUnknown' },
];

const FLAG_ICON_NAME: Record<string, IconName> = {
  e2e_dm_enabled: 'lock',
  maintenance_mode: 'tools',
  calls_group_enabled: 'phone',
  calls_screen_share_enabled: 'share-screen',
};

const flagIconName = (flag: AdminFlag): IconName => {
  return FLAG_ICON_NAME[flag.key] || (flag.dangerous ? 'warning' : 'settings');
};

const flagHumanName = (lang: ReturnType<typeof useLang>, key: string) => {
  const nameKey = `AdminFlagName_${key}`;
  const localized = (lang as unknown as (k: string) => string)(nameKey);
  if (localized && localized !== nameKey) return localized;
  return key;
};

const FlagsTab = () => {
  const lang = useLang();
  const [flags, setFlags] = useState<AdminFlag[]>([]);
  const [error, setError] = useState<string | undefined>();
  const [busyKey, setBusyKey] = useState<string | undefined>();
  const [pendingDangerous, setPendingDangerous] = useState<AdminFlag | undefined>();
  const [historyFor, setHistoryFor] = useState<AdminFlag | undefined>();

  const reload = useLastCallback(async () => {
    try {
      const list = await fetchAdminFlags();
      setFlags(list);
      setError(undefined);
    } catch (e) {
      setError(localizeAdminError(lang, e, 'load failed'));
    }
  });

  useEffect(() => {
    reload();
  }, [reload]);

  const applyToggle = useLastCallback(async (flag: AdminFlag, nextEnabled: boolean) => {
    setBusyKey(flag.key);
    try {
      const updated = await setAdminFlag(flag.key, nextEnabled, flag.metadata);
      setFlags((prev) => prev.map((f) => (f.key === updated.key ? updated : f)));
    } catch (e) {
      setError(localizeAdminError(lang, e, 'update failed'));
    } finally {
      setBusyKey(undefined);
    }
  });

  const handleToggle = useLastCallback((flag: AdminFlag) => {
    const nextEnabled = !flag.enabled;
    // Confirmation only when turning a dangerous flag ON. Turning off is
    // always safe (that's why these flags exist as dangerous in the first
    // place — they make the live system riskier when active).
    if (flag.dangerous && nextEnabled) {
      setPendingDangerous(flag);
      return;
    }
    applyToggle(flag, nextEnabled);
  });

  const handleConfirmDangerous = useLastCallback(() => {
    const target = pendingDangerous;
    if (!target) return;
    setPendingDangerous(undefined);
    applyToggle(target, true);
  });

  const handleCancelDangerous = useLastCallback(() => setPendingDangerous(undefined));
  const handleCloseHistory = useLastCallback(() => setHistoryFor(undefined));

  // Group flags by exposure so each section reads as a self-contained
  // settings card (Telegram-Web-A canonical pattern). Sections render only
  // when non-empty so the unauth-only registry today shows just two cards
  // instead of five empty stubs.
  const flagsBySection = useMemo(() => {
    const map = new Map<string, AdminFlag[]>();
    for (const f of flags) {
      const bucket = f.known ? f.exposure : 'unknown';
      if (!map.has(bucket)) map.set(bucket, []);
      map.get(bucket)!.push(f);
    }
    return map;
  }, [flags]);

  const summary = useMemo(() => {
    const enabled = flags.filter((f) => f.enabled).length;
    const dangerous = flags.filter((f) => f.dangerous && f.enabled).length;
    return { enabled, total: flags.length, dangerous };
  }, [flags]);

  return (
    <div className={styles.tabBody}>
      {error && <div className={styles.error}>{error}</div>}
      {flags.length === 0 && !error && <div className={styles.empty}>{lang('Loading')}</div>}

      {flags.length > 0 && (
        <div className={styles.flagSummary}>
          {lang('AdminFlagSummary', {
            enabled: summary.enabled,
            total: summary.total,
            dangerous: summary.dangerous,
          })}
        </div>
      )}

      {FLAG_SECTION_ORDER.map(({ exposure, labelKey }) => {
        const bucket = flagsBySection.get(exposure);
        if (!bucket || bucket.length === 0) return undefined;
        return (
          <div key={exposure} className={buildClassName('settings-item', 'no-border', styles.flagSection)}>
            <h4 className="settings-item-header">{lang(labelKey)}</h4>
            {bucket.map((flag) => {
              const desc = flagDescription(lang, flag);
              const humanName = flagHumanName(lang, flag.key);
              const isDisabled = busyKey === flag.key || !flag.known;
              return (
                <ListItem
                  key={flag.key}
                  className={buildClassName(
                    styles.flagItem,
                    flag.dangerous && styles.flagItemDangerous,
                  )}
                  icon={flagIconName(flag)}
                  multiline
                  narrow
                  ripple
                  // Unknown DB rows (no registry entry) are read-only — the
                  // backend Set() rejects them with "Unknown feature flag",
                  // so rendering a toggle would be a dead control.
                  disabled={isDisabled}
                  onClick={() => handleToggle(flag)}
                  rightElement={(
                    <Switcher
                      label={humanName}
                      checked={flag.enabled}
                      disabled={isDisabled}
                      inactive
                      onChange={() => handleToggle(flag)}
                    />
                  )}
                >
                  <span className="title">{humanName}</span>
                  <span className="subtitle" title={desc || flag.key}>
                    {desc || flag.key}
                  </span>
                </ListItem>
              );
            })}
          </div>
        );
      })}

      {pendingDangerous && (
        <Modal
          isOpen
          onClose={handleCancelDangerous}
          title={lang('AdminFlagDangerousConfirmTitle')}
          hasCloseButton
        >
          {/* Render the prompt as separate paragraphs instead of stuffing the
              flag description into the {description} placeholder. Otherwise
              localized sentences can be glued together without punctuation. */}
          <div className={styles.confirmBody}>
            <p>{lang('AdminFlagDangerousConfirmIntro', { key: pendingDangerous.key })}</p>
            {flagDescription(lang, pendingDangerous) && (
              <p className={styles.confirmDescription}>{flagDescription(lang, pendingDangerous)}</p>
            )}
            <p>{lang('AdminFlagDangerousConfirmQuestion')}</p>
          </div>
          <div className={styles.actions}>
            <button
              type="button"
              className={styles.secondaryBtn}
              onClick={handleCancelDangerous}
            >
              {lang('Cancel')}
            </button>
            <button
              type="button"
              className={styles.primaryBtn}
              onClick={handleConfirmDangerous}
            >
              {lang('AdminFlagDangerousConfirmAction')}
            </button>
          </div>
        </Modal>
      )}

      {historyFor && (
        <FlagHistoryModal flag={historyFor} onClose={handleCloseHistory} />
      )}
    </div>
  );
};

// FlagHistoryModal queries the existing audit-log endpoint with a fixed
// filter scope (target_type=feature_flag, target_id=<key>) and renders the
// last AUDIT_PAGE_SIZE entries. No backend changes — this reuses the
// pagination already exposed by /admin/audit-log.
type FlagHistoryModalProps = {
  flag: AdminFlag;
  onClose: NoneToVoidFunction;
};

const FlagHistoryModal = ({ flag, onClose }: FlagHistoryModalProps) => {
  const lang = useLang();
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [isBusy, setIsBusy] = useState(false);
  const [error, setError] = useState<string | undefined>();

  useEffect(() => {
    let cancelled = false;
    setIsBusy(true);
    fetchAuditLog({
      target_type: 'feature_flag',
      target_id: flag.key,
      limit: AUDIT_PAGE_SIZE,
    })
      .then((page) => {
        if (cancelled) return;
        setEntries(page.data);
        setError(undefined);
      })
      .catch((e) => {
        if (cancelled) return;
        setError(localizeAdminError(lang, e, 'load failed'));
      })
      .finally(() => {
        if (cancelled) return;
        setIsBusy(false);
      });
    return () => {
      cancelled = true;
    };
  }, [flag.key, lang]);

  return (
    <Modal
      isOpen
      onClose={onClose}
      title={lang('AdminFlagHistoryTitle', { key: flag.key })}
      hasCloseButton
    >
      <div className={styles.tabBody}>
        {error && <div className={styles.error}>{error}</div>}
        {isBusy && entries.length === 0 && <div className={styles.empty}>{lang('Loading')}</div>}
        {!isBusy && !error && entries.length === 0 && (
          <div className={styles.empty}>{lang('AdminFlagHistoryEmpty')}</div>
        )}
        <div className={styles.auditList}>
          {entries.map((row) => (
            <div key={row.id} className={styles.auditRow}>
              <div className={styles.auditMeta}>
                <span className={styles.auditAction}>{row.action}</span>
                <span className={styles.auditWhen}>
                  {new Date(row.created_at).toLocaleString()}
                </span>
              </div>
              <div className={styles.auditDetails}>
                <span className={styles.auditActor}>
                  {row.actor_name || row.actor_id}
                </span>
                {row.ip_address && (
                  <span className={styles.auditIp}>{row.ip_address}</span>
                )}
              </div>
              {row.details && Object.keys(row.details).length > 0 && (
                <pre className={styles.auditDump}>{JSON.stringify(row.details, undefined, 2)}</pre>
              )}
            </div>
          ))}
        </div>
        <div className={styles.actions}>
          <button
            type="button"
            className={styles.secondaryBtn}
            onClick={onClose}
          >
            {lang('AdminFlagHistoryClose')}
          </button>
        </div>
      </div>
    </Modal>
  );
};

// ===========================================================================
// Maintenance tab
// ===========================================================================
//
// Two additions on top of the basic enable / message / block_writes form:
//  1. Live preview at the top — renders MaintenanceBannerView with the
//     CURRENT form state, so the operator sees exactly what users will see
//     before clicking Apply. Reuses the same component as production.
//  2. Optional scheduled mode via two `<input type="datetime-local">`
//     fields. Empty = no bound. Backend evaluates the window at read time
//     against `time.Now()` (no migration, no sweeper) — see
//     services/messaging/internal/service/feature_flag_service.go
//     maintenanceWindowOpen.

// toDatetimeLocal converts an RFC3339 timestamp from the server into the
// "YYYY-MM-DDTHH:MM" shape that <input type="datetime-local"> expects.
// Returns '' if the input is missing or unparseable. The conversion is in
// the LOCAL browser timezone — that matches what the operator sees in the
// rest of the UI and what they'll re-submit when editing.
function toDatetimeLocal(rfc?: unknown): string {
  if (typeof rfc !== 'string' || !rfc) return '';
  const d = new Date(rfc);
  if (Number.isNaN(d.getTime())) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

// fromDatetimeLocal performs the inverse — interpreting the browser-native
// datetime-local shape ("YYYY-MM-DDTHH:MM", no timezone) as the operator's
// LOCAL clock and returning a fully-qualified RFC3339 string with the UTC
// offset baked in. This is critical: the backend sanitiser parses the
// "YYYY-MM-DDTHH:MM" form as UTC, so sending the bare string from a UTC+3
// operator would shift the window by their offset on every save. Sending
// `.toISOString()` removes that ambiguity — the server stores exactly the
// instant the operator clicked.
function fromDatetimeLocal(local: string): string {
  if (!local) return '';
  const d = new Date(local);
  if (Number.isNaN(d.getTime())) return '';
  return d.toISOString();
}

const isInviteUsable = (invite: AdminInvite) => {
  const expiresAt = invite.expires_at ? new Date(invite.expires_at).getTime() : 0;
  const isExpired = Boolean(expiresAt && expiresAt <= Date.now());
  return invite.is_active && invite.use_count < invite.max_uses && !isExpired;
};

const MaintenanceTab = () => {
  const lang = useLang();
  const [enabled, setEnabled] = useState(false);
  const [message, setMessage] = useState('');
  const [blockWrites, setBlockWrites] = useState(false);
  const [startAt, setStartAt] = useState('');
  const [endAt, setEndAt] = useState('');
  const [error, setError] = useState<string | undefined>();
  const [info, setInfo] = useState<string | undefined>();
  const [isBusy, setIsBusy] = useState(false);
  const [updatedAt, setUpdatedAt] = useState<string | undefined>();

  const reload = useLastCallback(async () => {
    try {
      const list = await fetchAdminFlags();
      const m = list.find((f) => f.key === 'maintenance_mode');
      if (!m) return;
      setEnabled(Boolean(m.enabled));
      const md = m.metadata || {};
      setMessage(typeof md.message === 'string' ? md.message : '');
      setBlockWrites(Boolean(md.block_writes));
      setStartAt(toDatetimeLocal(md.start_at));
      setEndAt(toDatetimeLocal(md.end_at));
      setUpdatedAt(m.updated_at);
    } catch (e) {
      setError(localizeAdminError(lang, e, 'load failed'));
    }
  });

  useEffect(() => {
    reload();
  }, [reload]);

  const handleApply = useLastCallback(async () => {
    setIsBusy(true);
    setError(undefined);
    setInfo(undefined);
    try {
      const flag = await setAdminMaintenance({
        enabled,
        message,
        block_writes: blockWrites,
        start_at: fromDatetimeLocal(startAt) || undefined,
        end_at: fromDatetimeLocal(endAt) || undefined,
      });
      setUpdatedAt(flag.updated_at);
      setInfo(lang('AdminMaintenanceSaved'));
    } catch (e) {
      setError(localizeAdminError(lang, e, 'save failed'));
    } finally {
      setIsBusy(false);
    }
  });

  const handleDisableQuick = useLastCallback(async () => {
    setIsBusy(true);
    setError(undefined);
    try {
      const flag = await setAdminMaintenance({ enabled: false, message: '', block_writes: false });
      setEnabled(false);
      setMessage('');
      setBlockWrites(false);
      setStartAt('');
      setEndAt('');
      setUpdatedAt(flag.updated_at);
      setInfo(lang('AdminMaintenanceDisabled'));
    } catch (e) {
      setError(localizeAdminError(lang, e, 'save failed'));
    } finally {
      setIsBusy(false);
    }
  });

  return (
    <div className={styles.tabBody}>
      <div className={styles.maintenancePreview}>
        <span className={styles.formLabelText}>{lang('AdminMaintenancePreviewTitle')}</span>
        {enabled
          ? (
            <MaintenanceBannerView
              active
              message={message}
              blockWrites={blockWrites}
            />
          )
          : (
            <div className={styles.maintenancePreviewIdle}>
              {lang('AdminMaintenancePreviewIdle')}
            </div>
          )}
      </div>
      <div className={styles.formRow}>
        <label className={styles.formLabel}>
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => setEnabled((e.target as HTMLInputElement).checked)}
          />
          <span>{lang('AdminMaintenanceEnable')}</span>
        </label>
      </div>
      <div className={styles.formRow}>
        <span className={styles.formLabelText}>{lang('AdminMaintenanceMessage')}</span>
        <textarea
          value={message}
          maxLength={500}
          rows={3}
          placeholder={lang('AdminMaintenanceMessagePlaceholder')}
          onChange={(e) => setMessage((e.target as HTMLTextAreaElement).value)}
          className={styles.formTextarea}
        />
      </div>
      <div className={styles.formRow}>
        <label className={styles.formLabel}>
          <input
            type="checkbox"
            checked={blockWrites}
            onChange={(e) => setBlockWrites((e.target as HTMLInputElement).checked)}
          />
          <span>{lang('AdminMaintenanceBlockWrites')}</span>
        </label>
        <div className={styles.formHelp}>{lang('AdminMaintenanceBlockWritesHelp')}</div>
      </div>
      <div className={styles.maintenanceWindowGrid}>
        <div className={styles.formRow}>
          <span className={styles.formLabelText}>{lang('AdminMaintenanceStartAt')}</span>
          <input
            type="datetime-local"
            className={styles.maintenanceDate}
            value={startAt}
            max={endAt || undefined}
            onChange={(e) => setStartAt((e.target as HTMLInputElement).value)}
          />
        </div>
        <div className={styles.formRow}>
          <span className={styles.formLabelText}>{lang('AdminMaintenanceEndAt')}</span>
          <input
            type="datetime-local"
            className={styles.maintenanceDate}
            value={endAt}
            min={startAt || undefined}
            onChange={(e) => setEndAt((e.target as HTMLInputElement).value)}
          />
        </div>
      </div>
      <div className={styles.formHelp}>{lang('AdminMaintenanceWindowHelp')}</div>
      {info && <div className={styles.success}>{info}</div>}
      {error && <div className={styles.error}>{error}</div>}
      {updatedAt && (
        <div className={styles.formHelp}>
          {lang('AdminMaintenanceLastUpdated')}
          :
          {new Date(updatedAt).toLocaleString()}
        </div>
      )}
      <div className={styles.actions}>
        <button
          type="button"
          className={styles.primaryBtn}
          disabled={isBusy}
          onClick={handleApply}
        >
          {lang('AdminMaintenanceApply')}
        </button>
        {enabled && (
          <button
            type="button"
            className={styles.secondaryBtn}
            disabled={isBusy}
            onClick={handleDisableQuick}
          >
            {lang('AdminMaintenanceQuickDisable')}
          </button>
        )}
      </div>
    </div>
  );
};

// ===========================================================================
// Welcome flow tab (mig 069) — Backfill default chats
// ===========================================================================
//
// The per-chat is_default_for_new_users flag lives in chat-settings UI and
// must NOT auto-trigger a backfill (a 150-user chat would suddenly appear in
// every existing user's list). This tab is the operator-driven safety net:
// click → confirm → POST /admin/default-chats/backfill → server returns the
// number of newly-inserted memberships.
type WelcomeTabProps = {
  role?: GlobalState['saturnRole'];
};

const WelcomeTab = ({ role }: WelcomeTabProps) => {
  const lang = useLang();
  const [isConfirming, setIsConfirming] = useState(false);
  const [isBusy, setIsBusy] = useState(false);
  const [isPreviewBusy, setIsPreviewBusy] = useState(true);
  const [preview, setPreview] = useState<DefaultChatsPreview | undefined>();
  const [insertedCount, setInsertedCount] = useState<number | undefined>();
  const [invites, setInvites] = useState<AdminInvite[]>([]);
  const [inviteEmail, setInviteEmail] = useState('');
  const [inviteRole, setInviteRole] = useState<AdminRoleValue>('member');
  const [inviteMaxUses, setInviteMaxUses] = useState('1');
  const [inviteExpiresAt, setInviteExpiresAt] = useState('');
  const [isInvitesBusy, setIsInvitesBusy] = useState(true);
  const [areInactiveInvitesShown, setAreInactiveInvitesShown] = useState(false);
  const [error, setError] = useState<string | undefined>();
  const [info, setInfo] = useState<string | undefined>();

  const inviteRoleOptions = useMemo(
    (): readonly AdminRoleValue[] => (role === 'superadmin' ? ADMIN_ROLE_OPTIONS : ['member']),
    [role],
  );

  const reloadPreview = useLastCallback(async () => {
    setIsPreviewBusy(true);
    try {
      const nextPreview = await fetchDefaultChatsPreview();
      setPreview(nextPreview);
      setError(undefined);
    } catch (e) {
      setError(localizeAdminError(lang, e, 'load failed'));
    } finally {
      setIsPreviewBusy(false);
    }
  });

  const reloadInvites = useLastCallback(async () => {
    setIsInvitesBusy(true);
    try {
      const list = await fetchAdminInvites();
      setInvites(list);
      setError(undefined);
    } catch (e) {
      setError(localizeAdminError(lang, e, 'load failed'));
    } finally {
      setIsInvitesBusy(false);
    }
  });

  useEffect(() => {
    reloadPreview();
    reloadInvites();
  }, [reloadPreview, reloadInvites]);

  useEffect(() => {
    if (!inviteRoleOptions.includes(inviteRole)) setInviteRole('member');
  }, [inviteRole, inviteRoleOptions]);

  const handleStart = useLastCallback(() => {
    setError(undefined);
    setInfo(undefined);
    setInsertedCount(undefined);
    setIsConfirming(true);
  });

  const handleCancel = useLastCallback(() => {
    if (!isBusy) setIsConfirming(false);
  });

  const handleConfirm = useLastCallback(async () => {
    setIsBusy(true);
    setError(undefined);
    try {
      const result = await backfillDefaultChats();
      setInsertedCount(result.inserted);
      setInfo(undefined);
      setIsConfirming(false);
      await reloadPreview();
    } catch (e) {
      setError(localizeAdminError(lang, e, 'backfill failed'));
    } finally {
      setIsBusy(false);
    }
  });

  const handleCreateInvite = useLastCallback(async () => {
    setIsBusy(true);
    setError(undefined);
    setInfo(undefined);
    try {
      const safeInviteRole = inviteRoleOptions.includes(inviteRole) ? inviteRole : 'member';
      await createAdminInvite({
        email: inviteEmail.trim() || undefined,
        role: safeInviteRole,
        max_uses: Math.max(1, Number(inviteMaxUses) || 1),
        expires_at: fromDatetimeLocal(inviteExpiresAt) || undefined,
      });
      setInviteEmail('');
      setInviteMaxUses('1');
      setInviteExpiresAt('');
      setInfo(lang('AdminInvitesCreated'));
      await reloadInvites();
    } catch (e) {
      setError(localizeAdminError(lang, e, 'invite create failed'));
    } finally {
      setIsBusy(false);
    }
  });

  const handleCopyInvite = useLastCallback(async (invite: AdminInvite) => {
    try {
      await navigator.clipboard?.writeText(invite.code);
      setInfo(lang('AdminInvitesCopied'));
      setError(undefined);
    } catch (e) {
      setError(localizeAdminError(lang, e, 'copy failed'));
    }
  });

  const handleRevokeInvite = useLastCallback(async (invite: AdminInvite) => {
    setIsBusy(true);
    setError(undefined);
    setInfo(undefined);
    try {
      await revokeAdminInvite(invite.id);
      setInvites((prev) => prev.map((item) => (
        item.id === invite.id ? { ...item, is_active: false } : item
      )));
      setInfo(lang('AdminInvitesRevoked'));
    } catch (e) {
      setError(localizeAdminError(lang, e, 'invite revoke failed'));
    } finally {
      setIsBusy(false);
    }
  });

  const defaultChats = preview?.default_chats || [];
  const sortedInvites = useMemo(
    () => [...invites].sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at)),
    [invites],
  );
  const inviteRows = useMemo(() => sortedInvites.map((invite) => ({
    invite,
    isActive: isInviteUsable(invite),
  })), [sortedInvites]);
  const visibleInviteRows = useMemo(() => (
    areInactiveInvitesShown ? inviteRows : inviteRows.filter((row) => row.isActive)
  ), [areInactiveInvitesShown, inviteRows]);
  const inactiveInviteCount = inviteRows.filter((row) => !row.isActive).length;
  const shouldDisableBackfill = isBusy || isPreviewBusy || !preview
    || defaultChats.length === 0 || preview.missing_memberships === 0;

  return (
    <div className={styles.tabBody}>
      <div className={styles.welcomeIntro}>
        {lang('AdminWelcomeIntro')}
      </div>
      <div className={styles.defaultChatBlock}>
        <div className={styles.sectionTitle}>{lang('AdminInvitesTitle')}</div>
        <div className={styles.formHelp}>{lang('AdminInvitesDescription')}</div>
        <div className={styles.inviteFormGrid}>
          <label className={styles.auditFilterField}>
            <span className={styles.formLabelText}>{lang('AdminInvitesEmail')}</span>
            <input
              type="email"
              className={styles.searchInput}
              value={inviteEmail}
              maxLength={200}
              placeholder={lang('AdminInvitesEmailPlaceholder')}
              onChange={(e) => setInviteEmail((e.target as HTMLInputElement).value)}
            />
          </label>
          <label className={styles.auditFilterField}>
            <span className={styles.formLabelText}>{lang('AdminInvitesRole')}</span>
            <select
              className={styles.auditFilterSelect}
              value={inviteRole}
              disabled={isBusy}
              onChange={(e) => setInviteRole((e.target as HTMLSelectElement).value as AdminRoleValue)}
            >
              {inviteRoleOptions.map((r) => (
                <option key={r} value={r}>
                  {adminRoleLabel(lang, r)}
                </option>
              ))}
            </select>
          </label>
          <label className={styles.auditFilterField}>
            <span className={styles.formLabelText}>{lang('AdminInvitesMaxUses')}</span>
            <input
              type="number"
              min="1"
              max="1000"
              className={styles.searchInput}
              value={inviteMaxUses}
              onChange={(e) => setInviteMaxUses((e.target as HTMLInputElement).value)}
            />
          </label>
          <label className={styles.auditFilterField}>
            <span className={styles.formLabelText}>{lang('AdminInvitesExpiresAt')}</span>
            <input
              type="datetime-local"
              className={styles.maintenanceDate}
              value={inviteExpiresAt}
              onChange={(e) => setInviteExpiresAt((e.target as HTMLInputElement).value)}
            />
          </label>
        </div>
        <div className={styles.actions}>
          <button
            type="button"
            className={styles.primaryBtn}
            disabled={isBusy}
            onClick={handleCreateInvite}
          >
            {lang(isBusy ? 'Loading' : 'AdminInvitesCreate')}
          </button>
        </div>
        {isInvitesBusy && sortedInvites.length === 0 && (
          <div className={styles.empty}>{lang('Loading')}</div>
        )}
        {!isInvitesBusy && sortedInvites.length === 0 && (
          <div className={styles.empty}>{lang('AdminInvitesEmpty')}</div>
        )}
        {!isInvitesBusy && sortedInvites.length > 0 && visibleInviteRows.length === 0 && (
          <div className={styles.empty}>{lang('AdminInvitesNoActive')}</div>
        )}
        {sortedInvites.length > 0 && (
          <div className={styles.inviteList}>
            {visibleInviteRows.map(({ invite, isActive }) => {
              return (
                <div
                  key={invite.id}
                  className={buildClassName(styles.inviteRow, !isActive && styles.inviteRowInactive)}
                >
                  <div className={styles.inviteMain}>
                    <div className={styles.inviteTitleLine}>
                      <code className={styles.inviteCode} title={invite.code}>{invite.code}</code>
                      <span className={buildClassName(
                        styles.statusPill,
                        isActive ? styles.statusPillActive : styles.statusPillInactive,
                      )}
                      >
                        {lang(isActive ? 'AdminInvitesStatusActive' : 'AdminInvitesStatusInactive')}
                      </span>
                    </div>
                    <span className={styles.inviteMeta}>
                      {invite.email || lang('AdminInvitesAllEmails')}
                      {' '}
                      ·
                      {adminRoleLabel(lang, invite.role)}
                    </span>
                    <span className={styles.inviteMeta}>
                      {lang('AdminInvitesUsage', { used: invite.use_count, max: invite.max_uses })}
                      {' '}
                      ·
                      {' '}
                      {invite.expires_at
                        ? lang('AdminInvitesExpires', { date: formatAdminDate(invite.expires_at) })
                        : lang('AdminInvitesNeverExpires')}
                    </span>
                  </div>
                  <div className={styles.inviteActions}>
                    <button
                      type="button"
                      className={styles.secondaryBtn}
                      disabled={isBusy}
                      onClick={() => handleCopyInvite(invite)}
                    >
                      {lang('AdminInvitesCopyCode')}
                    </button>
                    <button
                      type="button"
                      className={styles.dangerBtn}
                      disabled={isBusy || !isActive}
                      onClick={() => handleRevokeInvite(invite)}
                    >
                      {lang('AdminInvitesRevoke')}
                    </button>
                  </div>
                </div>
              );
            })}
          </div>
        )}
        {inactiveInviteCount > 0 && (
          <button
            type="button"
            className={styles.secondaryBtn}
            disabled={isBusy}
            onClick={() => setAreInactiveInvitesShown((value) => !value)}
          >
            {areInactiveInvitesShown
              ? lang('AdminInvitesHideInactive')
              : lang('AdminInvitesShowInactive', { count: inactiveInviteCount })}
          </button>
        )}
      </div>
      <div className={styles.welcomeBody}>
        <p>{lang('AdminWelcomeBackfillDescription')}</p>
        <p className={styles.welcomeWarn}>{lang('AdminWelcomeBackfillWarning')}</p>
      </div>
      <div className={styles.summaryGrid}>
        <div className={styles.summaryCard}>
          <span className={styles.summaryValue}>{isPreviewBusy && !preview ? '...' : defaultChats.length}</span>
          <span className={styles.summaryLabel}>{lang('AdminWelcomeDefaultChatsCount')}</span>
        </div>
        <div className={styles.summaryCard}>
          <span className={styles.summaryValue}>{isPreviewBusy && !preview ? '...' : preview?.user_count ?? 0}</span>
          <span className={styles.summaryLabel}>{lang('AdminWelcomeUsersCount')}</span>
        </div>
        <div
          className={buildClassName(
            styles.summaryCard,
            (preview?.missing_memberships || 0) > 0 && styles.summaryCardWarn,
          )}
        >
          <span className={styles.summaryValue}>
            {isPreviewBusy && !preview ? '...' : preview?.missing_memberships ?? 0}
          </span>
          <span className={styles.summaryLabel}>{lang('AdminWelcomeMissingCount')}</span>
        </div>
      </div>
      <div className={styles.defaultChatBlock}>
        <div className={styles.sectionTitle}>{lang('AdminWelcomeDefaultChatsTitle')}</div>
        {isPreviewBusy && defaultChats.length === 0 && (
          <div className={styles.empty}>{lang('Loading')}</div>
        )}
        {!isPreviewBusy && defaultChats.length === 0 && (
          <>
            <div className={styles.empty}>{lang('AdminWelcomeNoDefaultChats')}</div>
            <div className={styles.welcomeWarn}>{lang('AdminWelcomeNoDefaultChatsImpact')}</div>
          </>
        )}
        {defaultChats.length > 0 && (
          <div className={styles.defaultChatList}>
            {defaultChats.map((chat) => (
              <div key={chat.id} className={styles.defaultChatRow}>
                <div className={styles.defaultChatMain}>
                  <span className={styles.defaultChatName}>
                    {chat.name || lang('AdminWelcomeUnnamedChat')}
                  </span>
                  <span className={styles.defaultChatMeta}>
                    {lang('AdminWelcomeChatMeta', {
                      type: chat.type,
                      members: chat.member_count,
                      order: chat.default_join_order,
                    })}
                  </span>
                </div>
              </div>
            ))}
          </div>
        )}
        {preview && defaultChats.length > 0 && preview.missing_memberships === 0 && (
          <div className={styles.success}>{lang('AdminWelcomeNoMissingMemberships')}</div>
        )}
      </div>
      {insertedCount !== undefined && (
        <div className={styles.success}>
          {lang('AdminWelcomeBackfillResult', { count: insertedCount })}
        </div>
      )}
      {error && <div className={styles.error}>{error}</div>}
      {info && <div className={styles.success}>{info}</div>}
      <div className={styles.actions}>
        <button
          type="button"
          className={styles.primaryBtn}
          disabled={shouldDisableBackfill}
          onClick={handleStart}
        >
          {lang(isPreviewBusy ? 'Loading' : 'AdminWelcomeBackfillButton')}
        </button>
      </div>

      {isConfirming && (
        <Modal
          isOpen={isConfirming}
          onClose={handleCancel}
          title={lang('AdminWelcomeBackfillConfirmTitle')}
          hasCloseButton={!isBusy}
        >
          <div className={styles.confirmBody}>
            <p>
              {lang('AdminWelcomeBackfillConfirmBody', {
                chats: defaultChats.length,
                users: preview?.user_count ?? 0,
                missing: preview?.missing_memberships ?? 0,
              })}
            </p>
          </div>
          <div className={styles.actions}>
            <button
              type="button"
              className={styles.secondaryBtn}
              disabled={isBusy}
              onClick={handleCancel}
            >
              {lang('Cancel')}
            </button>
            <button
              type="button"
              className={styles.primaryBtn}
              disabled={isBusy}
              onClick={handleConfirm}
            >
              {lang(isBusy ? 'Loading' : 'AdminWelcomeBackfillConfirmAction')}
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
};

// ===========================================================================
// Push Inspector tab (Day 5.1)
// ===========================================================================
//
// Lets admins debug "user says pushes aren't arriving" without ssh'ing into
// the gateway. Looks up by email or UUID, fires one test push, displays
// per-device delivery status (ok/fail/stale) with provider host suffixes.
// Stale entries are auto-deleted server-side; clicking again refreshes.
const PushInspectorTab = () => {
  const lang = useLang();
  const [identifier, setIdentifier] = useState('');
  const [userResults, setUserResults] = useState<AdminUser[]>([]);
  const [selectedUser, setSelectedUser] = useState<AdminUser | undefined>();
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [report, setReport] = useState<PushTestReport | undefined>();
  const [error, setError] = useState<string | undefined>();
  const [isBusy, setIsBusy] = useState(false);
  const [isSearchingUsers, setIsSearchingUsers] = useState(false);

  useEffect(() => {
    const query = identifier.trim();
    if (selectedUser || query.length < 2) {
      setUserResults([]);
      setIsSearchingUsers(false);
      return undefined;
    }

    let cancelled = false;
    setIsSearchingUsers(true);
    const timeout = window.setTimeout(() => {
      fetchAdminUsers({ q: query, limit: 8 })
        .then((users) => {
          if (cancelled) return;
          setUserResults(users);
          setError(undefined);
        })
        .catch((e) => {
          if (cancelled) return;
          setUserResults([]);
          setError(localizeAdminError(lang, e, 'search failed'));
        })
        .finally(() => {
          if (cancelled) return;
          setIsSearchingUsers(false);
        });
    }, AUDIT_SEARCH_DEBOUNCE_MS);

    return () => {
      cancelled = true;
      window.clearTimeout(timeout);
    };
  }, [identifier, lang, selectedUser]);

  const handleIdentifierChange = useLastCallback((value: string) => {
    setIdentifier(value);
    setSelectedUser(undefined);
    setReport(undefined);
  });

  const handleSelectUser = useLastCallback((user: AdminUser) => {
    setSelectedUser(user);
    setIdentifier(user.display_name || user.email);
    setUserResults([]);
    setError(undefined);
  });

  const handleSend = useLastCallback(async () => {
    setError(undefined);
    setReport(undefined);
    setIsBusy(true);
    try {
      const isEmail = identifier.includes('@');
      const result = await sendAdminTestPush({
        user_id: selectedUser ? selectedUser.id : isEmail ? undefined : identifier.trim() || undefined,
        email: selectedUser ? undefined : isEmail ? identifier.trim() : undefined,
        title: title || undefined,
        body: body || undefined,
      });
      setReport(result);
    } catch (e) {
      setError(localizeAdminError(lang, e, 'send failed'));
    } finally {
      setIsBusy(false);
    }
  });

  return (
    <div className={styles.tabBody}>
      <div className={styles.welcomeIntro}>
        {lang('AdminPushInspectorIntro')}
      </div>
      <div className={styles.formRow}>
        <span className={styles.formLabelText}>{lang('AdminPushInspectorIdentifier')}</span>
        <input
          type="text"
          className={styles.searchInput}
          value={identifier}
          maxLength={200}
          placeholder={lang('AdminPushInspectorIdentifierPlaceholder')}
          onChange={(e) => handleIdentifierChange((e.target as HTMLInputElement).value)}
        />
        {isSearchingUsers && (
          <div className={styles.formHelp}>{lang('Loading')}</div>
        )}
        {!isSearchingUsers && identifier.trim().length >= 2 && !selectedUser && userResults.length === 0 && (
          <div className={styles.formHelp}>{lang('AdminPushInspectorSearchEmpty')}</div>
        )}
        {userResults.length > 0 && (
          <div className={styles.userSearchResults}>
            {userResults.map((user) => (
              <button
                key={user.id}
                type="button"
                className={styles.userSearchResult}
                onClick={() => handleSelectUser(user)}
              >
                <span className={styles.userSearchName}>{user.display_name || user.email}</span>
                <span className={styles.userSearchMeta}>
                  {user.email}
                  {' '}
                  ·
                  {user.role}
                  {user.is_active ? '' : ` · ${lang('AdminPushInspectorUserInactive')}`}
                </span>
              </button>
            ))}
          </div>
        )}
        {selectedUser && (
          <div className={styles.selectedUser}>
            <span className={styles.formLabelText}>{lang('AdminPushInspectorSelectedUser')}</span>
            <span>{selectedUser.display_name || selectedUser.email}</span>
            <span className={styles.userSearchMeta}>
              {selectedUser.email}
              {' '}
              ·
              {' '}
              {selectedUser.id}
            </span>
          </div>
        )}
      </div>
      <div className={styles.formRow}>
        <span className={styles.formLabelText}>{lang('AdminPushInspectorTitle')}</span>
        <input
          type="text"
          className={styles.searchInput}
          value={title}
          maxLength={200}
          placeholder={lang('AdminPushInspectorTitlePlaceholder')}
          onChange={(e) => setTitle((e.target as HTMLInputElement).value)}
        />
      </div>
      <div className={styles.formRow}>
        <span className={styles.formLabelText}>{lang('AdminPushInspectorBody')}</span>
        <textarea
          value={body}
          maxLength={1000}
          rows={2}
          placeholder={lang('AdminPushInspectorBodyPlaceholder')}
          onChange={(e) => setBody((e.target as HTMLTextAreaElement).value)}
          className={styles.formTextarea}
        />
      </div>
      <div className={styles.actions}>
        <button
          type="button"
          className={styles.primaryBtn}
          disabled={isBusy || (!selectedUser && !identifier.trim())}
          onClick={handleSend}
        >
          {lang(isBusy ? 'Loading' : 'AdminPushInspectorSend')}
        </button>
      </div>
      {error && <div className={styles.error}>{error}</div>}
      {report && (
        <div className={styles.pushReport}>
          <div className={styles.pushSummary}>
            <span>
              {lang('AdminPushInspectorTarget')}
              :
              {report.email || report.user_id}
              {report.display_name ? ` (${report.display_name})` : ''}
            </span>
            <span className={styles.pushCounts}>
              {lang('AdminPushInspectorCountSent', { count: report.sent })}
              {' · '}
              {lang('AdminPushInspectorCountFailed', { count: report.failed })}
              {' · '}
              {lang('AdminPushInspectorCountStale', { count: report.stale })}
            </span>
          </div>
          {report.devices.length === 0 && (
            <div className={styles.empty}>{lang('AdminPushInspectorNoDevices')}</div>
          )}
          {report.devices.length > 0 && (
            <ul className={styles.pushDeviceList}>
              {report.devices.map((d) => {
                const statusClass = d.status === 'ok'
                  ? styles.pushStatusOk
                  : d.status === 'fail'
                    ? styles.pushStatusFail
                    : styles.pushStatusStale;
                return (
                  <li key={d.device_id} className={buildClassName(styles.pushDeviceRow, statusClass)}>
                    <div className={styles.pushDeviceHead}>
                      <span className={styles.pushDeviceStatus}>{d.status.toUpperCase()}</span>
                      <span className={styles.pushDeviceHost}>{d.endpoint_host}</span>
                    </div>
                    {d.user_agent && (
                      <div className={styles.pushDeviceUA}>{d.user_agent}</div>
                    )}
                    {d.error && (
                      <div className={styles.pushDeviceError}>{d.error}</div>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      )}
    </div>
  );
};

// ===========================================================================
// Audit log tab
// ===========================================================================
//
// Filters compose AND-style server-side: q (free-text ILIKE) on top of the
// dropdown-bound action / target_type / actor_id / from-to date filters.
// Anything left blank is omitted from the request. Date inputs use native
// <input type="date"> (YYYY-MM-DD); the backend accepts that shape and
// interprets it as UTC midnight, so a "from 2026-04-01 to 2026-04-29" range
// includes both endpoints.
//
// The Export button is hidden for `admin` — the role has SysViewAuditLog
// but not SysExportData, so the server would 403. Compliance + superadmin
// see the button; clicking streams a CSV download with the same filter set.

type AuditTabProps = {
  role?: GlobalState['saturnRole'];
};

const canExportAudit = (role?: GlobalState['saturnRole']) => (
  role === 'compliance' || role === 'superadmin'
);

// AUDIT_FILTER_ALL is the sentinel value for "no filter" on the action /
// target_type dropdowns. We CANNOT use the empty string here — Teact drops
// `value=""` from rendered <option> attributes, which makes the browser fall
// back to option.text (the localized label) for `option.value`. That value
// would then be sent as an invalid action filter
// and the backend whitelist would reject it with 400 "unknown action". Use
// a non-empty sentinel and convert to '' before request-building.
const AUDIT_FILTER_ALL = '__all__';

const auditActionLabel = (lang: ReturnType<typeof useLang>, action: string) => {
  switch (action) {
    case 'chat.privileged_read': return lang('AdminAuditActionChatPrivilegedRead');
    case 'user.deactivate': return lang('AdminAuditActionUserDeactivate');
    case 'user.reactivate': return lang('AdminAuditActionUserReactivate');
    case 'user.role_change': return lang('AdminAuditActionUserRoleChange');
    case 'user.sessions_revoked': return lang('AdminAuditActionUserSessionsRevoked');
    case 'invite.create': return lang('AdminAuditActionInviteCreate');
    case 'invite.revoke': return lang('AdminAuditActionInviteRevoke');
    case 'audit.view': return lang('AdminAuditActionAuditView');
    case 'audit.export': return lang('AdminAuditActionAuditExport');
    case 'user.list_read': return lang('AdminAuditActionUserListRead');
    case 'data.export': return lang('AdminAuditActionDataExport');
    case 'feature_flag.list': return lang('AdminAuditActionFeatureFlagList');
    case 'feature_flag.set': return lang('AdminAuditActionFeatureFlagSet');
    case 'maintenance.enable': return lang('AdminAuditActionMaintenanceEnable');
    case 'maintenance.update': return lang('AdminAuditActionMaintenanceUpdate');
    case 'maintenance.disable': return lang('AdminAuditActionMaintenanceDisable');
    case 'chat.default_status_set': return lang('AdminAuditActionChatDefaultStatusSet');
    case 'default_chats.backfill': return lang('AdminAuditActionDefaultChatsBackfill');
    case 'push.test_sent': return lang('AdminAuditActionPushTestSent');
    default: return action;
  }
};

const auditTargetTypeLabel = (lang: ReturnType<typeof useLang>, targetType: string) => {
  switch (targetType) {
    case 'system': return lang('AdminAuditTargetSystem');
    case 'user': return lang('AdminAuditTargetUser');
    case 'chat': return lang('AdminAuditTargetChat');
    case 'message': return lang('AdminAuditTargetMessage');
    case 'feature_flag': return lang('AdminAuditTargetFeatureFlag');
    default: return targetType;
  }
};

const auditTargetLabel = (lang: ReturnType<typeof useLang>, row: AuditEntry) => {
  const target = auditTargetTypeLabel(lang, row.target_type);
  return row.target_id ? `${target} · ${row.target_id}` : target;
};

const detailString = (details: Record<string, unknown> | undefined, key: string) => {
  const value = details?.[key];
  return typeof value === 'string' ? value : undefined;
};

const detailNumber = (details: Record<string, unknown> | undefined, key: string) => {
  const value = details?.[key];
  return typeof value === 'number' ? value : undefined;
};

const detailBoolean = (details: Record<string, unknown> | undefined, key: string) => {
  const value = details?.[key];
  return typeof value === 'boolean' ? value : undefined;
};

const detailDate = (details: Record<string, unknown> | undefined, key: string) => {
  const value = detailString(details, key);
  if (!value) return undefined;
  const time = Date.parse(value);
  if (!Number.isFinite(time)) return value;
  return new Date(time).toLocaleString();
};

const formatDetailValue = (value: unknown) => {
  if (typeof value === 'boolean') return value ? 'true' : 'false';
  if (typeof value === 'number') return String(value);
  if (typeof value === 'string') return value;
  return undefined;
};

const auditDetailKeyLabel = (lang: ReturnType<typeof useLang>, key: string) => {
  switch (key) {
    case 'reason': return lang('AdminAuditDetailKeyReason');
    case 'old_role': return lang('AdminAuditDetailKeyOldRole');
    case 'new_role': return lang('AdminAuditDetailKeyNewRole');
    case 'target_user_id': return lang('AdminAuditDetailKeyTargetUser');
    case 'device_count': return lang('AdminAuditDetailKeyDevices');
    case 'sent': return lang('AdminAuditDetailKeySent');
    case 'failed': return lang('AdminAuditDetailKeyFailed');
    case 'stale': return lang('AdminAuditDetailKeyStale');
    case 'title': return lang('AdminAuditDetailKeyTitle');
    case 'is_default': return lang('AdminAuditDetailKeyDefaultChat');
    case 'default_join_order': return lang('AdminAuditDetailKeyJoinOrder');
    case 'key': return lang('AdminAuditDetailKeyFlag');
    case 'prev': return lang('AdminAuditDetailKeyPrevious');
    case 'next': return lang('AdminAuditDetailKeyNext');
    case 'format': return lang('AdminAuditDetailKeyFormat');
    case 'count': return lang('AdminAuditDetailKeyCount');
    case 'hard_cap': return lang('AdminAuditDetailKeyHardCap');
    case 'action': return lang('AdminAuditDetailKeyAction');
    case 'target_type': return lang('AdminAuditDetailKeyTargetType');
    case 'target_id': return lang('AdminAuditDetailKeyTargetId');
    case 'actor_id': return lang('AdminAuditDetailKeyActorId');
    case 'since': return lang('AdminAuditDetailKeySince');
    case 'until': return lang('AdminAuditDetailKeyUntil');
    case 'q': return lang('AdminAuditDetailKeyQuery');
    default: return key;
  }
};

const auditDetailValueLabel = (lang: ReturnType<typeof useLang>, key: string, value: unknown) => {
  if (key === 'prev' || key === 'next' || key === 'is_default') {
    return value === true ? lang('AdminAuditEnabled') : value === false ? lang('AdminAuditDisabled') : undefined;
  }
  if (key === 'old_role' || key === 'new_role') {
    const role = formatDetailValue(value);
    return role ? adminRoleLabel(lang, role) : undefined;
  }
  if (key === 'action') {
    const action = formatDetailValue(value);
    return action ? auditActionLabel(lang, action) : undefined;
  }
  if (key === 'target_type') {
    const targetType = formatDetailValue(value);
    return targetType ? auditTargetTypeLabel(lang, targetType) : undefined;
  }
  if (key === 'since' || key === 'until') {
    const asDate = typeof value === 'string' ? Date.parse(value) : Number.NaN;
    return Number.isFinite(asDate) ? new Date(asDate).toLocaleString() : formatDetailValue(value);
  }
  return formatDetailValue(value);
};

const auditDetailEntries = (lang: ReturnType<typeof useLang>, row: AuditEntry) => {
  const { details } = row;
  if (!details) return [];
  const preferredKeys = [
    'reason', 'old_role', 'new_role', 'target_user_id', 'device_count', 'sent', 'failed', 'stale', 'title',
    'is_default', 'default_join_order', 'key', 'prev', 'next', 'format', 'count', 'hard_cap',
    'action', 'target_type', 'target_id', 'actor_id', 'since', 'until', 'q',
  ];
  const orderedKeys = [
    ...preferredKeys.filter((key) => Object.prototype.hasOwnProperty.call(details, key)),
    ...Object.keys(details).filter((key) => !preferredKeys.includes(key)),
  ];

  return orderedKeys
    .map((key) => {
      const value = auditDetailValueLabel(lang, key, details[key]);
      return value ? { key, label: auditDetailKeyLabel(lang, key), value } : undefined;
    })
    .filter(Boolean);
};

const auditDetailsSummary = (lang: ReturnType<typeof useLang>, row: AuditEntry) => {
  const { details } = row;
  if (!details || Object.keys(details).length === 0) {
    if (row.action === 'user.reactivate') return lang('AdminAuditDetailUserReactivate');
    if (row.action === 'audit.view') return lang('AdminAuditDetailAuditView');
    if (row.action === 'default_chats.backfill') return lang('AdminAuditDetailBackfillStarted');
    return undefined;
  }

  if (row.action === 'user.deactivate') {
    const reason = detailString(details, 'reason');
    return reason
      ? lang('AdminAuditDetailUserDeactivateReason', { reason })
      : lang('AdminAuditDetailUserDeactivate');
  }
  if (row.action === 'user.role_change') {
    return lang('AdminAuditDetailRoleChange', {
      oldRole: adminRoleLabel(lang, detailString(details, 'old_role') || 'member'),
      newRole: adminRoleLabel(lang, detailString(details, 'new_role') || 'member'),
    });
  }
  if (row.action === 'user.sessions_revoked') {
    return lang('AdminAuditDetailSessionsRevoked');
  }
  if (row.action === 'invite.create') {
    return lang('AdminAuditDetailInviteCreate');
  }
  if (row.action === 'invite.revoke') {
    return lang('AdminAuditDetailInviteRevoke');
  }
  if (row.action === 'audit.view') {
    return lang('AdminAuditDetailAuditView');
  }
  if (row.action === 'push.test_sent') {
    const title = detailString(details, 'title');
    return lang('AdminAuditDetailPushTest', {
      devices: detailNumber(details, 'device_count') ?? 0,
      sent: detailNumber(details, 'sent') ?? 0,
      failed: detailNumber(details, 'failed') ?? 0,
      stale: detailNumber(details, 'stale') ?? 0,
      title: title || lang('AdminAuditDetailNoTitle'),
    });
  }
  if (row.action === 'chat.default_status_set') {
    const isDefault = detailBoolean(details, 'is_default');
    return lang('AdminAuditDetailDefaultChat', {
      status: isDefault ? lang('AdminAuditEnabled') : lang('AdminAuditDisabled'),
      order: detailNumber(details, 'default_join_order') ?? 0,
    });
  }
  if (row.action === 'feature_flag.set'
    || row.action === 'maintenance.enable'
    || row.action === 'maintenance.update'
    || row.action === 'maintenance.disable') {
    const enabled = detailBoolean(details, 'next');
    if (enabled !== undefined) {
      return lang('AdminAuditDetailFlagState', {
        state: enabled ? lang('AdminAuditEnabled') : lang('AdminAuditDisabled'),
      });
    }
  }
  if (row.action === 'user.list_read' || row.action === 'chat.privileged_read') {
    return lang('AdminAuditDetailCount', { count: detailNumber(details, 'count') ?? 0 });
  }
  if (row.action === 'data.export') {
    return lang('AdminAuditDetailExportFormat', { format: detailString(details, 'format') || '?' });
  }
  if (row.action === 'audit.export') {
    const since = detailDate(details, 'since') || lang('AdminAuditDetailAnyTime');
    const until = detailDate(details, 'until') || lang('AdminAuditDetailAnyTime');
    return lang('AdminAuditDetailAuditExport', {
      cap: detailNumber(details, 'hard_cap') ?? 0,
      since,
      until,
    });
  }

  return lang('AdminAuditDetailTechnical');
};

const AuditTab = ({ role }: AuditTabProps) => {
  const lang = useLang();
  const [q, setQ] = useState('');
  const [debouncedQ, setDebouncedQ] = useState('');
  const [actionFilter, setActionFilter] = useState('');
  const [targetTypeFilter, setTargetTypeFilter] = useState('');
  const [actorIdFilter, setActorIdFilter] = useState('');
  const [fromDate, setFromDate] = useState('');
  const [toDate, setToDate] = useState('');
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [cursor, setCursor] = useState<string | undefined>();
  const [hasMore, setHasMore] = useState(false);
  const [error, setError] = useState<string | undefined>();
  const [isBusy, setIsBusy] = useState(false);
  const [isExporting, setIsExporting] = useState(false);

  useEffect(() => {
    const t = window.setTimeout(() => setDebouncedQ(q), AUDIT_SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(t);
  }, [q]);

  // Resolve the active filter into the AuditQuery shape sent to backend. Empty
  // strings are dropped via buildAuditParams; we still translate them here so
  // useEffect dep tracking is on real values.
  const buildQuery = useLastCallback((cursorVal?: string): AuditQuery => ({
    q: debouncedQ || undefined,
    action: actionFilter || undefined,
    target_type: targetTypeFilter || undefined,
    actor_id: actorIdFilter.trim() || undefined,
    since: fromDate || undefined,
    until: toDate || undefined,
    cursor: cursorVal,
    limit: AUDIT_PAGE_SIZE,
  }));

  const load = useLastCallback(async (next?: string) => {
    setIsBusy(true);
    try {
      const page = await fetchAuditLog(buildQuery(next));
      if (next) {
        setEntries((prev) => [...prev, ...page.data]);
      } else {
        setEntries(page.data);
      }
      setCursor(page.cursor);
      setHasMore(page.has_more);
      setError(undefined);
    } catch (e) {
      setError(localizeAdminError(lang, e, 'load failed'));
    } finally {
      setIsBusy(false);
    }
  });

  // Refetch from scratch whenever any of the filter inputs change. Free-text
  // is the only one debounced — the rest fire on the next tick.
  useEffect(() => {
    load(undefined);
  }, [
    debouncedQ, actionFilter, targetTypeFilter, actorIdFilter, fromDate, toDate, load,
  ]);

  const handleResetFilters = useLastCallback(() => {
    setQ('');
    setActionFilter('');
    setTargetTypeFilter('');
    setActorIdFilter('');
    setFromDate('');
    setToDate('');
  });

  const handleExport = useLastCallback(async () => {
    setIsExporting(true);
    setError(undefined);
    try {
      const res = await fetchAuditLogExport({
        q: debouncedQ || undefined,
        action: actionFilter || undefined,
        target_type: targetTypeFilter || undefined,
        actor_id: actorIdFilter.trim() || undefined,
        since: fromDate || undefined,
        until: toDate || undefined,
      });
      if (!res.ok) {
        const status = res.status;
        if (status === 403) {
          throw new Error(lang('AdminAuditExportForbidden'));
        }
        throw new Error(`HTTP ${status}`);
      }
      const blob = await res.blob();
      // Trigger a regular browser download. Chrome/Firefox/Safari all honour
      // download attribute on a same-origin blob URL; revoke the URL after
      // the click to release memory once the download starts.
      const url = URL.createObjectURL(blob);
      const stamp = new Date().toISOString().replace(/[:.]/g, '-');
      const a = document.createElement('a');
      a.href = url;
      a.download = `orbit-audit-${stamp}.csv`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      // Defer revoke until the next tick so the browser has the URL committed.
      window.setTimeout(() => URL.revokeObjectURL(url), 0);
    } catch (e) {
      setError(lang('AdminAuditExportFailed', { error: (e as Error).message || 'unknown' }));
    } finally {
      setIsExporting(false);
    }
  });

  const rows = useMemo(() => entries, [entries]);

  return (
    <div className={styles.tabBody}>
      <div className={styles.auditFilterGrid}>
        <div className={styles.auditFilterField}>
          <span className={styles.formLabelText}>{lang('AdminAuditFilterAction')}</span>
          <select
            className={styles.auditFilterSelect}
            value={actionFilter || AUDIT_FILTER_ALL}
            onChange={(e) => {
              const v = (e.target as HTMLSelectElement).value;
              setActionFilter(v === AUDIT_FILTER_ALL ? '' : v);
            }}
          >
            <option value={AUDIT_FILTER_ALL}>{lang('AdminAuditFilterActionAll')}</option>
            {AUDIT_ACTIONS.map((a) => (
              <option key={a} value={a}>{auditActionLabel(lang, a)}</option>
            ))}
          </select>
        </div>
        <div className={styles.auditFilterField}>
          <span className={styles.formLabelText}>{lang('AdminAuditFilterTargetType')}</span>
          <select
            className={styles.auditFilterSelect}
            value={targetTypeFilter || AUDIT_FILTER_ALL}
            onChange={(e) => {
              const v = (e.target as HTMLSelectElement).value;
              setTargetTypeFilter(v === AUDIT_FILTER_ALL ? '' : v);
            }}
          >
            <option value={AUDIT_FILTER_ALL}>{lang('AdminAuditFilterTargetTypeAll')}</option>
            {AUDIT_TARGET_TYPES.map((t) => (
              <option key={t} value={t}>{auditTargetTypeLabel(lang, t)}</option>
            ))}
          </select>
        </div>
        <div className={styles.auditFilterField}>
          <span className={styles.formLabelText}>{lang('AdminAuditFilterFrom')}</span>
          <input
            type="date"
            className={styles.auditFilterDate}
            value={fromDate}
            max={toDate || undefined}
            onChange={(e) => setFromDate((e.target as HTMLInputElement).value)}
          />
        </div>
        <div className={styles.auditFilterField}>
          <span className={styles.formLabelText}>{lang('AdminAuditFilterTo')}</span>
          <input
            type="date"
            className={styles.auditFilterDate}
            value={toDate}
            min={fromDate || undefined}
            onChange={(e) => setToDate((e.target as HTMLInputElement).value)}
          />
        </div>
      </div>
      <div className={styles.formRow}>
        <input
          type="text"
          className={styles.searchInput}
          value={q}
          maxLength={200}
          placeholder={lang('AdminAuditSearchPlaceholder')}
          onChange={(e) => setQ((e.target as HTMLInputElement).value)}
        />
      </div>
      <div className={styles.auditToolbar}>
        <div className={styles.auditToolbarLeft}>
          <button
            type="button"
            className={styles.secondaryBtn}
            disabled={isBusy}
            onClick={handleResetFilters}
          >
            {lang('AdminAuditFilterReset')}
          </button>
        </div>
        {canExportAudit(role) && (
          <button
            type="button"
            className={styles.primaryBtn}
            disabled={isExporting}
            onClick={handleExport}
            title={lang('AdminAuditExportHint', { cap: AUDIT_EXPORT_HARD_CAP })}
          >
            {lang(isExporting ? 'AdminAuditExportRunning' : 'AdminAuditExport')}
          </button>
        )}
      </div>
      {error && <div className={styles.error}>{error}</div>}
      {!error && rows.length === 0 && !isBusy && (
        <div className={styles.empty}>{lang('AdminAuditEmpty')}</div>
      )}
      <div className={styles.auditList}>
        {rows.map((row) => {
          const detailsSummary = auditDetailsSummary(lang, row);
          const detailEntries = auditDetailEntries(lang, row);
          return (
            <div key={row.id} className={styles.auditRow}>
              <div className={styles.auditMeta}>
                <div className={styles.auditTitleLine}>
                  <span className={styles.auditTitle}>{auditActionLabel(lang, row.action)}</span>
                  <span className={styles.auditBadge}>{row.action}</span>
                </div>
                <span className={styles.auditWhen}>
                  {new Date(row.created_at).toLocaleString()}
                </span>
              </div>
              <div className={styles.auditDetails}>
                <span className={styles.auditActor}>
                  {lang('AdminAuditActor', { actor: row.actor_name || row.actor_id })}
                </span>
                <span className={styles.auditTarget}>
                  {auditTargetLabel(lang, row)}
                </span>
                {row.ip_address && (
                  <span className={styles.auditIp}>{row.ip_address}</span>
                )}
              </div>
              {detailsSummary && (
                <div className={styles.auditSubtitle}>{detailsSummary}</div>
              )}
              {detailEntries.length > 0 && (
                <div className={styles.auditDetailChips}>
                  {detailEntries.map(({ key, label, value }) => (
                    <span key={key} className={styles.auditDetailChip}>
                      <span className={styles.auditDetailChipLabel}>{label}</span>
                      <span className={styles.auditDetailChipValue}>{value}</span>
                    </span>
                  ))}
                </div>
              )}
              {row.details && Object.keys(row.details).length > 0 && (
                <details className={styles.auditTechnical}>
                  <summary>{lang('AdminAuditTechnicalDetails')}</summary>
                  <pre className={styles.auditDump}>{JSON.stringify(row.details, undefined, 2)}</pre>
                </details>
              )}
            </div>
          );
        })}
      </div>
      {hasMore && (
        <div className={styles.actions}>
          <button
            type="button"
            className={styles.secondaryBtn}
            disabled={isBusy}
            onClick={() => load(cursor)}
          >
            {lang('AdminAuditLoadMore')}
          </button>
        </div>
      )}
    </div>
  );
};

export default memo(withGlobal<OwnProps>(
  (global): Complete<StateProps> => {
    const tabState = selectTabState(global);
    return {
      saturnRole: global.saturnRole,
      currentUserId: global.currentUserId,
      tab: tabState.adminPanel?.tab,
    };
  },
)(AdminPanel));
