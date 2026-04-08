import type { OnApiUpdate } from '../types';
import type { SaturnErrorResponse, SaturnLoginResponse, SaturnWsMessage } from './types';

import { DEBUG } from '../../config';

const TOKEN_REFRESH_MARGIN_MS = 60 * 1000; // Refresh 60s before expiry
const WS_PING_INTERVAL_MS = 25 * 1000;
const WS_RECONNECT_BASE_MS = 1000;
const WS_RECONNECT_MAX_MS = 30 * 1000;
const ACCESS_TOKEN_STORAGE_KEY = 'saturn_access_token';
const ACCESS_TOKEN_EXPIRES_AT_STORAGE_KEY = 'saturn_access_token_expires_at';

let baseUrl = '';
let accessToken: string | undefined;
let tokenExpiresAt = 0;
let refreshPromise: Promise<void> | undefined;
let onUpdate: OnApiUpdate | undefined;
const pendingRequests = new Map<string, Promise<unknown>>();

// Auth readiness gate: requests wait until initial auth check completes
let authReadyResolve: (() => void) | undefined;
let authReadyPromise: Promise<void> | undefined;

export function createAuthGate() {
  authReadyPromise = new Promise<void>((resolve) => {
    authReadyResolve = resolve;
  });
}

export function resolveAuthGate() {
  authReadyResolve?.();
  authReadyPromise = undefined;
  authReadyResolve = undefined;
}
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
  restorePersistedAccessToken();
}

export function getBaseUrl() {
  return baseUrl;
}

export function setAccessToken(token: string, expiresIn: number) {
  accessToken = token;
  tokenExpiresAt = Date.now() + expiresIn * 1000;
  persistAccessToken();
}

export function getAccessToken() {
  return accessToken;
}

export async function ensureAuth(): Promise<string | undefined> {
  if (authReadyPromise) {
    await authReadyPromise;
  }
  await ensureToken();
  return accessToken;
}

export function clearAuth() {
  accessToken = undefined;
  tokenExpiresAt = 0;
  clearPersistedAccessToken();
}

async function ensureToken() {
  // If token exists and is not near expiry, nothing to do
  if (accessToken && Date.now() < tokenExpiresAt - TOKEN_REFRESH_MARGIN_MS) return;

  // Token missing or near expiry — try to refresh
  if (!refreshPromise) {
    refreshPromise = refreshToken();
  }
  await refreshPromise;
}

