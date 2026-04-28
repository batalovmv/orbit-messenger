// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import {
  memo, useEffect, useMemo, useState,
} from '@teact';
import { getActions, withGlobal } from '../../../global';

import type { GlobalState } from '../../../global/types';
import type {
  AdminFlag, AuditEntry,
} from '../../../api/saturn/methods/admin';

import {
  backfillDefaultChats, fetchAdminFlags, fetchAuditLog,
  fetchUserSessions,
  revokeSession,
  sendAdminTestPush,
  setAdminFlag, setAdminMaintenance,
} from '../../../api/saturn/methods/admin';
import type { AdminSession, PushTestReport } from '../../../api/saturn/methods/admin';
import { selectTabState } from '../../../global/selectors';
import buildClassName from '../../../util/buildClassName';

import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import Modal from '../../ui/Modal';

import styles from './AdminPanel.module.scss';

export type OwnProps = {
  isOpen?: boolean;
};

type AdminTab = 'flags' | 'maintenance' | 'audit' | 'welcome' | 'push' | 'sessions';

type StateProps = {
  saturnRole?: GlobalState['saturnRole'];
  tab?: AdminTab;
};

const AUDIT_PAGE_SIZE = 50;
const AUDIT_SEARCH_DEBOUNCE_MS = 300;

const tabLangKey = (tab: AdminTab) => {
  switch (tab) {
    case 'flags': return 'AdminTabFeatureFlags';
    case 'maintenance': return 'AdminTabMaintenance';
    case 'audit': return 'AdminTabAuditLog';
    case 'welcome': return 'AdminTabWelcome';
    case 'push': return 'AdminTabPushInspector';
    case 'sessions': return 'AdminTabSessions';
  }
};

// Tab visibility mirrors the backend permission split in pkg/permissions:
// admin/superadmin have SysManageSettings (flags + maintenance + welcome)
// and SysViewAuditLog (audit). compliance is audit-only — no write access
// to system settings.
const tabsForRole = (role?: GlobalState['saturnRole']): readonly AdminTab[] => {
  if (role === 'admin' || role === 'superadmin') return ['flags', 'maintenance', 'welcome', 'push', 'sessions', 'audit'];
  if (role === 'compliance') return ['audit'];
  return [];
};

const AdminPanel = ({ isOpen, saturnRole, tab }: OwnProps & StateProps) => {
  const { closeAdminPanel, selectAdminTab } = getActions();
  const lang = useLang();

  const visibleTabs = useMemo(() => tabsForRole(saturnRole), [saturnRole]);
  const hasAccess = visibleTabs.length > 0;
  const shouldRender = Boolean(isOpen && hasAccess);

  const activeTab: AdminTab = tab && visibleTabs.includes(tab) ? tab : visibleTabs[0] ?? 'audit';

  const handleClose = useLastCallback(() => closeAdminPanel());

  if (!shouldRender) return undefined;

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
      <div className={styles.tabs} role="tablist">
        {visibleTabs.map((t) => (
          <button
            type="button"
            key={t}
            role="tab"
            aria-selected={activeTab === t}
            className={buildClassName(styles.tab, activeTab === t && styles.tabActive)}
            onClick={() => selectAdminTab({ tab: t })}
          >
            {lang(tabLangKey(t))}
          </button>
        ))}
      </div>
      <div className={styles.body}>
        {activeTab === 'flags' && <FlagsTab />}
        {activeTab === 'maintenance' && <MaintenanceTab />}
        {activeTab === 'welcome' && <WelcomeTab />}
        {activeTab === 'push' && <PushInspectorTab />}
        {activeTab === 'sessions' && <SessionsTab />}
        {activeTab === 'audit' && <AuditTab />}
      </div>
    </Modal>
  );
};

