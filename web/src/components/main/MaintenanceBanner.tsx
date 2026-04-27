// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// Renders the "идут технические работы" banner across the top of the app
// while feature_flags.maintenance_mode is enabled. The banner alone is
// informational — write-blocking is enforced server-side by the gateway,
// so a tampered client cannot escape it.

import { memo, useEffect } from '@teact';
import { getActions, withGlobal } from '../../global';

import useLang from '../../hooks/useLang';

import styles from './MaintenanceBanner.module.scss';

const POLL_INTERVAL_MS = 60_000;

type StateProps = {
  active: boolean;
  message?: string;
  blockWrites?: boolean;
};

const MaintenanceBanner = ({ active, message, blockWrites }: StateProps) => {
  const { refreshSaturnSystemConfig } = getActions();
  const lang = useLang();

  // Always poll: the banner is opt-in for the user but we want it to appear
  // within ~60 s of an admin enabling maintenance. The endpoint is public,
  // CDN-cached at the messaging service edge for free, so the cost is trivial.
  useEffect(() => {
    refreshSaturnSystemConfig();
    const id = window.setInterval(refreshSaturnSystemConfig, POLL_INTERVAL_MS);
    return () => window.clearInterval(id);
  }, [refreshSaturnSystemConfig]);

  if (!active) return undefined;

  const text = message?.trim() || lang('MaintenanceBannerDefault');

  return (
    <div className={styles.banner} role="status" aria-live="polite">
      <i className="icon icon-info" aria-hidden />
      <span className={styles.text}>{text}</span>
      {blockWrites && (
        <span className={styles.badge}>{lang('MaintenanceBannerWriteBlocked')}</span>
      )}
    </div>
  );
};

export default memo(withGlobal(
  (global): Complete<StateProps> => {
    const m = global.saturnSystem?.maintenance;
    return {
      active: Boolean(m?.active),
      message: m?.message,
      blockWrites: m?.blockWrites,
    };
  },
)(MaintenanceBanner));
