import { getActions, getGlobal } from '../global';

import { DEBUG, DEBUG_MORE } from '../config';
import { IS_SERVICE_WORKER_SUPPORTED } from './browser/windowEnvironment';

// In development mode, unregister any existing service workers to prevent stale cache
if (DEBUG && IS_SERVICE_WORKER_SUPPORTED) {
  navigator.serviceWorker.getRegistrations().then((registrations) => {
    registrations.forEach((r) => r.unregister());
  });
}
import { formatShareText } from './deeplink';
import { validateFiles } from './files';
import { notifyClientReady, playNotifySoundDebounced, subscribe as subscribeToPush } from './notifications';

type WorkerAction = {
  type: string;
  payload: Record<string, any>;
};

const IGNORE_WORKER_PATH = '/k/';

// Stage 4: when SW notification is clicked, the call_incoming WS frame may not
// have arrived yet (fresh tab, or async order). Park the action and retry on a
// short interval until phoneCall.id matches in global state.
type PendingCallAction = {
  action: 'accept' | 'decline';
  callId: string;
  callMode: 'p2p' | 'group';
};
let pendingCallAction: PendingCallAction | undefined;
const CALL_ACTION_POLL_MS = 250;
const CALL_ACTION_MAX_WAIT_MS = 15000;

function dispatchPendingCallAction() {
  if (!pendingCallAction) return;
  const dispatch = getActions();
  const { action, callId, callMode } = pendingCallAction;

  if (callMode === 'group') {
    if (action === 'accept') {
      dispatch.connectToActiveGroupCall?.({ id: callId, accessHash: '' } as any);
    } else {
      dispatch.leaveGroupCall?.();
    }
    pendingCallAction = undefined;
    return;
  }

  // p2p — wait until phoneCall arrives in global state, then act on it.
  const global = getGlobal();
  if (global.phoneCall?.id === callId) {
    if (action === 'accept') {
      dispatch.acceptCall?.();
    } else {
      dispatch.hangUp?.({});
    }
    pendingCallAction = undefined;
  }
}

function schedulePendingCallActionRetry() {
  if (!pendingCallAction) return;
  const start = Date.now();
  const intervalId = window.setInterval(() => {
    if (!pendingCallAction || Date.now() - start > CALL_ACTION_MAX_WAIT_MS) {
      window.clearInterval(intervalId);
      pendingCallAction = undefined;
      return;
    }
    dispatchPendingCallAction();
    if (!pendingCallAction) {
      window.clearInterval(intervalId);
    }
  }, CALL_ACTION_POLL_MS);
}

function handleWorkerMessage(e: MessageEvent) {
  const action: WorkerAction = e.data;
  if (DEBUG_MORE) {
    // eslint-disable-next-line no-console
    console.log('[SW] Message from worker', action);
  }
  if (!action.type) return;
  const dispatch = getActions();
  const payload = action.payload;
  switch (action.type) {
    case 'callAction':
      pendingCallAction = {
        action: payload.action === 'decline' ? 'decline' : 'accept',
        callId: String(payload.callId),
        callMode: payload.callMode === 'group' ? 'group' : 'p2p',
      };
      dispatchPendingCallAction();
      if (pendingCallAction) schedulePendingCallActionRetry();
      break;
    case 'focusMessage':
      if (payload.messageId) {
        dispatch.focusMessage?.(payload as any);
      } else if (payload.chatId) {
        dispatch.openChat?.({
          id: payload.chatId,
          shouldReplaceHistory: payload.shouldReplaceHistory,
        });
      }
      break;
    case 'playNotificationSound':
      playNotifySoundDebounced(action.payload.id);
      break;
    case 'share':
      dispatch.openChatWithDraft({
        text: formatShareText(payload.url, payload.text, payload.title),
        files: validateFiles(payload.files),
      });
      break;
    case 'staleChunkDetected': {
      // A deploy has invalidated a cached JS chunk. Don't reload immediately —
      // the user may be mid-composition. Surface the existing update banner
      // (`isAppUpdateAvailable`) so they can finish typing and click "Refresh"
      // when ready; the banner path runs `prepareUpdateRescue` first.
      // eslint-disable-next-line no-console
      console.warn('[SW] Stale chunk detected after deploy, showing update banner');
      const global = getGlobal();
      if (!global.isAppUpdateAvailable) {
        dispatch.checkAppVersion?.({ force: true });
      }
      break;
    }
    case 'pushsubscriptionchange':
      // Browser rotated/expired the push subscription. Re-run the subscribe
      // pipeline so the new endpoint is registered with the backend.
      void subscribeToPush();
      break;
  }
}

