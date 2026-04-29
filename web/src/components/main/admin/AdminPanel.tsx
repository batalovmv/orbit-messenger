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
  AUDIT_ACTIONS, AUDIT_EXPORT_HARD_CAP, AUDIT_TARGET_TYPES,
  backfillDefaultChats, fetchAdminFlags, fetchAuditLog, fetchAuditLogExport,
  sendAdminTestPush,
  setAdminFlag, setAdminMaintenance,
} from '../../../api/saturn/methods/admin';
import type { AuditQuery, PushTestReport } from '../../../api/saturn/methods/admin';
import { selectTabState } from '../../../global/selectors';
import buildClassName from '../../../util/buildClassName';

import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import Modal from '../../ui/Modal';

import styles from './AdminPanel.module.scss';

export type OwnProps = {
  isOpen?: boolean;
};

type AdminTab = 'flags' | 'maintenance' | 'audit' | 'welcome' | 'push';

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
  }
};

// Tab visibility mirrors the backend permission split in pkg/permissions:
// admin/superadmin have SysManageSettings (flags + maintenance + welcome)
// and SysViewAuditLog (audit). compliance is audit-only — no write access
// to system settings.
const tabsForRole = (role?: GlobalState['saturnRole']): readonly AdminTab[] => {
  if (role === 'admin' || role === 'superadmin') return ['flags', 'maintenance', 'welcome', 'push', 'audit'];
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
        {activeTab === 'audit' && <AuditTab role={saturnRole} />}
      </div>
    </Modal>
  );
};

// ===========================================================================
// Feature Flags tab
// ===========================================================================
//
// Three additions on top of the basic toggle list:
//  1. Exposure filter (segmented control) — `unauth` / `auth` / `admin` /
//     `server_only`. "All" is the default. Filter is purely client-side
//     since the dataset is the small in-code registry (≤20 entries).
//  2. Per-row "History" button — opens a modal showing the audit feed
//     filtered to this flag (`target_type=feature_flag&target_id=<key>`).
//     Reuses the existing GET /admin/audit-log endpoint, no new backend.
//  3. Per-row Dangerous badge + confirmation modal when toggling ON.
//     Backend annotation is `featureflags.Definition.Dangerous` and ships
//     in the AdminFlag JSON. Toggle OFF never asks (turning a danger flag
//     off is always safe — that's why it's marked dangerous).

type ExposureFilter = '' | AdminFlag['exposure'];

const EXPOSURE_FILTERS: { value: ExposureFilter; labelKey: 'AdminFlagFilterAll' | 'AdminFlagFilterUnauth' | 'AdminFlagFilterAuth' | 'AdminFlagFilterAdmin' | 'AdminFlagFilterServerOnly' }[] = [
  { value: '', labelKey: 'AdminFlagFilterAll' },
  { value: 'unauth', labelKey: 'AdminFlagFilterUnauth' },
  { value: 'auth', labelKey: 'AdminFlagFilterAuth' },
  { value: 'admin', labelKey: 'AdminFlagFilterAdmin' },
  { value: 'server_only', labelKey: 'AdminFlagFilterServerOnly' },
];

