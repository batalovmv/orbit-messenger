import { memo, useEffect, useState } from '../../lib/teact/teact';

import { IS_IOS, IS_PWA } from '../../util/browser/windowEnvironment';
import buildClassName from '../../util/buildClassName';

import useLang from '../../hooks/useLang';
import useLastCallback from '../../hooks/useLastCallback';

import Icon from '../common/icons/Icon';
import Button from '../ui/Button';

import styles from './IosInstallBanner.module.scss';

const DISMISS_KEY = 'tt-ios-install-dismissed-v1';
const SHOW_DELAY_MS = 8000;
const REPROMPT_AFTER_MS = 14 * 24 * 60 * 60 * 1000;

const IosInstallBanner = () => {
  const lang = useLang();
  const [shouldShow, setShouldShow] = useState(false);

  useEffect(() => {
    if (!IS_IOS || IS_PWA) return undefined;

    let dismissedAt = 0;
    try {
      dismissedAt = Number(localStorage.getItem(DISMISS_KEY)) || 0;
    } catch {
      dismissedAt = 0;
    }
    if (dismissedAt && Date.now() - dismissedAt < REPROMPT_AFTER_MS) return undefined;

    const id = window.setTimeout(() => setShouldShow(true), SHOW_DELAY_MS);
    return () => window.clearTimeout(id);
  }, []);

  const handleDismiss = useLastCallback(() => {
    try {
      localStorage.setItem(DISMISS_KEY, String(Date.now()));
    } catch {
      // ignore — we'll re-prompt next session
    }
    setShouldShow(false);
  });

  if (!shouldShow) return undefined;

  return (
    <div className={buildClassName(styles.root)} role="status" aria-live="polite">
      <Icon name="install" className={styles.icon} />
      <div className={styles.body}>
        <p className={styles.title}>{lang('IosInstallBanner.Title')}</p>
        <p className={styles.hint}>{lang('IosInstallBanner.Hint')}</p>
      </div>
      <Button
        className={styles.close}
        round
        size="smaller"
        color="translucent"
        onClick={handleDismiss}
        ariaLabel={lang('Close')}
      >
        <Icon name="close" />
      </Button>
    </div>
  );
};

export default memo(IosInstallBanner);