// ===========================================================================
// Feature Flags tab
// ===========================================================================
const FlagsTab = () => {
  const lang = useLang();
  const [flags, setFlags] = useState<AdminFlag[]>([]);
  const [error, setError] = useState<string | undefined>();
  const [busyKey, setBusyKey] = useState<string | undefined>();

  const reload = useLastCallback(async () => {
    try {
      const list = await fetchAdminFlags();
      setFlags(list);
      setError(undefined);
    } catch (e) {
      setError((e as Error).message || 'load failed');
    }
  });

  useEffect(() => { reload(); }, [reload]);

  const handleToggle = useLastCallback(async (flag: AdminFlag) => {
    setBusyKey(flag.key);
    try {
      const updated = await setAdminFlag(flag.key, !flag.enabled, flag.metadata);
      setFlags((prev) => prev.map((f) => (f.key === updated.key ? updated : f)));
    } catch (e) {
      setError((e as Error).message || 'update failed');
    } finally {
      setBusyKey(undefined);
    }
  });

  return (
    <div className={styles.tabBody}>
      {error && <div className={styles.error}>{error}</div>}
      {flags.length === 0 && !error && <div className={styles.empty}>{lang('Loading')}</div>}
      <ul className={styles.flagList}>
        {flags.map((flag) => (
          <li key={flag.key} className={styles.flagRow}>
            <div className={styles.flagInfo}>
              <div className={styles.flagKey}>
                {flag.key}
                {!flag.known && (
                  <span className={styles.flagBadge}>{lang('AdminFlagUnknown')}</span>
                )}
                <span className={styles.flagExposure}>{flag.exposure}</span>
              </div>
              {flag.description && <div className={styles.flagDesc}>{flag.description}</div>}
            </div>
            <button
              type="button"
              className={buildClassName(
                styles.flagToggle,
                flag.enabled && styles.flagToggleOn,
              )}
              disabled={busyKey === flag.key}
              onClick={() => handleToggle(flag)}
            >
              {flag.enabled ? lang('AdminFlagOn') : lang('AdminFlagOff')}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
};

// ===========================================================================
// Maintenance tab
// ===========================================================================
const MaintenanceTab = () => {
  const lang = useLang();
  const [enabled, setEnabled] = useState(false);
  const [message, setMessage] = useState('');
  const [blockWrites, setBlockWrites] = useState(false);
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
      setUpdatedAt(m.updated_at);
    } catch (e) {
      setError((e as Error).message || 'load failed');
    }
  });

  useEffect(() => { reload(); }, [reload]);

  const handleApply = useLastCallback(async () => {
    setIsBusy(true);
    setError(undefined);
    setInfo(undefined);
    try {
      const flag = await setAdminMaintenance({ enabled, message, block_writes: blockWrites });
      setUpdatedAt(flag.updated_at);
      setInfo(lang('AdminMaintenanceSaved'));
    } catch (e) {
      setError((e as Error).message || 'save failed');
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
      setUpdatedAt(flag.updated_at);
      setInfo(lang('AdminMaintenanceDisabled'));
    } catch (e) {
      setError((e as Error).message || 'save failed');
    } finally {
      setIsBusy(false);
    }
  });

  return (
    <div className={styles.tabBody}>
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
      {info && <div className={styles.success}>{info}</div>}
      {error && <div className={styles.error}>{error}</div>}
      {updatedAt && (
        <div className={styles.formHelp}>
          {lang('AdminMaintenanceLastUpdated')}: {new Date(updatedAt).toLocaleString()}
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
const WelcomeTab = () => {
  const lang = useLang();
  const [isConfirming, setIsConfirming] = useState(false);
  const [isBusy, setIsBusy] = useState(false);
  const [insertedCount, setInsertedCount] = useState<number | undefined>();
  const [error, setError] = useState<string | undefined>();

  const handleStart = useLastCallback(() => {
    setError(undefined);
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
      setIsConfirming(false);
    } catch (e) {
      setError((e as Error).message || 'backfill failed');
    } finally {
      setIsBusy(false);
    }
  });

  return (
    <div className={styles.tabBody}>
      <div className={styles.welcomeIntro}>
        {lang('AdminWelcomeIntro')}
      </div>
      <div className={styles.welcomeBody}>
        <p>{lang('AdminWelcomeBackfillDescription')}</p>
        <p className={styles.welcomeWarn}>{lang('AdminWelcomeBackfillWarning')}</p>
      </div>
      {insertedCount !== undefined && (
        <div className={styles.success}>
          {lang('AdminWelcomeBackfillResult', { count: insertedCount })}
        </div>
      )}
      {error && <div className={styles.error}>{error}</div>}
      <div className={styles.actions}>
        <button
          type="button"
          className={styles.primaryBtn}
          disabled={isBusy}
          onClick={handleStart}
        >
          {lang('AdminWelcomeBackfillButton')}
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
            <p>{lang('AdminWelcomeBackfillConfirmBody')}</p>
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
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [report, setReport] = useState<PushTestReport | undefined>();
  const [error, setError] = useState<string | undefined>();
  const [isBusy, setIsBusy] = useState(false);

  const handleSend = useLastCallback(async () => {
    setError(undefined);
    setReport(undefined);
    setIsBusy(true);
    try {
      // identifier is email if it has '@', else UUID — let server validate.
      const isEmail = identifier.includes('@');
      const result = await sendAdminTestPush({
        user_id: isEmail ? undefined : identifier.trim() || undefined,
        email: isEmail ? identifier.trim() : undefined,
        title: title || undefined,
        body: body || undefined,
      });
      setReport(result);
    } catch (e) {
      setError((e as Error).message || 'send failed');
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
          onChange={(e) => setIdentifier((e.target as HTMLInputElement).value)}
        />
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
          disabled={isBusy || !identifier.trim()}
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
              {lang('AdminPushInspectorTarget')}: {report.email || report.user_id}
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
// Sessions tab (Day 5.2)
// ===========================================================================
//
// Lets admins terminate a single device's JWT session without deactivating
// the whole user (lost laptop / ex-employee / suspicious IP). The server
// handles audit + guards (own-current, role hierarchy); the UI only needs
// to render the list, hide the revoke button on the actor's own row, and
// confirm before sending DELETE.
const SessionsTab = () => {
  const lang = useLang();
  const [identifier, setIdentifier] = useState('');
  const [resolvedUserId, setResolvedUserId] = useState('');
  const [sessions, setSessions] = useState<AdminSession[]>([]);
  const [error, setError] = useState<string | undefined>();
  const [info, setInfo] = useState<string | undefined>();
  const [isBusy, setIsBusy] = useState(false);
  const [confirmId, setConfirmId] = useState<string | undefined>();
  const [revokingId, setRevokingId] = useState<string | undefined>();

  const handleLookup = useLastCallback(async () => {
    setError(undefined);
    setInfo(undefined);
    setSessions([]);
    const id = identifier.trim();
    if (!id) return;
    // Email lookup is not supported by the sessions endpoint (auth indexes by
    // user_id only). Tell the operator to copy the UUID from the Audit log
    // or Push Inspector tab — both expose target user_id directly.
    if (id.includes('@')) {
      setError(lang('AdminSessionsEmailUnsupported'));
      return;
    }
    setIsBusy(true);
    setResolvedUserId(id);
    try {
      const list = await fetchUserSessions(id);
      setSessions(list);
    } catch (e) {
      setError((e as Error).message || 'lookup failed');
    } finally {
      setIsBusy(false);
    }
  });

  const handleAskConfirm = useLastCallback((sessionId: string) => {
    setError(undefined);
    setInfo(undefined);
    setConfirmId(sessionId);
  });

  const handleCancelConfirm = useLastCallback(() => {
    if (!revokingId) setConfirmId(undefined);
  });

  const handleConfirmRevoke = useLastCallback(async () => {
    if (!confirmId) return;
    setRevokingId(confirmId);
    setError(undefined);
    try {
      await revokeSession(confirmId);
      setSessions((prev) => prev.filter((s) => s.id !== confirmId));
      setInfo(lang('AdminSessionsRevokedOk'));
      setConfirmId(undefined);
    } catch (e) {
      setError((e as Error).message || 'revoke failed');
    } finally {
      setRevokingId(undefined);
    }
  });

  return (
    <div className={styles.tabBody}>
      <div className={styles.welcomeIntro}>
        {lang('AdminSessionsIntro')}
      </div>
      <div className={styles.formRow}>
        <span className={styles.formLabelText}>{lang('AdminSessionsIdentifier')}</span>
        <input
          type="text"
          className={styles.searchInput}
          value={identifier}
          maxLength={200}
          placeholder={lang('AdminSessionsIdentifierPlaceholder')}
          onChange={(e) => setIdentifier((e.target as HTMLInputElement).value)}
        />
      </div>
      <div className={styles.actions}>
        <button
          type="button"
          className={styles.primaryBtn}
          disabled={isBusy || !identifier.trim()}
          onClick={handleLookup}
        >
          {lang(isBusy ? 'Loading' : 'AdminSessionsLookup')}
        </button>
      </div>
      {info && <div className={styles.success}>{info}</div>}
      {error && <div className={styles.error}>{error}</div>}
      {resolvedUserId && !error && sessions.length === 0 && !isBusy && (
        <div className={styles.empty}>{lang('AdminSessionsEmpty')}</div>
      )}
      {sessions.length > 0 && (
        <ul className={styles.sessionList}>
          {sessions.map((s) => {
            const isExpired = new Date(s.expires_at).getTime() < Date.now();
            return (
              <li
                key={s.id}
                className={buildClassName(styles.sessionRow, s.is_current && styles.sessionRowCurrent)}
              >
                <div className={styles.sessionMeta}>
                  <div className={styles.sessionHead}>
                    <span className={styles.sessionId}>{s.id}</span>
                    {s.is_current && (
                      <span className={styles.sessionBadge}>{lang('AdminSessionsCurrent')}</span>
                    )}
                    {isExpired && (
                      <span className={styles.sessionBadgeExpired}>{lang('AdminSessionsExpired')}</span>
                    )}
                  </div>
                  {s.user_agent && (
                    <div className={styles.sessionUA}>{s.user_agent}</div>
                  )}
                  <div className={styles.sessionFoot}>
                    {s.ip_address && <span>{s.ip_address}</span>}
                    <span>
                      {lang('AdminSessionsCreatedAt')}: {new Date(s.created_at).toLocaleString()}
                    </span>
                    <span>
                      {lang('AdminSessionsExpiresAt')}: {new Date(s.expires_at).toLocaleString()}
                    </span>
                  </div>
                </div>
                <div className={styles.sessionActions}>
                  {s.is_current ? (
                    <span className={styles.sessionCurrentNote}>
                      {lang('AdminSessionsCurrentNote')}
                    </span>
                  ) : (
                    <button
                      type="button"
                      className={styles.dangerBtn}
                      disabled={Boolean(revokingId)}
                      onClick={() => handleAskConfirm(s.id)}
                    >
                      {lang('AdminSessionsRevoke')}
                    </button>
                  )}
                </div>
              </li>
            );
          })}
        </ul>
      )}

      {confirmId && (
        <Modal
          isOpen={Boolean(confirmId)}
          onClose={handleCancelConfirm}
          title={lang('AdminSessionsRevokeConfirmTitle')}
          hasCloseButton={!revokingId}
        >
          <div className={styles.confirmBody}>
            <p>{lang('AdminSessionsRevokeConfirmBody')}</p>
          </div>
          <div className={styles.actions}>
            <button
              type="button"
              className={styles.secondaryBtn}
              disabled={Boolean(revokingId)}
              onClick={handleCancelConfirm}
            >
              {lang('Cancel')}
            </button>
            <button
              type="button"
              className={styles.dangerBtn}
              disabled={Boolean(revokingId)}
              onClick={handleConfirmRevoke}
            >
              {lang(revokingId ? 'Loading' : 'AdminSessionsRevokeConfirmAction')}
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
};

// ===========================================================================
// Audit log tab
// ===========================================================================
const AuditTab = () => {
  const lang = useLang();
  const [q, setQ] = useState('');
  const [debouncedQ, setDebouncedQ] = useState('');
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [cursor, setCursor] = useState<string | undefined>();
  const [hasMore, setHasMore] = useState(false);
  const [error, setError] = useState<string | undefined>();
  const [isBusy, setIsBusy] = useState(false);

  useEffect(() => {
    const t = window.setTimeout(() => setDebouncedQ(q), AUDIT_SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(t);
  }, [q]);

  const load = useLastCallback(async (next?: string) => {
    setIsBusy(true);
    try {
      const page = await fetchAuditLog({
        q: debouncedQ || undefined,
        cursor: next,
        limit: AUDIT_PAGE_SIZE,
      });
      if (next) {
        setEntries((prev) => [...prev, ...page.data]);
      } else {
        setEntries(page.data);
      }
      setCursor(page.cursor);
      setHasMore(page.has_more);
      setError(undefined);
    } catch (e) {
      setError((e as Error).message || 'load failed');
    } finally {
      setIsBusy(false);
    }
  });

  // Refetch from scratch whenever the debounced query changes.
  useEffect(() => { load(undefined); }, [debouncedQ, load]);

  const rows = useMemo(() => entries, [entries]);

  return (
    <div className={styles.tabBody}>
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
      {error && <div className={styles.error}>{error}</div>}
      {!error && rows.length === 0 && !isBusy && (
        <div className={styles.empty}>{lang('AdminAuditEmpty')}</div>
      )}
      <div className={styles.auditList}>
        {rows.map((row) => (
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
              {row.target_type && row.target_type !== 'system' && (
                <span className={styles.auditTarget}>
                  → {row.target_type}{row.target_id ? `:${row.target_id}` : ''}
                </span>
              )}
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
      tab: tabState.adminPanel?.tab,
    };
  },
)(AdminPanel));
