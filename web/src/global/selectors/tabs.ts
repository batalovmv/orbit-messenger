import type {
  GlobalState, TabArgs,
  TabState,
} from '../types';

import { getCurrentTabId } from '../../util/establishMultitabRole';
import { INITIAL_TAB_STATE } from '../initialState';

export function selectTabState<T extends GlobalState>(
  global: T, ...[tabId = getCurrentTabId()]: TabArgs<T>
): TabState {
  // Safety guard: if tab not yet registered, return first available tab state
  // Using INITIAL_TAB_STATE as fallback ensures all nested objects (payment, etc.) exist
  return global.byTabId[tabId]
    || Object.values(global.byTabId)[0]
    || INITIAL_TAB_STATE;
}
