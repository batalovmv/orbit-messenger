import type { ApiInitialArgs, OnApiUpdate } from '../../types';

import * as saturnClient from '../client';
import { init as initUpdateEmitter, sendApiUpdate } from '../updates/apiUpdateEmitter';
import { initWsHandler, setWsCurrentUserId } from '../updates/wsHandler';
import { checkAuth } from './auth';
import { fetchChats, setCurrentUserId as setChatUserId } from './chats';
import { setCurrentUserId as setMsgUserId } from './messages';
import { setCurrentUserId as setSearchUserId } from './search';

export function init(initialArgs: ApiInitialArgs, onUpdate: OnApiUpdate) {
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
  setSearchUserId(userId);
  setWsCurrentUserId(userId);
}

export function destroy(noLogOut = false, _noClearLocalDb = false) {
  saturnClient.disconnectWs();

  if (!noLogOut) {
    saturnClient.clearAuth();
  }
}

export function disconnect() {
  saturnClient.disconnectWs();
}
