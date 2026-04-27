// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// Periodically refreshes /system/config so the rest of the UI can react to
// maintenance mode and feature-flag changes without page reload. The poll
// runs every 60 s while the tab is visible — enough to surface a freshly
// enabled banner inside one minute, low-rate enough to be invisible on the
// network panel.

import { addActionHandler, setGlobal } from '../../index';

import {
  fetchPublicSystemConfig, fetchSystemConfig,
} from '../../../api/saturn/methods/admin';

addActionHandler('refreshSaturnSystemConfig', async (global): Promise<void> => {
  // Logged-in users get the auth-exposed config; pre-login screens fall back
  // to the public endpoint. The shape is the same — only the flag set differs.
  const fetcher = global.currentUserId ? fetchSystemConfig : fetchPublicSystemConfig;
  try {
    const cfg = await fetcher();
    const flags: Record<string, boolean> = {};
    for (const f of cfg.flags || []) {
      flags[f.key] = Boolean(f.enabled);
    }
    global = {
      ...global,
      saturnSystem: {
        maintenance: {
          active: Boolean(cfg.maintenance?.active),
          message: cfg.maintenance?.message,
          blockWrites: Boolean(cfg.maintenance?.block_writes),
        },
        flags,
        fetchedAt: Date.now(),
      },
    };
    setGlobal(global);
  } catch (_e) {
    // Swallowing the error is intentional: a transient /system/config 5xx
    // must NOT block messaging. The next poll will retry.
  }
});