const FlagsTab = () => {
  const lang = useLang();
  const [flags, setFlags] = useState<AdminFlag[]>([]);
  const [error, setError] = useState<string | undefined>();
  const [busyKey, setBusyKey] = useState<string | undefined>();
  const [exposureFilter, setExposureFilter] = useState<ExposureFilter>('');
  const [pendingDangerous, setPendingDangerous] = useState<AdminFlag | undefined>();
  const [historyFor, setHistoryFor] = useState<AdminFlag | undefined>();

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

  const applyToggle = useLastCallback(async (flag: AdminFlag, nextEnabled: boolean) => {
    setBusyKey(flag.key);
    try {
      const updated = await setAdminFlag(flag.key, nextEnabled, flag.metadata);
      setFlags((prev) => prev.map((f) => (f.key === updated.key ? updated : f)));
    } catch (e) {
      setError((e as Error).message || 'update failed');
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

  const visibleFlags = useMemo(() => {
    if (!exposureFilter) return flags;
    return flags.filter((f) => f.exposure === exposureFilter);
  }, [flags, exposureFilter]);

  return (
    <div className={styles.tabBody}>
      <div className={styles.flagFilterBar} role="tablist">
        {EXPOSURE_FILTERS.map((opt) => (
          <button
            key={opt.value || 'all'}
            type="button"
            role="tab"
            aria-selected={exposureFilter === opt.value}
            className={buildClassName(
              styles.flagFilterChip,
              exposureFilter === opt.value && styles.flagFilterChipActive,
            )}
            onClick={() => setExposureFilter(opt.value)}
          >
            {lang(opt.labelKey)}
          </button>
        ))}
      </div>
      {error && <div className={styles.error}>{error}</div>}
      {flags.length === 0 && !error && <div className={styles.empty}>{lang('Loading')}</div>}
      {flags.length > 0 && visibleFlags.length === 0 && (
        <div className={styles.empty}>{lang('AdminAuditEmpty')}</div>
      )}
      <ul className={styles.flagList}>
        {visibleFlags.map((flag) => (
          <li key={flag.key} className={styles.flagRow}>
            <div className={styles.flagInfo}>
              <div className={styles.flagKey}>
                {flag.key}
                {!flag.known && (
                  <span className={styles.flagBadge}>{lang('AdminFlagUnknown')}</span>
                )}
                {flag.dangerous && (
                  <span className={styles.flagBadge}>{lang('AdminFlagDangerous')}</span>
                )}
                <span className={styles.flagExposure}>{flag.exposure}</span>
              </div>
              {flag.description && <div className={styles.flagDesc}>{flag.description}</div>}
            </div>
            <div className={styles.flagControls}>
              <button
                type="button"
                className={styles.flagHistoryBtn}
                onClick={() => setHistoryFor(flag)}
              >
                {lang('AdminFlagHistory')}
              </button>
              {/* Unknown DB rows (no registry entry) are read-only — the
                  backend Set() rejects them with "Unknown feature flag", so
                  rendering a toggle would be a dead control. The "не в
                  реестре" badge surfaces the state. */}
              <button
                type="button"
                className={buildClassName(
                  styles.flagToggle,
                  flag.enabled && styles.flagToggleOn,
                )}
                disabled={busyKey === flag.key || !flag.known}
                onClick={() => handleToggle(flag)}
              >
                {flag.enabled ? lang('AdminFlagOn') : lang('AdminFlagOff')}
              </button>
            </div>
          </li>
        ))}
      </ul>

      {pendingDangerous && (
        <Modal
          isOpen
          onClose={handleCancelDangerous}
          title={lang('AdminFlagDangerousConfirmTitle')}
          hasCloseButton
        >
          <div className={styles.confirmBody}>
            <p>
              {lang('AdminFlagDangerousConfirmBody', {
                key: pendingDangerous.key,
                description: pendingDangerous.description || '',
              })}
            </p>
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
        setError((e as Error).message || 'load failed');
      })
      .finally(() => {
        if (cancelled) return;
        setIsBusy(false);
      });
    return () => { cancelled = true; };
  }, [flag.key]);

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
// back to option.text (the localized label like "Все действия") for
// `option.value`. That value would then be sent as `?action=Все действия`
// and the backend whitelist would reject it with 400 "unknown action". Use
// a non-empty sentinel and convert to '' before request-building.
const AUDIT_FILTER_ALL = '__all__';

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
      setError((e as Error).message || 'load failed');
    } finally {
      setIsBusy(false);
    }
  });

  // Refetch from scratch whenever any of the filter inputs change. Free-text
  // is the only one debounced — the rest fire on the next tick.
  useEffect(() => { load(undefined); }, [
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
              <option key={a} value={a}>{a}</option>
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
              <option key={t} value={t}>{t}</option>
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
