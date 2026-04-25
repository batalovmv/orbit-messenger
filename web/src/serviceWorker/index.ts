import { DEBUG } from '../config';
import { pause } from '../util/schedulers';
import { clearAssetCache, respondWithCache, respondWithCacheNetworkFirst } from './assetCache';
import { respondForDownload } from './download';
import { respondForProgressive } from './progressive';
import {
  handleClientMessage as handleNotificationMessage,
  handleNotificationClick,
  handlePush,
} from './pushNotification';
import { handleClientMessage as handleShareMessage, respondForShare } from './share';

declare const self: ServiceWorkerGlobalScope;

const RE_NETWORK_FIRST_ASSETS = /\.(wasm|html)$/;
const RE_CACHE_FIRST_ASSETS = /[\da-f]{20}.*\.(js|css|woff2?|svg|png|jpg|jpeg|tgs|json|wasm)$/;
const ACTIVATE_TIMEOUT = 3000;

self.addEventListener('install', (e) => {
  if (DEBUG) {
    // eslint-disable-next-line no-console
    console.log('ServiceWorker installed');
  }

  // Activate worker immediately
  e.waitUntil(self.skipWaiting());
});

self.addEventListener('activate', (e) => {
  if (DEBUG) {
    // eslint-disable-next-line no-console
    console.log('ServiceWorker activated');
  }

  // Always clear asset cache on activate to prevent stale chunks after deploy.
  // clearAssetCache must complete before claiming clients — don't let the timeout skip it.
  e.waitUntil(
    clearAssetCache().catch(() => {}).then(() => {
      return Promise.race([
        // An attempt to fix freezing UI on iOS
        pause(ACTIVATE_TIMEOUT),
        self.clients.claim(),
      ]);
    }),
  );
});

self.addEventListener('fetch', (e: FetchEvent) => {
  const { url } = e.request;
  const { scope } = self.registration;
  if (!url.startsWith(scope)) {
    return false;
  }

  const { pathname, protocol } = new URL(url);
  const { pathname: scopePathname } = new URL(scope);

  if (pathname.includes('/progressive/')) {
    e.respondWith(respondForProgressive(e));
    return true;
  }

  if (pathname.includes('/download/')) {
    e.respondWith(respondForDownload(e));
    return true;
  }

  if (pathname.includes('/share/')) {
    e.respondWith(respondForShare(e));
  }

  if (protocol === 'http:' || protocol === 'https:') {
    if (pathname === scopePathname || pathname.match(RE_NETWORK_FIRST_ASSETS)) {
      e.respondWith(respondWithCacheNetworkFirst(e));
      return true;
    }

    if (pathname.match(RE_CACHE_FIRST_ASSETS)) {
      e.respondWith(respondWithCache(e));
      return true;
    }
  }

  return false;
});

self.addEventListener('push', handlePush);
self.addEventListener('notificationclick', handleNotificationClick);
self.addEventListener('message', (event) => {
  handleNotificationMessage(event);
  handleShareMessage(event);
});

// Browser-managed push subscriptions can expire or rotate; without a handler
// here, push delivery silently dies until the user manually re-subscribes.
// We re-subscribe in the SW (using the previous applicationServerKey, which
// the browser preserves on the old subscription) and notify any open client
// to push the new endpoint to the backend. If no client is open, the next
// `subscribe()` pipeline run on tab open detects the missing subscription
// and registers the new one through the standard /push/subscribe flow.
self.addEventListener('pushsubscriptionchange', (event) => {
  const e = event as Event & {
    oldSubscription?: PushSubscription;
    newSubscription?: PushSubscription;
    waitUntil: (p: Promise<unknown>) => void;
  };
  e.waitUntil((async () => {
    try {
      let next: PushSubscription | null | undefined = e.newSubscription;
      if (!next) {
        const applicationServerKey = e.oldSubscription?.options?.applicationServerKey;
        if (applicationServerKey) {
          next = await self.registration.pushManager.subscribe({
            userVisibleOnly: true,
            applicationServerKey,
          });
        }
      }

      const clients = await self.clients.matchAll({ type: 'window' });
      const payload = next ? {
        endpoint: next.endpoint,
        keys: next.toJSON().keys,
      } : undefined;
      clients.forEach((client) => {
        client.postMessage({
          type: 'pushsubscriptionchange',
          payload,
        });
      });
    } catch (error) {
      if (DEBUG) {
        // eslint-disable-next-line no-console
        console.error('[SW] pushsubscriptionchange handler failed', error);
      }
    }
  })());
});
