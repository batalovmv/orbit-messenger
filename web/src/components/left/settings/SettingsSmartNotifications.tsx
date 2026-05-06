import { memo, useEffect, useMemo, useState } from '../../../lib/teact/teact';
import { getActions } from '../../../global';

import type { NotificationMode, NotificationStats } from '../../../api/saturn/methods/notifications';
import type { RegularLangKey } from '../../../types/language';

import {
  fetchNotificationStats,
  getNotificationMode,
  updateNotificationMode,
} from '../../../api/saturn/methods/notifications';

import useHistoryBack from '../../../hooks/useHistoryBack';
import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import Checkbox from '../../ui/Checkbox';
import RadioGroup from '../../ui/RadioGroup';

type OwnProps = {
  isActive?: boolean;
  onReset: NoneToVoidFunction;
};

const MODE_OPTIONS: Array<{ value: NotificationMode; labelKey: RegularLangKey; subLabelKey?: RegularLangKey }> = [
  { value: 'smart', labelKey: 'SmartNotificationsModeSmart', subLabelKey: 'SmartNotificationsDesc' },
  { value: 'off', labelKey: 'SmartNotificationsModeOff' },
];

// Order matters — visual ranking from highest to lowest priority.
const PRIORITY_ROWS: Array<{ key: 'urgent' | 'important' | 'normal' | 'low'; labelKey: RegularLangKey }> = [
  { key: 'urgent', labelKey: 'NotificationPriorityUrgent' },
  { key: 'important', labelKey: 'NotificationPriorityImportant' },
  { key: 'normal', labelKey: 'NotificationPriorityNormal' },
  { key: 'low', labelKey: 'NotificationPriorityLow' },
];

// Per-priority delivery preferences. Keys are stable storage names that the
// service worker is expected to read (chunk 5 SW wiring is tracked
// separately — for now the toggles persist to localStorage so user choice
// is captured + visible in subsequent sessions).
const PREFS_STORAGE_KEY = 'orbit:smart_notification_prefs:v1';

type PriorityKey = 'urgent' | 'important' | 'normal' | 'low';

interface PriorityPrefs {
  enabled: boolean;
  sound: boolean;
}

const DEFAULT_PREFS: Record<PriorityKey, PriorityPrefs> = {
  // urgent: must be loud + persistent — defaults match SW behavior.
  urgent: { enabled: true, sound: true },
  important: { enabled: true, sound: true },
  normal: { enabled: true, sound: true },
  // low: silent by default — SW suppresses sound for `priority='low'` already.
  low: { enabled: true, sound: false },
};

function loadPrefs(): Record<PriorityKey, PriorityPrefs> {
  if (typeof localStorage === 'undefined') return { ...DEFAULT_PREFS };
  try {
    const raw = localStorage.getItem(PREFS_STORAGE_KEY);
    if (!raw) return { ...DEFAULT_PREFS };
    const parsed = JSON.parse(raw) as Partial<Record<PriorityKey, PriorityPrefs>>;
    return {
      urgent: { ...DEFAULT_PREFS.urgent, ...(parsed.urgent || {}) },
      important: { ...DEFAULT_PREFS.important, ...(parsed.important || {}) },
      normal: { ...DEFAULT_PREFS.normal, ...(parsed.normal || {}) },
      low: { ...DEFAULT_PREFS.low, ...(parsed.low || {}) },
    };
  } catch {
    return { ...DEFAULT_PREFS };
  }
}

// Cache API key shared with web/src/serviceWorker/pushNotification.ts so the
// SW reads the user's choices on every push event. Both halves must stay
// aligned — keep the URL / cache name in sync if you ever rename either.
const PREFS_CACHE_NAME = 'orbit-smart-prefs-v1';
const PREFS_CACHE_URL = '/prefs/smart_notifications.json';

async function syncPrefsToCache(prefs: Record<PriorityKey, PriorityPrefs>) {
  if (typeof caches === 'undefined') return;
  try {
    const cache = await caches.open(PREFS_CACHE_NAME);
    await cache.put(PREFS_CACHE_URL, new Response(JSON.stringify(prefs), {
      headers: { 'Content-Type': 'application/json' },
    }));
  } catch {
    // ignore — Cache API unavailable in some private-mode setups
  }
}

function savePrefs(prefs: Record<PriorityKey, PriorityPrefs>) {
  if (typeof localStorage !== 'undefined') {
    try {
      localStorage.setItem(PREFS_STORAGE_KEY, JSON.stringify(prefs));
    } catch {
      // ignore — quota or private mode
    }
  }
  // Cache API write is async but fire-and-forget — UI shouldn't block on it.
  void syncPrefsToCache(prefs);
}

