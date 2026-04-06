import type { GlobalState, TabArgs, TabState } from '../types';

import { getCurrentTabId } from '../../util/establishMultitabRole';
import { INITIAL_TAB_STATE } from '../initialState';

export function updateTabState<T extends GlobalState>(
  global: T,
  tabStatePartial: Partial<TabState>,
  ...[tabId = getCurrentTabId()]: TabArgs<T>
): T {
  // Safety: if the tab was removed from byTabId (e.g. by another tab's init),
  // use INITIAL_TAB_STATE as base to avoid spreading undefined and creating
  // partial state objects missing required nested fields (payment, inlineBots, etc.)
  const existingTabState = global.byTabId[tabId] || INITIAL_TAB_STATE;
  return {
    ...global,
    byTabId: {
      ...global.byTabId,
      [tabId]: {
        ...existingTabState,
        ...tabStatePartial,
      },
    },
  };
}
