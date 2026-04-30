import { memo, useEffect, useState } from '../../../lib/teact/teact';
import { getActions } from '../../../global';

import type { NotificationMode } from '../../../api/saturn/methods/notifications';
import type { RegularLangKey } from '../../../types/language';

import { getNotificationMode, updateNotificationMode } from '../../../api/saturn/methods/notifications';

import useHistoryBack from '../../../hooks/useHistoryBack';
import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import RadioGroup from '../../ui/RadioGroup';

type OwnProps = {
  isActive?: boolean;
  onReset: NoneToVoidFunction;
};

const MODE_OPTIONS: Array<{ value: NotificationMode; labelKey: RegularLangKey; subLabelKey?: RegularLangKey }> = [
  { value: 'smart', labelKey: 'SmartNotificationsModeSmart', subLabelKey: 'SmartNotificationsDesc' },
  { value: 'off', labelKey: 'SmartNotificationsModeOff' },
];

const SettingsSmartNotifications = ({ isActive, onReset }: OwnProps) => {
  const { showNotification } = getActions();
  const lang = useLang();

  const [mode, setMode] = useState<NotificationMode>('smart');
  const [, setIsLoading] = useState(true);

  useHistoryBack({ isActive, onBack: onReset });

  useEffect(() => {
    let isCancelled = false;
    (async () => {
      try {
        const currentMode = await getNotificationMode();
        if (isCancelled) return;
        if (currentMode?.mode) {
          setMode(currentMode.mode);
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
    </div>
  );
};

export default memo(SettingsSmartNotifications);
