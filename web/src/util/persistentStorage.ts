import { DEBUG } from '../config';

let isRequested = false;

// Ask the browser to mark our origin's IndexedDB and Cache Storage as
// "persistent" so the user agent will not evict it under storage pressure.
// Without this, an offline-first messenger can lose chats and queued media
// when the device is low on disk. The browser may grant immediately
// (installed PWA, bookmarked, high engagement) or silently deny — either
// way we run this once and don't surface a UI.
export async function requestPersistentStorage(): Promise<boolean | undefined> {
  if (isRequested) return undefined;
  isRequested = true;

  if (typeof navigator === 'undefined' || !navigator.storage?.persist) {
    return undefined;
  }

  try {
    const already = await navigator.storage.persisted?.();
    if (already) return true;

    const granted = await navigator.storage.persist();
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.log('[STORAGE] persistent storage granted:', granted);
    }
    return granted;
  } catch (err) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.warn('[STORAGE] persistent storage request failed', err);
    }
    return undefined;
  }
}