async function refreshToken(): Promise<void> {
  try {
    const url = baseUrl || `${window.location.origin}/api/v1`;
    const response = await fetch(`${url}/auth/refresh`, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'XMLHttpRequest' },
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
  options?: { noAuth?: boolean; signal?: AbortSignal; skipAuthReady?: boolean },
): Promise<T> {
  if (options?.signal?.aborted) {
    throw createAbortError();
  }

  if (!options?.noAuth) {
    // Wait for initial auth check to complete before making authenticated requests
    if (authReadyPromise && !options?.skipAuthReady) {
      await authReadyPromise;
    }
    await ensureToken();
  }

  if (options?.signal?.aborted) {
    throw createAbortError();
  }

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    'X-Requested-With': 'XMLHttpRequest',
  };
  if (accessToken && !options?.noAuth) {
    headers.Authorization = `Bearer ${accessToken}`;
  }

  const effectiveBase = baseUrl || `${window.location.origin}/api/v1`;
  const response = await fetch(`${effectiveBase}${path}`, {
    method,
    headers,
    credentials: 'include',
    body: body ? JSON.stringify(body) : undefined,
    signal: options?.signal,
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

export function deduplicateRequest<T>(key: string, factory: () => Promise<T>): Promise<T> {
  const pending = pendingRequests.get(key);
  if (pending) {
    return pending as Promise<T>;
  }

  const requestPromise = factory()
    .finally(() => {
      if (pendingRequests.get(key) === requestPromise) {
        pendingRequests.delete(key);
      }
    });

  pendingRequests.set(key, requestPromise);

  return requestPromise;
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

function restorePersistedAccessToken() {
  if (typeof sessionStorage === 'undefined') {
    return;
  }

  try {
    const storedToken = sessionStorage.getItem(ACCESS_TOKEN_STORAGE_KEY);
    const storedExpiresAt = Number(sessionStorage.getItem(ACCESS_TOKEN_EXPIRES_AT_STORAGE_KEY) || 0);

    if (!storedToken || !storedExpiresAt || storedExpiresAt <= Date.now()) {
      clearPersistedAccessToken();
      return;
    }

    accessToken = storedToken;
    tokenExpiresAt = storedExpiresAt;
  } catch {
    clearPersistedAccessToken();
  }
}

function persistAccessToken() {
  if (typeof sessionStorage === 'undefined' || !accessToken || !tokenExpiresAt) {
    return;
  }

  try {
    sessionStorage.setItem(ACCESS_TOKEN_STORAGE_KEY, accessToken);
    sessionStorage.setItem(ACCESS_TOKEN_EXPIRES_AT_STORAGE_KEY, String(tokenExpiresAt));
  } catch {
    // Ignore storage failures and keep token in memory.
  }
}

function clearPersistedAccessToken() {
  if (typeof sessionStorage === 'undefined') {
    return;
  }

  try {
    sessionStorage.removeItem(ACCESS_TOKEN_STORAGE_KEY);
    sessionStorage.removeItem(ACCESS_TOKEN_EXPIRES_AT_STORAGE_KEY);
  } catch {
    // Ignore storage failures.
  }
}

function createAbortError() {
  if (typeof DOMException !== 'undefined') {
    return new DOMException('Request aborted', 'AbortError');
  }

  const error = new Error('Request aborted');
  error.name = 'AbortError';

  return error;
}

// WebSocket management

export function connectWs() {
  // eslint-disable-next-line no-console
  console.log('[Saturn WS] connectWs() called, ws state:', ws?.readyState, 'token:', accessToken ? 'present' : 'MISSING');
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
    // eslint-disable-next-line no-console
    console.log('[Saturn WS] Already connected/connecting, skipping');
    return;
  }
  if (!accessToken) {
    // eslint-disable-next-line no-console
    console.warn('[Saturn WS] connectWs called without access token');
    return;
  }

  wsIntentionalClose = false;
  const wsUrl = baseUrl.replace(/^http/, 'ws') + '/ws';
  // eslint-disable-next-line no-console
  console.log('[Saturn WS] Connecting to', wsUrl);
  ws = new WebSocket(wsUrl);

  ws.onopen = () => {
    // eslint-disable-next-line no-console
    console.log('[Saturn WS] Connected, sending auth frame');
    // Send auth frame immediately — token is NOT in URL for security
    ws!.send(JSON.stringify({ type: 'auth', data: { token: accessToken } }));
    wsReconnectDelay = WS_RECONNECT_BASE_MS;
    startPing();
    // Don't dispatch connectionStateConnecting here — WS is already open,
    // we're just waiting for auth_ok which will set connectionStateReady.
    // Dispatching 'connecting' here would show "Waiting for network" during auth handshake.
  };

  ws.onmessage = (event) => {
    try {
      const msg: SaturnWsMessage = JSON.parse(event.data as string);
      // eslint-disable-next-line no-console
      if (DEBUG && msg.type !== 'pong') console.log('[Saturn WS] Received:', msg.type);

      if (msg.type === 'auth_ok') {
        onUpdate?.({ '@type': 'updateConnectionState', connectionState: 'connectionStateReady' });
        if (wsHasConnectedBefore) {
          onReconnect?.();
        }
        wsHasConnectedBefore = true;
        return;
      }

      if (msg.type === 'error') {
        // eslint-disable-next-line no-console
        console.error('[Saturn WS] Server error:', msg.data);
        const errorData = msg.data as { message?: unknown } | undefined;
        const errorMsg = typeof errorData?.message === 'string'
          ? errorData.message.toLowerCase()
          : 'unknown error';
        if (errorMsg.includes('auth') || errorMsg.includes('token') || errorMsg.includes('unauthorized')) {
          onUpdate?.({ '@type': 'updateConnectionState', connectionState: 'connectionStateBroken' });
          wsIntentionalClose = true;
          ws?.close();
          return;
        }
      }

      handleWsMessage(msg);
    } catch {
      // Ignore malformed messages
    }
  };

  ws.onclose = (event) => {
    // eslint-disable-next-line no-console
    console.warn('[Saturn WS] Closed:', event.code, event.reason, 'intentional:', wsIntentionalClose);
    stopPing();
    if (!wsIntentionalClose) {
      onUpdate?.({ '@type': 'updateConnectionState', connectionState: 'connectionStateConnecting' });
      scheduleReconnect();
    }
  };

  ws.onerror = (event) => {
    // eslint-disable-next-line no-console
    console.error('[Saturn WS] Error:', event);
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
  wsReconnectTimeout = setTimeout(async () => {
    wsReconnectTimeout = undefined;
    wsReconnectDelay = Math.min(wsReconnectDelay * 2, WS_RECONNECT_MAX_MS);
    // Ensure token is valid before attempting WS reconnect
    await ensureToken();
    // If refresh failed (no access token), stop reconnecting — user will be signed out
    if (!accessToken) {
      wsIntentionalClose = true;
      onUpdate?.({ '@type': 'updateConnectionState', connectionState: 'connectionStateBroken' });
      return;
    }
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
