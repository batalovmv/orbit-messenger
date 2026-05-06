// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import type { OnApiUpdate } from '../types';
import type { SaturnErrorResponse, SaturnLoginResponse, SaturnWsMessage } from './types';

import { DEBUG } from '../../config';
import {
  clearSaturnSessionHint,
  hasSaturnSessionHint,
  isOfflineNetworkError,
  setSaturnSessionHint,
} from '../../util/saturnSession';

const TOKEN_REFRESH_MARGIN_MS = 60 * 1000; // Refresh 60s before expiry
const WS_PING_INTERVAL_MS = 25 * 1000;
const WS_RECONNECT_BASE_MS = 1000;
const WS_RECONNECT_MAX_MS = 30 * 1000;
const ACCESS_TOKEN_STORAGE_KEY = 'saturn_access_token';
const ACCESS_TOKEN_EXPIRES_AT_STORAGE_KEY = 'saturn_access_token_expires_at';
const SESSION_ID_STORAGE_KEY = 'saturn_session_id';

let cachedSessionId: string | undefined;

// getSessionId returns a per-tab opaque identifier persisted in sessionStorage.
// It is sent to the backend in the WS auth frame and the X-Session-ID REST
// header so server-published events that originated on this tab can be
// excluded from the cross-device fanout (no echo to the device that just
// performed the action). sessionStorage is per-tab, which is exactly what we
// want — opening the app in a second tab gets a distinct id and behaves like
// a separate device.
export function getSessionId(): string {
  if (cachedSessionId) return cachedSessionId;
  try {
    const existing = sessionStorage.getItem(SESSION_ID_STORAGE_KEY);
    if (existing) {
      cachedSessionId = existing;
      return existing;
    }
  } catch { /* storage disabled — fall through to ephemeral id */ }

  const generated = generateSessionId();
  cachedSessionId = generated;
  try {
    sessionStorage.setItem(SESSION_ID_STORAGE_KEY, generated);
  } catch { /* noop */ }
  return generated;
}

