// Small helper to read the current logged-in user id without dragging
// the full global state machinery into the Safety Numbers modal.
//
// `getGlobal()` is a non-reactive read which is allowed for one-off
// lookups — this matches the global state overview in web/CLAUDE.md:
// "Use `getGlobal` only inside hooks for one-off reads".

import { getGlobal } from '../../global';

export function getCurrentUserIdForSafetyNumbers(): string | undefined {
  return getGlobal().currentUserId;
}
