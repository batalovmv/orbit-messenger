import type { OnApiUpdate } from '../types';
import type { SaturnErrorResponse, SaturnLoginResponse, SaturnWsMessage } from './types';

const TOKEN_REFRESH_MARGIN_MS = 60 * 1000; // Refresh 60s before expiry
const WS_PING_INTERVAL_MS = 25 * 1000;
const WS_RECONNECT_BASE_MS = 1000;
const WS_RECONNECT_MAX_MS = 30 * 1000;

let baseUrl = '';
let accessToken: string | undefined;
let tokenExpiresAt = 0;
let refreshPromise: Promise<void> | undefined;
let onUpdate: OnApiUpdate | undefined;
let ws: WebSocket | undefined;
let wsPingInterval: ReturnType<typeof setInterval> | undefined;
let wsReconnectTimeout: ReturnType<typeof setTimeout> | undefined;
let wsReconnectDelay = WS_RECONNECT_BASE_MS;
let wsIntentionalClose = false;
let wsHasConnectedBefore = false;
let onReconnect: (() => void) | undefined;

export function init(apiUrl: string, updateCallback: OnApiUpdate) {
  baseUrl = apiUrl.replace(/\/$/, '');
  onUpdate = updateCallback;
}

export function getBaseUrl() {
  return baseUrl;
}

export function setAccessToken(token: string, expiresIn: number) {
  accessToken = token;
  tokenExpiresAt = Date.now() + expiresIn * 1000;
}

export function getAccessToken() {
  return accessToken;
}

export function clearAuth() {
  accessToken = undefined;
  tokenExpiresAt = 0;
}

async function ensureToken() {
  if (!accessToken) return;
  if (Date.now() < tokenExpiresAt - TOKEN_REFRESH_MARGIN_MS) return;

  if (!refreshPromise) {
    refreshPromise = refreshToken();
  }
  await refreshPromise;
}

async function refreshToken(): Promise<void> {
  try {
    const response = await fetch(`${baseUrl}/auth/refresh`, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
    });

    if (!response.ok) {
      clearAuth();
      onUpdate?.({ '@type': 'updateAuthorizationState', authorizationState: 'authorizationStateWaitPhoneNumber' });
      return;
    }

    const data: SaturnLoginResponse = await response.json();
    setAccessToken(data.access_token, data.expires_in);
  } catch {
    clearAuth();
    onUpdate?.({ '@type': 'updateAuthorizationState', authorizationState: 'authorizationStateWaitPhoneNumber' });
  } finally {
    refreshPromise = undefined;
  }
}

export async function request<T>(
  method: string,
  path: string,
  body?: Record<string, unknown>,
  options?: { noAuth?: boolean },
): Promise<T> {
  if (!options?.noAuth) {
    await ensureToken();
  }

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  };
  if (accessToken && !options?.noAuth) {
    headers.Authorization = `Bearer ${accessToken}`;
  }

  const response = await fetch(`${baseUrl}${path}`, {
    method,
    headers,
    credentials: 'include',
    body: body ? JSON.stringify(body) : undefined,
  });

  if (!response.ok) {
    const errorBody = await response.json().catch(() => ({
      error: 'unknown',
      message: response.statusText,
      status: response.status,
    })) as SaturnErrorResponse;
    throw new ApiError(errorBody.message || errorBody.error, response.status, errorBody.error);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  return response.json();
}

export class ApiError extends Error {
  status: number;
  code: string;

  constructor(message: string, status: number, code = 'unknown') {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

// WebSocket management

export function connectWs() {
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
    return;
  }
  if (!accessToken) return;

  wsIntentionalClose = false;
  const wsUrl = baseUrl.replace(/^http/, 'ws') + '/ws';
  ws = new WebSocket(wsUrl);

  ws.onopen = () => {
    // Send auth frame immediately — token is NOT in URL for security
    ws!.send(JSON.stringify({ type: 'auth', data: { token: accessToken } }));
    wsReconnectDelay = WS_RECONNECT_BASE_MS;
    startPing();
    onUpdate?.({ '@type': 'updateConnectionState', connectionState: 'connectionStateReady' });

    // On reconnect, re-fetch chats to sync missed updates
    if (wsHasConnectedBefore) {
      onReconnect?.();
    }
    wsHasConnectedBefore = true;
  };

  ws.onmessage = (event) => {
    try {
      const msg: SaturnWsMessage = JSON.parse(event.data as string);
      handleWsMessage(msg);
    } catch {
      // Ignore malformed messages
    }
  };

  ws.onclose = () => {
    stopPing();
    if (!wsIntentionalClose) {
      onUpdate?.({ '@type': 'updateConnectionState', connectionState: 'connectionStateConnecting' });
      scheduleReconnect();
    }
  };

  ws.onerror = () => {
    // onclose will fire after onerror
  };
}

export function disconnectWs() {
  wsIntentionalClose = true;
  if (wsReconnectTimeout) {
    clearTimeout(wsReconnectTimeout);
    wsReconnectTimeout = undefined;
  }
  stopPing();
  if (ws) {
    ws.close();
    ws = undefined;
  }
}

export function sendWsMessage(type: string, data: Record<string, unknown>) {
  if (ws?.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type, data }));
  }
}

function startPing() {
  stopPing();
  wsPingInterval = setInterval(() => {
    sendWsMessage('ping', {});
  }, WS_PING_INTERVAL_MS);
}

function stopPing() {
  if (wsPingInterval) {
    clearInterval(wsPingInterval);
    wsPingInterval = undefined;
  }
}

function scheduleReconnect() {
  if (wsReconnectTimeout) return;
  // Add ±25% jitter to avoid thundering herd on reconnect
  const jitter = wsReconnectDelay * (0.75 + Math.random() * 0.5);
  wsReconnectTimeout = setTimeout(() => {
    wsReconnectTimeout = undefined;
    wsReconnectDelay = Math.min(wsReconnectDelay * 2, WS_RECONNECT_MAX_MS);
    connectWs();
  }, jitter);
}

// Imported by wsHandler — set externally
let wsMessageHandler: ((msg: SaturnWsMessage) => void) | undefined;

export function setWsMessageHandler(handler: (msg: SaturnWsMessage) => void) {
  wsMessageHandler = handler;
}

export function setOnReconnect(handler: () => void) {
  onReconnect = handler;
}

function handleWsMessage(msg: SaturnWsMessage) {
  if (msg.type === 'pong') return;
  wsMessageHandler?.(msg);
}
