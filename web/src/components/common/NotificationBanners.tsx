import { memo } from '../../lib/teact/teact';
import { withGlobal } from '../../global';

import type { InAppNotificationBanner } from '../../global/types';

import { selectTabState } from '../../global/selectors';

import NotificationBanner from '../ui/NotificationBanner';

import styles from './NotificationBanners.module.scss';

type StateProps = {
  notificationBanners: InAppNotificationBanner[];
};

const EMPTY_BANNERS: InAppNotificationBanner[] = [];

const NotificationBanners = ({ notificationBanners }: StateProps) => {
  if (!notificationBanners.length) {
    return undefined;
  }

  return (
    <div className={styles.root}>
      {notificationBanners.map((banner) => (
        <NotificationBanner key={banner.localId} banner={banner} />
      ))}
    </div>
  );
};

export default memo(withGlobal(
  (global): Complete<StateProps> => ({
    notificationBanners: selectTabState(global).notificationBanners || EMPTY_BANNERS,
  }),
)(NotificationBanners));
