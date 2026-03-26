import type {
  GlobalState, TabArgs,
  TabState,
} from '../types';

import { getCurrentTabId } from '../../util/establishMultitabRole';

export function selectTabState<T extends GlobalState>(
  global: T, ...[tabId = getCurrentTabId()]: TabArgs<T>
): TabState {
  // Safety guard: if tab not yet registered, return first available tab state or empty object
  return global.byTabId[tabId]
    || Object.values(global.byTabId)[0]
    || {} as TabState;
}
