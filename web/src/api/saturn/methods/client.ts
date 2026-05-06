// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import type { ApiInitialArgs, OnApiUpdate } from '../../types';

import * as saturnClient from '../client';
import { init as initUpdateEmitter, sendApiUpdate } from '../updates/apiUpdateEmitter';
import { initWsHandler, setWsCurrentUserId } from '../updates/wsHandler';
import { checkAuth } from './auth';
import { setCurrentUserId as setChatUserId } from './chats';
import { setCurrentUserId as setMsgUserId } from './messages';
import { setCurrentUserId as setSearchUserId } from './search';

export function init(initialArgs: ApiInitialArgs, onUpdate: OnApiUpdate) {
  // Both production (nginx) and development (webpack proxy) route /api/* to gateway
  const apiUrl = `${window.location.origin}/api/v1`;

  saturnClient.init(apiUrl, onUpdate);
  // OIDC callback hands the SPA an access_token via querystring. Pulling
  // it into the in-memory token store before checkAuth runs lets the
  // rest of the boot sequence treat the SSO landing as a normal logged-in
  // session — no extra refresh, no UI flash through the login screen.
  saturnClient.absorbOIDCAccessTokenFromUrl();
  initUpdateEmitter(onUpdate);
  initWsHandler();

  // On WS reconnect, trigger a full sync so missed messages are fetched
  saturnClient.setOnReconnect(() => {
    sendApiUpdate({ '@type': 'requestSync' });
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
