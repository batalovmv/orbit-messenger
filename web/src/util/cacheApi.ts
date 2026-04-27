import {
  LANG_CACHE_NAME,
  MEDIA_CACHE_BUDGET_DEFAULT_BYTES,
  MEDIA_CACHE_BUDGET_MAX_BYTES,
  MEDIA_CACHE_BUDGET_MIN_BYTES,
  MEDIA_CACHE_NAME,
  MEDIA_CACHE_NAME_AVATARS,
  MEDIA_PROGRESSIVE_CACHE_NAME,
} from '../config';
import { yieldToMain } from './browser/scheduler';
import { ACCOUNT_SLOT } from './multiaccount';

const cacheApi = self.caches;

const LAST_ACCESS_HEADER = 'X-Last-Access';
const CACHE_TTL = 5 * 24 * 60 * 60 * 1000; // 5 days
const ACCESS_THROTTLE = 24 * 60 * 60 * 1000; // 1 day
const CLEANUP_INTERVAL = 1 * 60 * 60 * 1000; // 1 hour
const QUOTA_INTERVAL = 10 * 60 * 1000; // 10 minutes
const STORAGE_PRESSURE_RATIO = 0.8;
// Per-bucket fairness inside the total media budget. Progressive media is the
// most disposable (rebuildable from server stream), avatars are smallest and
// most-reused → smallest share but evict last in the cross-bucket pass.
const BUCKET_SHARES: Record<string, number> = {
  [MEDIA_CACHE_NAME]: 0.65,
  [MEDIA_PROGRESSIVE_CACHE_NAME]: 0.25,
  [MEDIA_CACHE_NAME_AVATARS]: 0.10,
};
const CROSS_BUCKET_EVICT_ORDER = [
  MEDIA_PROGRESSIVE_CACHE_NAME, MEDIA_CACHE_NAME, MEDIA_CACHE_NAME_AVATARS,
];

const CLEARABLE_CACHE_NAMES = [MEDIA_CACHE_NAME, MEDIA_CACHE_NAME_AVATARS, MEDIA_PROGRESSIVE_CACHE_NAME];

cleanup(CLEARABLE_CACHE_NAMES);
void enforceQuota(CLEARABLE_CACHE_NAMES);
setInterval(() => {
  cleanup(CLEARABLE_CACHE_NAMES);
}, CLEANUP_INTERVAL);
setInterval(() => {
  void enforceQuota(CLEARABLE_CACHE_NAMES);
}, QUOTA_INTERVAL);
if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') {
      void enforceQuota(CLEARABLE_CACHE_NAMES);
    }
  });
}

let isSupported: boolean | undefined;

export async function isCacheApiSupported() {
  if (!cacheApi) return false;

  isSupported = isSupported ?? await cacheApi.has('test').then(() => true).catch(() => false);
  return isSupported;
}

export enum Type {
  Text,
  Blob,
  Json,
  ArrayBuffer,
}

function getCacheName(cacheName: string) {
  if (cacheName === LANG_CACHE_NAME) return cacheName;

  const suffix = ACCOUNT_SLOT ? `_${ACCOUNT_SLOT}` : '';
  return `${cacheName}${suffix}`;
}