function generateSessionId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  // Fallback for very old browsers — collision risk is acceptable for a
  // tab-scoped, ephemeral ID that the server only uses for echo suppression.
  return `s-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

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
let wsReconnectFireAt = 0;
let wsIntentionalClose = false;
let wsHasConnectedBefore = false;
let onReconnect: (() => void) | undefined;
const WS_KICK_COOLDOWN_MS = 5000;

export function init(apiUrl: string, updateCallback: OnApiUpdate) {
  baseUrl = apiUrl.replace(/\/$/, '');
  onUpdate = updateCallback;
  restorePersistedAccessToken();
}

// absorbOIDCAccessTokenFromUrl reads the access_token + expires_in query
// params left behind by the auth service's OIDC callback redirect, primes
// the in-memory token store, then strips the params from history so the
// SPA URL stays clean (no token in the visible address bar, no risk of
// the token surviving a copy-paste of the URL).
//
// Returns true when a token was absorbed — caller can short-circuit any
// initial refresh round-trip in that case.
export function absorbOIDCAccessTokenFromUrl(): boolean {
  if (typeof window === 'undefined' || !window.location.search) return false;
  const params = new URLSearchParams(window.location.search);
  const token = params.get('access_token');
  const expiresIn = Number(params.get('expires_in') || 0);
  if (!token || !expiresIn) return false;

  setAccessToken(token, expiresIn);

  // Strip only the OIDC params; keep the rest of the query intact so any
  // pre-existing app state encoded in the URL (e.g. ?to=...) survives.
  params.delete('access_token');
  params.delete('expires_in');
  const remaining = params.toString();
  const newUrl = window.location.pathname
    + (remaining ? `?${remaining}` : '')
    + window.location.hash;
  try {
    window.history.replaceState(window.history.state, '', newUrl);
  } catch {
    // History API can fail in odd embedding contexts — token is already
    // in memory, the address bar staying as-is is a cosmetic miss.
  }
  return true;
}

export function getBaseUrl() {
  return baseUrl;
}

export function setAccessToken(token: string, expiresIn: number) {
  accessToken = token;
  tokenExpiresAt = Date.now() + expiresIn * 1000;
  persistAccessToken();
  setSaturnSessionHint();
}

export function getAccessToken() {
  return accessToken;
}

export function hasSessionHint(): boolean {
  return hasSaturnSessionHint();
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
  clearSaturnSessionHint();
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
    // Notify the UI: when refresh races a stale-token request mid-session,
    // setAccessToken alone leaves Auth components stuck on the loading state
    // (the apiUpdaters/initial reducer only flips isLoading when an
    // authorizationState update arrives). Without this, JWT expiry forces
    // users to hard-reload to escape the spinner.
    onUpdate?.({ '@type': 'updateAuthorizationState', authorizationState: 'authorizationStateReady' });
  } catch (error) {
    if (hasSessionHint() && isOfflineNetworkError(error)) {
      onUpdate?.({ '@type': 'updateConnectionState', connectionState: 'connectionStateConnecting' });
      return;
    }

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
  options?: {
    noAuth?: boolean;
    signal?: AbortSignal;
    skipAuthReady?: boolean;
    headers?: Record<string, string>;
  },
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
    'X-Session-ID': getSessionId(),
  };
  if (accessToken && !options?.noAuth) {
    headers.Authorization = `Bearer ${accessToken}`;
  }
  if (options?.headers) {
    for (const [k, v] of Object.entries(options.headers)) {
      headers[k] = v;
    }
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

  // Successful REST reply is proof the backend is reachable. If the WS client
  // is stuck in a long exponential backoff after a transient outage, force an
  // immediate reconnect instead of waiting out the current timer.
  kickWsReconnect();

  if (response.status === 204) {
    return undefined as T;
  }

  return response.json();
}

function kickWsReconnect() {
  // Only relevant when WS is not currently open.
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
    return;
  }
  // Ignore if the app explicitly disconnected (logout, etc).
  if (wsIntentionalClose) return;
  // Cooldown: if a reconnect is already due to fire within the next few
  // seconds, let it run — otherwise REST chatter (which kicks here on every
  // 200) collapses the backoff to base every time and makes the WS thrash
  // through stale-token auth retries dozens of times per minute.
  if (wsReconnectTimeout && wsReconnectFireAt - Date.now() < WS_KICK_COOLDOWN_MS) {
    return;
  }
  // REST confirms the server is back — reset backoff to base before retrying.
  wsReconnectDelay = WS_RECONNECT_BASE_MS;
  if (wsReconnectTimeout) {
    clearTimeout(wsReconnectTimeout);
    wsReconnectTimeout = undefined;
  }
  scheduleReconnect();
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
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
    return;
  }
  if (!accessToken) {
    // eslint-disable-next-line no-console
    console.warn('[Saturn WS] connectWs called without access token');
    return;
  }

  wsIntentionalClose = false;
  const wsUrl = baseUrl.replace(/^http/, 'ws') + '/ws';
  if (DEBUG) {
    // eslint-disable-next-line no-console
    console.log('[Saturn WS] Connecting to', wsUrl);
  }
  ws = new WebSocket(wsUrl);

  ws.onopen = () => {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.log('[Saturn WS] Connected, sending auth frame');
    }
    // Send auth frame immediately — token is NOT in URL for security.
    // session_id lets the gateway exclude this connection from cross-device
    // sync fanout (read receipts, etc) so we don't receive our own echoes.
    ws!.send(JSON.stringify({ type: 'auth', data: { token: accessToken, session_id: getSessionId() } }));
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
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.warn('[Saturn WS] Closed:', event.code, event.reason, 'intentional:', wsIntentionalClose);
    }
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
  wsReconnectFireAt = Date.now() + jitter;
  wsReconnectTimeout = setTimeout(async () => {
    wsReconnectTimeout = undefined;
    wsReconnectFireAt = 0;
    wsReconnectDelay = Math.min(wsReconnectDelay * 2, WS_RECONNECT_MAX_MS);
    // Ensure token is valid before attempting WS reconnect
    await ensureToken();
    // If refresh failed (no access token), stop reconnecting — user will be signed out
    if (!accessToken) {
      if (hasSessionHint() && typeof navigator !== 'undefined' && !navigator.onLine) {
        onUpdate?.({ '@type': 'updateConnectionState', connectionState: 'connectionStateConnecting' });
        scheduleReconnect();
        return;
      }

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
