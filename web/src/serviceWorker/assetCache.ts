import { ASSET_CACHE_NAME } from '../config';
import { pause } from '../util/schedulers';

declare const self: ServiceWorkerGlobalScope;

// An attempt to fix freezing UI on iOS
const TIMEOUT = 3000;

export async function respondWithCacheNetworkFirst(e: FetchEvent) {
  const remote = await withTimeout(() => fetch(e.request), TIMEOUT);
  if (!remote?.ok) {
    return respondWithCache(e);
  }

  const toCache = remote.clone();
  self.caches.open(ASSET_CACHE_NAME).then((cache) => {
    return cache?.put(e.request, toCache);
  });

  return remote;
}

export async function respondWithCache(e: FetchEvent) {
  const cacheResult = await withTimeout(async () => {
    const cache = await self.caches.open(ASSET_CACHE_NAME);
    const cached = await cache.match(e.request);

    return { cache, cached };
  }, TIMEOUT);

  const { cache, cached } = cacheResult || {};

  if (cache && cached) {
    if (cached.ok) {
      // Validate cached content-hashed assets by checking if they still exist on the server.
      // After a deploy, old chunk hashes become 404 — serve stale cache but trigger a reload.
      return cached;
    } else {
      await cache.delete(e.request);
    }
  }

  let remote: Response;
  try {
    remote = await fetch(e.request);
  } catch {
    // Offline or network failure with no usable cache entry.
    // Fall back to the cached app shell for navigation requests so the SPA
    // boots and shows a controlled offline state instead of the browser's
    // default error page.
    if (cache && (e.request.mode === 'navigate' || e.request.destination === 'document')) {
      const shell = await cache.match('/') || await cache.match(self.registration.scope);
      if (shell) return shell;
    }
    return Response.error();
  }

  if (remote.ok && cache) {
    cache.put(e.request, remote.clone());
  }

  // If a content-hashed asset returns 404, the deploy has invalidated this chunk.
  // Clear the entire asset cache and notify clients to reload.
  if (!remote.ok && remote.status === 404 && e.request.url.match(/[\da-f]{20}/)) {
    // eslint-disable-next-line no-console
    console.warn('[SW] Stale chunk detected (404), clearing cache:', e.request.url);
    await clearAssetCache();
    const clients = await self.clients.matchAll({ type: 'window' });
    clients.forEach((client) => {
      client.postMessage({ type: 'staleChunkDetected' });
    });
  }

  return remote;
}

async function withTimeout<T>(cb: () => Promise<T>, timeout: number) {
  let isResolved = false;

  try {
    return await Promise.race([
      pause(timeout).then(() => (isResolved ? undefined : Promise.reject(new Error('TIMEOUT')))),
      cb(),
    ]);
  } catch (err) {
    // eslint-disable-next-line no-console
    console.error(err);
    return undefined;
  } finally {
    isResolved = true;
  }
}

export function clearAssetCache() {
  return self.caches.delete(ASSET_CACHE_NAME);
}