export async function fetch(
  cacheName: string, key: string, type: Type, isHtmlAllowed = false,
) {
  if (!cacheApi) {
    return undefined;
  }

  try {
    // To avoid the error "Request scheme 'webdocument' is unsupported"
    const request = new Request(key.replace(/:/g, '_'));
    const cache = await cacheApi.open(getCacheName(cacheName));
    const response = await cache.match(request);
    if (!response) {
      return undefined;
    }

    const lastAccess = Number(response.headers.get(LAST_ACCESS_HEADER));
    const now = Date.now();
    if (!lastAccess || now - lastAccess > ACCESS_THROTTLE) {
      updateAccessTime(cache, request, response);
    }

    const contentType = response.headers.get('Content-Type');

    switch (type) {
      case Type.Text:
        return await response.text();
      case Type.Blob: {
        // Ignore deprecated data-uri avatars
        if (key.startsWith('avatar') && contentType && contentType.startsWith('text')) {
          return undefined;
        }

        const blob = await response.blob();
        const shouldRecreate = !blob.type || (!isHtmlAllowed && blob.type.includes('html'));
        // iOS Safari fails to preserve `type` in cache
        let resolvedType = blob.type || contentType;

        if (!(shouldRecreate && resolvedType)) {
          return blob;
        }

        // Prevent HTML-in-video attacks (for files that were cached before fix)
        if (!isHtmlAllowed) {
          resolvedType = resolvedType.replace(/html/gi, '');
        }

        return new Blob([blob], { type: resolvedType });
      }
      case Type.Json:
        return await response.json();
      case Type.ArrayBuffer:
        return await response.arrayBuffer();
      default:
        return undefined;
    }
  } catch (err) {
    // eslint-disable-next-line no-console
    console.warn(err);
    return undefined;
  }
}

export async function save(cacheName: string, key: string, data: AnyLiteral | Blob | ArrayBuffer | string) {
  if (!cacheApi) {
    return false;
  }

  try {
    const cacheData = typeof data === 'string' || data instanceof Blob || data instanceof ArrayBuffer
      ? data
      : JSON.stringify(data);
    // To avoid the error "Request scheme 'webdocument' is unsupported"
    const request = new Request(key.replace(/:/g, '_'));
    const response = new Response(cacheData);
    response.headers.set(LAST_ACCESS_HEADER, Date.now().toString());
    const cache = await cacheApi.open(getCacheName(cacheName));
    await cache.put(request, response);

    return true;
  } catch (err) {
    // eslint-disable-next-line no-console
    console.warn(err);
    return false;
  }
}

export async function remove(cacheName: string, key: string) {
  try {
    if (!cacheApi) {
      return undefined;
    }

    const cache = await cacheApi.open(getCacheName(cacheName));
    return await cache.delete(key);
  } catch (err) {
    // eslint-disable-next-line no-console
    console.warn(err);
    return undefined;
  }
}

export async function clear(cacheName: string) {
  try {
    if (!cacheApi) {
      return undefined;
    }

    return await cacheApi.delete(getCacheName(cacheName));
  } catch (err) {
    // eslint-disable-next-line no-console
    console.warn(err);
    return undefined;
  }
}

export async function cleanup(cacheNames: string[]) {
  if (!cacheApi) return;

  try {
    for (const cacheName of cacheNames) {
      const cache = await cacheApi.open(getCacheName(cacheName));
      const keys = await cache.keys();
      const now = Date.now();

      for (const request of keys) {
        await yieldToMain();
        const response = await cache.match(request);
        if (!response) continue;

        const lastAccess = Number(response.headers.get(LAST_ACCESS_HEADER));
        if (lastAccess && now - lastAccess > CACHE_TTL) {
          await cache.delete(request);
        }
      }
    }
  } catch (err) {
    // eslint-disable-next-line no-console
    console.warn(err);
  }
}

export function purgeClearableCache() {
  CLEARABLE_CACHE_NAMES.forEach((cacheName) => clear(cacheName));
}

// Resolve total media budget. Uses navigator.storage.estimate() when available
// to grow up to 15% of the origin quota — but never trust it as exact; it
// rolls IDB + Cache + everything else into one number and varies per browser.
async function resolveMediaBudget() {
  try {
    if (typeof navigator !== 'undefined' && navigator.storage && navigator.storage.estimate) {
      const { quota, usage } = await navigator.storage.estimate();
      if (quota && Number.isFinite(quota)) {
        let target = Math.round(quota * 0.15);
        target = Math.max(MEDIA_CACHE_BUDGET_MIN_BYTES, Math.min(MEDIA_CACHE_BUDGET_MAX_BYTES, target));
        // Storage pressure: shrink target when the origin is close to full so
        // we don't compete with IDB-stored global state for the last 20%.
        if (usage && quota && usage / quota > STORAGE_PRESSURE_RATIO) {
          target = Math.max(MEDIA_CACHE_BUDGET_MIN_BYTES, Math.round(target * 0.5));
        }
        return target;
      }
    }
  } catch {
    // ignore — fallback below
  }
  return MEDIA_CACHE_BUDGET_DEFAULT_BYTES;
}

