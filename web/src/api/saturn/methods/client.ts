import type { ApiInitialArgs, OnApiUpdate } from '../../types';

import * as saturnClient from '../client';
import { init as initUpdateEmitter, sendApiUpdate } from '../updates/apiUpdateEmitter';
import { initWsHandler, setWsCurrentUserId } from '../updates/wsHandler';
import { checkAuth } from './auth';
import { setCurrentUserId as setChatUserId, fetchChats } from './chats';
import { setCurrentUserId as setMsgUserId } from './messages';

let currentOnUpdate: OnApiUpdate | undefined;

export function init(initialArgs: ApiInitialArgs, onUpdate: OnApiUpdate) {
  currentOnUpdate = onUpdate;

  // Both production (nginx) and development (webpack proxy) route /api/* to gateway
  const apiUrl = `${window.location.origin}/api/v1`;

  saturnClient.init(apiUrl, onUpdate);
  initUpdateEmitter(onUpdate);
  initWsHandler();

  // On WS reconnect, re-fetch chats to sync any missed updates
  saturnClient.setOnReconnect(() => {
    fetchChats({ limit: 50 }).catch(() => {});
  });

  // Create auth gate — authenticated requests will wait until checkAuth completes
  saturnClient.createAuthGate();

  // Restore auth session on page reload (refresh token from cookie).
  // Runs async — doesn't block initApi so isInited becomes true immediately.
  // The auth gate ensures requests wait for the token before firing.
  checkAuth().then((isAuthed) => {
    saturnClient.resolveAuthGate();
    if (isAuthed) {
      sendApiUpdate({ '@type': 'requestSync' });
    }
  }).catch(() => {
    saturnClient.resolveAuthGate();
  });
}

export function setCurrentUser(userId: string) {
  // eslint-disable-next-line no-console
  console.log('[Saturn] setCurrentUser:', userId);
  setChatUserId(userId);
  setMsgUserId(userId);
  setWsCurrentUserId(userId);
}

export function destroy() {
  saturnClient.disconnectWs();
  saturnClient.clearAuth();
}

export function disconnect() {
  saturnClient.disconnectWs();
}
