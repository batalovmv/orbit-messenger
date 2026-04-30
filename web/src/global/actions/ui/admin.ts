// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import type { ActionReturnType } from '../../types';

import { getCurrentTabId } from '../../../util/establishMultitabRole';
import { addActionHandler } from '../../index';
import { updateTabState } from '../../reducers/tabs';
import { selectTabState } from '../../selectors';

addActionHandler('openAdminPanel', (global, actions, payload): ActionReturnType => {
  const { tab, tabId = getCurrentTabId() } = payload || {};
  return updateTabState(global, {
      adminPanel: {
        isOpen: true,
        tab: tab || 'users',
      },
  }, tabId);
});

addActionHandler('closeAdminPanel', (global, actions, payload): ActionReturnType => {
  const { tabId = getCurrentTabId() } = payload || {};
  const tabState = selectTabState(global, tabId);
  if (!tabState.adminPanel) return undefined;
  return updateTabState(global, {
    adminPanel: { isOpen: false, tab: tabState.adminPanel.tab },
  }, tabId);
});

addActionHandler('selectAdminTab', (global, actions, payload): ActionReturnType => {
  const { tab, tabId = getCurrentTabId() } = payload;
  const tabState = selectTabState(global, tabId);
  if (!tabState.adminPanel?.isOpen) return undefined;
  return updateTabState(global, {
    adminPanel: { isOpen: true, tab },
  }, tabId);
});
