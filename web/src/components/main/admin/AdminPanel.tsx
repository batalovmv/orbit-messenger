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
  fetchAdminFlags, fetchAuditLog, setAdminFlag, setAdminMaintenance,
} from '../../../api/saturn/methods/admin';
import { selectTabState } from '../../../global/selectors';
import buildClassName from '../../../util/buildClassName';

import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import Modal from '../../ui/Modal';

import styles from './AdminPanel.module.scss';

export type OwnProps = {
  isOpen?: boolean;
};

type StateProps = {
  saturnRole?: GlobalState['saturnRole'];
  tab?: 'flags' | 'maintenance' | 'audit';
};

const TABS: ReadonlyArray<'flags' | 'maintenance' | 'audit'> = ['flags', 'maintenance', 'audit'];
const AUDIT_PAGE_SIZE = 50;
const AUDIT_SEARCH_DEBOUNCE_MS = 300;

const tabLangKey = (tab: 'flags' | 'maintenance' | 'audit') => {
  switch (tab) {
    case 'flags': return 'AdminTabFeatureFlags';
    case 'maintenance': return 'AdminTabMaintenance';
    case 'audit': return 'AdminTabAuditLog';
  }
};

const AdminPanel = ({ isOpen, saturnRole, tab = 'flags' }: OwnProps & StateProps) => {
  const { closeAdminPanel, selectAdminTab } = getActions();
  const lang = useLang();

  // Defense in depth — backend enforces too. The bitmask split (admin vs
  // compliance vs superadmin) is checked per-action server-side; this gate
  // just hides the entry point for non-privileged accounts.
  const hasAccess = saturnRole === 'admin' || saturnRole === 'superadmin' || saturnRole === 'compliance';
  const shouldRender = Boolean(isOpen && hasAccess);

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
        {TABS.map((t) => (
          <button
            type="button"
            key={t}
            role="tab"
            aria-selected={tab === t}
            className={buildClassName(styles.tab, tab === t && styles.tabActive)}
            onClick={() => selectAdminTab({ tab: t })}
          >
            {lang(tabLangKey(t))}
          </button>
        ))}
      </div>
      <div className={styles.body}>
        {tab === 'flags' && <FlagsTab />}
        {tab === 'maintenance' && <MaintenanceTab />}
        {tab === 'audit' && <AuditTab />}
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