const SettingsSmartNotifications = ({ isActive, onReset }: OwnProps) => {
  const { showNotification } = getActions();
  const lang = useLang();

  const [mode, setMode] = useState<NotificationMode>('smart');
  const [, setIsLoading] = useState(true);
  const [stats, setStats] = useState<NotificationStats | undefined>();
  const [prefs, setPrefs] = useState<Record<PriorityKey, PriorityPrefs>>(loadPrefs);

  // Mirror localStorage prefs into the Cache API on mount so a fresh SW
  // (browser may evict the cache on update) sees the user's saved choices
  // before the first push lands.
  useEffect(() => {
    void syncPrefsToCache(loadPrefs());
  }, []);

  useHistoryBack({ isActive, onBack: onReset });

  useEffect(() => {
    let isCancelled = false;
    (async () => {
      try {
        const [currentMode, currentStats] = await Promise.all([
          getNotificationMode(),
          fetchNotificationStats(),
        ]);
        if (isCancelled) return;
        if (currentMode?.mode) {
          setMode(currentMode.mode);
        }
        if (currentStats) {
          setStats(currentStats);
        }
      } finally {
        if (!isCancelled) {
          setIsLoading(false);
        }
      }
    })();
    return () => {
      isCancelled = true;
    };
  }, []);

  const handlePrefChange = useLastCallback((priority: PriorityKey, field: 'enabled' | 'sound', value: boolean) => {
    setPrefs((prev) => {
      const next = { ...prev, [priority]: { ...prev[priority], [field]: value } };
      savePrefs(next);
      return next;
    });
  });

  const handleModeChange = useLastCallback(async (value: string) => {
    const newMode = value as NotificationMode;
    setMode(newMode);
    try {
      await updateNotificationMode(newMode);
    } catch {
      showNotification({ message: { key: 'SmartNotificationsUpdateFailed' } });
    }
  });

  const options = MODE_OPTIONS.map((opt) => ({
    value: opt.value,
    label: lang(opt.labelKey),
    subLabel: opt.subLabelKey ? lang(opt.subLabelKey) : undefined,
  }));

  const totalClassified = stats?.total_classified ?? 0;

  // Format the period_start ISO timestamp into a localized date for the
  // "Since {date}" label. Falls back to the raw string on Intl errors so
  // the row never shows undefined to the user.
  const periodStartLabel = useMemo(() => {
    if (!stats?.period_start) return undefined;
    try {
      const d = new Date(stats.period_start);
      return d.toLocaleDateString(lang.code || undefined, { day: 'numeric', month: 'short' });
    } catch {
      return stats.period_start;
    }
  }, [stats?.period_start, lang.code]);

  return (
    <div className="settings-content custom-scroll">
      <div className="settings-item">
        <h4 className="settings-item-header" dir={lang.isRtl ? 'rtl' : undefined}>
          {lang('SmartNotifications')}
        </h4>
        <p className="settings-item-description">
          {lang('SmartNotificationsDesc')}
        </p>
      </div>
      <div className="settings-item">
        <RadioGroup
          name="smartNotificationMode"
          options={options}
          selected={mode}
          onChange={handleModeChange}
        />
      </div>

      {mode === 'smart' && (
        <div className="settings-item">
          <h4 className="settings-item-header" dir={lang.isRtl ? 'rtl' : undefined}>
            {lang('SmartNotificationsBehaviorTitle')}
          </h4>
          <p className="settings-item-description">
            {lang('SmartNotificationsBehaviorDesc')}
          </p>
          {PRIORITY_ROWS.map(({ key, labelKey }) => (
            <div className="settings-priority-pref-row" key={key}>
              <span className={`settings-stats-pill settings-stats-pill--${key}`}>
                {lang(labelKey)}
              </span>
              <Checkbox
                label={lang('SmartNotificationsBehaviorEnabled')}
                checked={prefs[key].enabled}
                onCheck={(value) => handlePrefChange(key, 'enabled', value)}
              />
              <Checkbox
                label={lang('SmartNotificationsBehaviorSound')}
                checked={prefs[key].sound}
                onCheck={(value) => handlePrefChange(key, 'sound', value)}
                disabled={!prefs[key].enabled}
              />
            </div>
          ))}
        </div>
      )}

      {mode === 'smart' && (
        <div className="settings-item">
          <h4 className="settings-item-header" dir={lang.isRtl ? 'rtl' : undefined}>
            {lang('SmartNotificationsStatsTitle')}
          </h4>
          {totalClassified === 0 ? (
            <p className="settings-item-description">
              {lang('SmartNotificationsStatsEmpty')}
            </p>
          ) : (
            <>
              <p className="settings-item-description">
                {lang('SmartNotificationsStatsTotal', { count: String(totalClassified) })}
                {periodStartLabel ? ` · ${lang('SmartNotificationsStatsSince', { date: periodStartLabel })}` : ''}
              </p>
              <ul className="settings-stats-list">
                {PRIORITY_ROWS.map(({ key, labelKey }) => {
                  const count = stats?.by_priority?.[key] ?? 0;
                  const pct = totalClassified > 0 ? Math.round((count / totalClassified) * 100) : 0;
                  return (
                    <li className="settings-stats-row" key={key}>
                      <span className={`settings-stats-pill settings-stats-pill--${key}`}>
                        {lang(labelKey)}
                      </span>
                      <span className="settings-stats-count">{count}</span>
                      <span className="settings-stats-pct">{pct}%</span>
                    </li>
                  );
                })}
              </ul>
            </>
          )}
        </div>
      )}
    </div>
  );
};

export default memo(SettingsSmartNotifications);
