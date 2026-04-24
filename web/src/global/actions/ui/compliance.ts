import type { ActionReturnType } from '../../types';

import { getCurrentTabId } from '../../../util/establishMultitabRole';
import { addActionHandler } from '../../index';
import { updateTabState } from '../../reducers/tabs';
import { selectTabState } from '../../selectors';

addActionHandler('openCompliancePanel', (global, actions, payload): ActionReturnType => {
  const { tabId = getCurrentTabId() } = payload || {};

  return updateTabState(global, {
    compliancePanel: {
      isOpen: true,
    },
  }, tabId);
});

addActionHandler('closeCompliancePanel', (global, actions, payload): ActionReturnType => {
  const { tabId = getCurrentTabId() } = payload || {};

  const tabState = selectTabState(global, tabId);
  if (!tabState.compliancePanel) return undefined;

  return updateTabState(global, {
    compliancePanel: {
      isOpen: false,
    },
  }, tabId);
});

addActionHandler('selectComplianceUser', (global, actions, payload): ActionReturnType => {
  const { userId, tabId = getCurrentTabId() } = payload;

  const tabState = selectTabState(global, tabId);
  if (!tabState.compliancePanel?.isOpen) return undefined;

  return updateTabState(global, {
    compliancePanel: {
      isOpen: true,
      userId,
      chatId: undefined,
    },
  }, tabId);
});

addActionHandler('selectComplianceChat', (global, actions, payload): ActionReturnType => {
  const { chatId, tabId = getCurrentTabId() } = payload;

  const tabState = selectTabState(global, tabId);
  if (!tabState.compliancePanel?.isOpen) return undefined;

  return updateTabState(global, {
    compliancePanel: {
      isOpen: true,
      userId: tabState.compliancePanel.userId,
      chatId,
    },
  }, tabId);
});
