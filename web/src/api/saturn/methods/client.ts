import type { ApiInitialArgs, OnApiUpdate } from '../../types';

import * as saturnClient from '../client';
import { init as initUpdateEmitter } from '../updates/apiUpdateEmitter';
import { initWsHandler, setWsCurrentUserId } from '../updates/wsHandler';
import { setCurrentUserId as setChatUserId, fetchChats } from './chats';
import { setCurrentUserId as setMsgUserId } from './messages';

let currentOnUpdate: OnApiUpdate | undefined;

export function init(initialArgs: ApiInitialArgs, onUpdate: OnApiUpdate) {
  currentOnUpdate = onUpdate;

  // API URL injected by webpack DefinePlugin at build time
  const apiUrl = process.env.SATURN_API_URL || 'http://localhost:8080/api/v1';

  saturnClient.init(apiUrl, onUpdate);
  initUpdateEmitter(onUpdate);
  initWsHandler();

  // On WS reconnect, re-fetch chats to sync any missed updates
  saturnClient.setOnReconnect(() => {
    fetchChats({ limit: 50 }).catch(() => {});
  });
}

export function setCurrentUser(userId: string) {
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