function subscribeToWorker() {
  navigator.serviceWorker.removeEventListener('message', handleWorkerMessage);
  navigator.serviceWorker.addEventListener('message', handleWorkerMessage);
  // Notify web worker that client is ready to receive messages
  notifyClientReady();
}

async function waitForServiceWorkerController(timeout = 5000) {
  if (navigator.serviceWorker.controller) {
    return navigator.serviceWorker.controller;
  }

  return new Promise<ServiceWorker | null>((resolve) => {
    const cleanup = () => {
      navigator.serviceWorker.removeEventListener('controllerchange', handleControllerChange);
      window.clearTimeout(timeoutId);
    };

    const handleControllerChange = () => {
      cleanup();
      resolve(navigator.serviceWorker.controller);
    };

    const timeoutId = window.setTimeout(() => {
      cleanup();
      resolve(navigator.serviceWorker.controller);
    }, timeout);

    navigator.serviceWorker.addEventListener('controllerchange', handleControllerChange, { once: true });
  });
}

if (IS_SERVICE_WORKER_SUPPORTED && !DEBUG) {
  window.addEventListener('load', async () => {
    try {
      const controller = navigator.serviceWorker.controller;
      if (!controller || controller.scriptURL.includes(IGNORE_WORKER_PATH)) {
        const registrations = await navigator.serviceWorker.getRegistrations();
        const ourRegistrations = registrations.filter((r) => !r.scope.includes(IGNORE_WORKER_PATH));
        if (ourRegistrations.length) {
          if (DEBUG) {
            // eslint-disable-next-line no-console
            console.log('[SW] Hard reload detected, re-enabling Service Worker');
          }
          await Promise.all(ourRegistrations.map((r) => r.unregister()));
        }
      }

      // Stable script URL — see webpack.config.ts comment on the
      // `serviceWorker` entry. Per-deploy contenthashing here would
      // make each register() create a fresh registration alongside
      // the previous one instead of running the proper update flow.
      await navigator.serviceWorker.register('/serviceWorker.js');

      if (DEBUG) {
        // eslint-disable-next-line no-console
        console.log('[SW] ServiceWorker registered');
      }

      await navigator.serviceWorker.ready;

      // Wait for registration to be available
      await navigator.serviceWorker.getRegistration();

      const activeController = navigator.serviceWorker.controller || await waitForServiceWorkerController();

      if (activeController) {
        if (DEBUG) {
          // eslint-disable-next-line no-console
          console.log('[SW] ServiceWorker ready');
        }
        subscribeToWorker();
      } else {
        if (DEBUG) {
          // eslint-disable-next-line no-console
          console.error('[SW] ServiceWorker not available');
        }

        // Saturn uses REST for media, not SW streaming — skip the warning dialog
        // if (!IS_IOS && !IS_ANDROID && !IS_TEST) {
        //   getActions().showDialog?.({ data: { message: 'SERVICE_WORKER_DISABLED', hasErrorKey: true } });
        // }
      }
    } catch (err) {
      if (DEBUG) {
        // eslint-disable-next-line no-console
        console.error('[SW] ServiceWorker registration failed: ', err);
      }
    }
  });
  window.addEventListener('focus', async () => {
    await navigator.serviceWorker.ready;
    subscribeToWorker();
  });
}

// Stage 4: handle ?call_action=accept&call_id=…&call_mode=… set by SW openWindow
// when no app tab existed. Strip the params from the URL so a refresh doesn't
// re-trigger the action.
if (typeof window !== 'undefined' && window.location?.search) {
  try {
    const params = new URLSearchParams(window.location.search);
    const callAction = params.get('call_action');
    const callId = params.get('call_id');
    if (callAction && callId) {
      const callMode = params.get('call_mode') === 'group' ? 'group' : 'p2p';
      pendingCallAction = {
        action: callAction === 'decline' ? 'decline' : 'accept',
        callId,
        callMode,
      };
      params.delete('call_action');
      params.delete('call_id');
      params.delete('call_mode');
      const cleaned = params.toString();
      const newUrl = window.location.pathname + (cleaned ? `?${cleaned}` : '') + window.location.hash;
      window.history.replaceState({}, '', newUrl);
      schedulePendingCallActionRetry();
    }
  } catch (error) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.warn('[SW] failed to parse call_action params', error);
    }
  }
}
