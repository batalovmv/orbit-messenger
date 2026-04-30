import { memo, useEffect, useState } from '../../lib/teact/teact';
import { withGlobal } from '../../global';

import { selectTabState } from '../../global/selectors';
import { IS_PWA } from '../../util/browser/windowEnvironment';
import buildClassName from '../../util/buildClassName';
import { getPromptInstall } from '../../util/installPrompt';

import useLang from '../../hooks/useLang';
import useLastCallback from '../../hooks/useLastCallback';

import Icon from '../common/icons/Icon';
import Button from '../ui/Button';

import styles from './InstallAppBanner.module.scss';

const DISMISS_KEY = 'tt-install-app-dismissed-v1';
const SHOW_DELAY_MS = 8000;
const REPROMPT_AFTER_MS = 14 * 24 * 60 * 60 * 1000;

type StateProps = {
  canInstall?: boolean;
};

const InstallAppBanner = ({ canInstall }: StateProps) => {
  const lang = useLang();
  const [shouldShow, setShouldShow] = useState(false);

  useEffect(() => {
    if (!canInstall || IS_PWA) {
      setShouldShow(false);
      return undefined;
    }

    let dismissedAt = 0;
    try {
      dismissedAt = Number(localStorage.getItem(DISMISS_KEY)) || 0;
    } catch {
      dismissedAt = 0;
    }
    if (dismissedAt && Date.now() - dismissedAt < REPROMPT_AFTER_MS) {
      setShouldShow(false);
      return undefined;
    }

    const id = window.setTimeout(() => setShouldShow(true), SHOW_DELAY_MS);
    return () => window.clearTimeout(id);
  }, [canInstall]);

  const dismiss = useLastCallback(() => {
    try {
      localStorage.setItem(DISMISS_KEY, String(Date.now()));
    } catch {
      // Ignore storage failures. The banner may reappear next session.
    }
    setShouldShow(false);
  });

  const handleInstall = useLastCallback(async () => {
    const promptInstall = getPromptInstall();
    if (!promptInstall) {
      dismiss();
      return;
    }

    await promptInstall();
    dismiss();
  });

  if (!shouldShow) return undefined;

  return (
    <div className={buildClassName(styles.root)} role="status" aria-live="polite">
      <Icon name="install" className={styles.icon} />
      <div className={styles.body}>
        <p className={styles.title}>{lang('InstallAppBanner.Title')}</p>
        <p className={styles.hint}>{lang('InstallAppBanner.Hint')}</p>
      </div>
      <Button
        className={styles.action}
        color="primary"
        size="smaller"
        onClick={handleInstall}
      >
        {lang('InstallAppBanner.Action')}
      </Button>
      <Button
        className={styles.close}
        round
        size="smaller"
        color="translucent"
        onClick={dismiss}
        ariaLabel={lang('Close')}
      >
        <Icon name="close" />
      </Button>
    </div>
  );
};

export default memo(withGlobal(
  (global): Complete<StateProps> => {
    return {
      canInstall: selectTabState(global).canInstall,
    };
  },
)(InstallAppBanner));
