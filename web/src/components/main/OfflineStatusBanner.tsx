import { memo, useEffect, useState } from '../../lib/teact/teact';

import useLang from '../../hooks/useLang';

import Icon from '../common/icons/Icon';

import styles from './OfflineStatusBanner.module.scss';

const OfflineStatusBanner = () => {
  const lang = useLang();
  const [isOffline, setIsOffline] = useState(() => typeof navigator !== 'undefined' && !navigator.onLine);

  useEffect(() => {
    const updateOnlineState = () => {
      setIsOffline(!navigator.onLine);
    };

    window.addEventListener('online', updateOnlineState);
    window.addEventListener('offline', updateOnlineState);

    return () => {
      window.removeEventListener('online', updateOnlineState);
      window.removeEventListener('offline', updateOnlineState);
    };
  }, []);

  if (!isOffline) return undefined;

  return (
    <div className={styles.root} role="status" aria-live="polite">
      <Icon name="warning" className={styles.icon} />
      <div className={styles.body}>
        <p className={styles.title}>{lang('OfflineStatusBanner.Title')}</p>
        <p className={styles.hint}>{lang('OfflineStatusBanner.Hint')}</p>
      </div>
    </div>
  );
};

export default memo(OfflineStatusBanner);