type CacheEntry = { request: Request; size: number; lastAccess: number };

async function listEntries(cache: Cache): Promise<CacheEntry[]> {
  const entries: CacheEntry[] = [];
  const keys = await cache.keys();
  for (const request of keys) {
    await yieldToMain();
    const response = await cache.match(request);
    if (!response) continue;

    let size = 0;
    const contentLength = response.headers.get('Content-Length');
    if (contentLength) {
      const parsed = Number(contentLength);
      if (Number.isFinite(parsed) && parsed > 0) size = parsed;
    }
    if (!size) {
      try {
        // Cloning before consuming so the cached entry stays valid.
        const blob = await response.clone().blob();
        size = blob.size;
      } catch {
        size = 0;
      }
    }

    const lastAccess = Number(response.headers.get(LAST_ACCESS_HEADER)) || 0;
    entries.push({ request, size, lastAccess });
  }
  return entries;
}

export async function enforceQuota(cacheNames: string[]) {
  if (!cacheApi) return;

  try {
    const budget = await resolveMediaBudget();

    // Per-bucket pass: trim each bucket down to its share. Oldest first.
    const liveBuckets: { name: string; cache: Cache; entries: CacheEntry[]; total: number }[] = [];
    for (const cacheName of cacheNames) {
      const cache = await cacheApi.open(getCacheName(cacheName));
      const entries = await listEntries(cache);
      let total = 0;
      for (const e of entries) total += e.size;

      const share = BUCKET_SHARES[cacheName] ?? 0;
      const bucketLimit = Math.round(budget * share);

      if (total > bucketLimit) {
        entries.sort((a, b) => a.lastAccess - b.lastAccess);
        for (const entry of entries) {
          if (total <= bucketLimit) break;
          await yieldToMain();
          const deleted = await cache.delete(entry.request);
          if (deleted) total -= entry.size;
        }
      }

      // Rebuild after eviction so cross-bucket pass sees current state.
      const survivors = entries.filter((e) => e.size > 0);
      liveBuckets.push({
        name: cacheName, cache, entries: survivors, total,
      });
    }

    // Cross-bucket pass: if global total still exceeds budget, evict in
    // disposability order until under budget.
    let grandTotal = liveBuckets.reduce((acc, b) => acc + b.total, 0);
    if (grandTotal <= budget) return;

    for (const cacheName of CROSS_BUCKET_EVICT_ORDER) {
      const bucket = liveBuckets.find((b) => b.name === cacheName);
      if (!bucket) continue;

      bucket.entries.sort((a, b) => a.lastAccess - b.lastAccess);
      for (const entry of bucket.entries) {
        if (grandTotal <= budget) return;
        await yieldToMain();
        const deleted = await bucket.cache.delete(entry.request);
        if (deleted) {
          grandTotal -= entry.size;
          bucket.total -= entry.size;
        }
      }
    }
  } catch (err) {
    // eslint-disable-next-line no-console
    console.warn('[cacheApi] enforceQuota failed', err);
  }
}

async function updateAccessTime(cache: Cache, request: Request, response: Response) {
  try {
    const headers = new Headers(response.headers);
    headers.set(LAST_ACCESS_HEADER, Date.now().toString());
    const newResponse = new Response(response.clone().body, {
      status: response.status,
      statusText: response.statusText,
      headers,
    });
    await cache.put(request, newResponse);
  } catch (err) {
    // eslint-disable-next-line no-console
    console.warn(err);
  }
}
