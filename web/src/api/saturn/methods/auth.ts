// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import type { SaturnLoginResponse, SaturnUser } from '../types';

import { isOfflineNetworkError } from '../../../util/saturnSession';
import { buildApiUser, buildApiUserFullInfo, buildApiUserStatus } from '../apiBuilders/users';
import * as client from '../client';
import { sendApiUpdate } from '../updates/apiUpdateEmitter';

export type OIDCConfigResponse = {
  enabled: boolean;
  providerKey: string;
  displayName: string;
};

// fetchOIDCConfig probes the auth service for a configured SSO provider.
// Always returns a response — when SSO is off the backend responds with
// enabled:false rather than 404 so the SPA has one consistent decode path.
// Used by the sign-in screen to decide whether to render the SSO button.
export async function fetchOIDCConfig() {
  try {
    return await client.request<OIDCConfigResponse>(
      'GET', '/auth/oidc/config', undefined, { noAuth: true, skipAuthReady: true },
    );
  } catch {
    // Network or 5xx — degrade gracefully to "no SSO" so the rest of the
    // login screen still works.
    return { enabled: false, providerKey: '', displayName: '' };
  }
}

// startOIDCAuthorize navigates the browser to the auth service's
// /authorize endpoint, which redirects to the IdP. We must use a full
// navigation (not fetch) because the browser needs to follow the IdP's
// redirect chain and eventually post back to /callback.
export function startOIDCAuthorize(providerKey: string, returnTo: string) {
  const base = `${window.location.origin}/api/v1`;
  const url = new URL(`${base}/auth/oidc/${encodeURIComponent(providerKey)}/authorize`);
  if (returnTo) url.searchParams.set('return_to', returnTo);
  window.location.assign(url.toString());
}

export async function validateInviteCode({ code }: { code: string }) {
  const result = await client.request<{ valid: boolean; email?: string; role: string }>(
    'POST', '/auth/invite/validate', { code }, { noAuth: true },
  );
  return result;
}

export async function registerWithInvite({
  inviteCode, email, password, displayName,
}: {
  inviteCode: string;
  email: string;
  password: string;
  displayName: string;
}) {
  await client.request<SaturnUser>(
    'POST', '/auth/register',
    { invite_code: inviteCode, email, password, display_name: displayName },
    { noAuth: true },
  );

  // After registration, auto-login
  return loginWithEmail({ email, password });
}

// Encrypted credentials for 2FA flow — plaintext never stored in module scope
let pending2FAEncrypted: { email: string; iv: Uint8Array; ciphertext: ArrayBuffer; key: CryptoKey } | undefined;
let pending2FATimeout: ReturnType<typeof setTimeout> | undefined;

async function encryptCredentials(email: string, password: string) {
  const key = await crypto.subtle.generateKey({ name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt']);
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const ciphertext = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv: iv as BufferSource },
    key,
    new TextEncoder().encode(password),
  );
  return { email, iv, ciphertext, key };
}

async function decryptCredentials(): Promise<{ email: string; password: string } | undefined> {
  if (!pending2FAEncrypted) return undefined;
  const { email, iv, ciphertext, key } = pending2FAEncrypted;
  const decrypted = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: iv as BufferSource }, key, ciphertext);
  return { email, password: new TextDecoder().decode(decrypted) };
}

export async function loginWithEmail({
  email, password, totpCode,
}: {
  email: string;
  password: string;
  totpCode?: string;
}) {
  try {
    const body: Record<string, unknown> = { email, password };
    if (totpCode) body.totp_code = totpCode;

    const result = await client.request<SaturnLoginResponse>(
      'POST', '/auth/login', body, { noAuth: true },
    );

    client.setAccessToken(result.access_token, result.expires_in);
    pending2FAEncrypted = undefined;
    if (pending2FATimeout) {
      clearTimeout(pending2FATimeout);
      pending2FATimeout = undefined;
    }

    const apiUser = buildApiUser(result.user);
    apiUser.isSelf = true;

    sendApiUpdate({
      '@type': 'updateCurrentUser',
      currentUser: apiUser,
      currentUserFullInfo: buildApiUserFullInfo(result.user),
      saturnRole: result.user.role,
    });

    sendApiUpdate({
      '@type': 'updateUserStatus',
      userId: result.user.id,
      status: buildApiUserStatus(result.user),
    });

    sendApiUpdate({
      '@type': 'updateAuthorizationState',
      authorizationState: 'authorizationStateReady',
    });

    // Connect WebSocket after auth
    client.connectWs();

    return { user: apiUser };
  } catch (e) {
    if (e instanceof client.ApiError) {
      if (e.code === '2fa_required') {
        encryptCredentials(email, password).then((encrypted) => {
          pending2FAEncrypted = encrypted;
        });
        if (pending2FATimeout) clearTimeout(pending2FATimeout);
        pending2FATimeout = setTimeout(() => {
          pending2FAEncrypted = undefined;
        }, 5 * 60 * 1000);
        sendApiUpdate({
          '@type': 'updateAuthorizationState',
          authorizationState: 'authorizationStateWaitPassword',
        });
        return undefined;
      }

      let errorKey: { key: 'FloodWait' | 'ErrorIncorrectPassword' | 'ErrorServerUnavailable' };
      if (e.status === 429) {
        errorKey = { key: 'FloodWait' };
      } else if (e.status >= 500) {
        errorKey = { key: 'ErrorServerUnavailable' };
      } else {
        errorKey = { key: 'ErrorIncorrectPassword' };
      }

      sendApiUpdate({
        '@type': 'updateAuthorizationError',
        errorKey,
      });
    } else {
      sendApiUpdate({
        '@type': 'updateAuthorizationError',
        errorKey: { key: 'ErrorServerUnavailable' as const },
      });
    }
    return undefined;
  }
}

export async function checkAuth() {
  const storedToken = client.getAccessToken();

  // Try to refresh if no token but user had a session before
  if (!storedToken) {
    if (!client.hasSessionHint()) {
      sendApiUpdate({
        '@type': 'updateAuthorizationState',
        authorizationState: 'authorizationStateWaitPhoneNumber',
      });
      return false;
    }

    try {
      const result = await client.request<SaturnLoginResponse>(
        'POST', '/auth/refresh', undefined, { noAuth: true },
      );

      client.setAccessToken(result.access_token, result.expires_in);

      const apiUser = buildApiUser(result.user);
      apiUser.isSelf = true;

      sendApiUpdate({
        '@type': 'updateCurrentUser',
        currentUser: apiUser,
        currentUserFullInfo: buildApiUserFullInfo(result.user),
        saturnRole: result.user.role,
      });

      sendApiUpdate({
        '@type': 'updateAuthorizationState',
        authorizationState: 'authorizationStateReady',
      });

      client.connectWs();
      return true;
    } catch (error) {
      if (isOfflineNetworkError(error)) {
        sendApiUpdate({
          '@type': 'updateConnectionState',
          connectionState: 'connectionStateConnecting',
        });
        return false;
      }

      sendApiUpdate({
        '@type': 'updateAuthorizationState',
        authorizationState: 'authorizationStateWaitPhoneNumber',
      });
      return false;
    }
  }

  // Verify existing token
  try {
    const user = await client.request<SaturnUser>('GET', '/auth/me', undefined, { skipAuthReady: true });
    const apiUser = buildApiUser(user);
    apiUser.isSelf = true;

    sendApiUpdate({
      '@type': 'updateCurrentUser',
      currentUser: apiUser,
      currentUserFullInfo: buildApiUserFullInfo(user),
      saturnRole: user.role,
    });

    sendApiUpdate({
      '@type': 'updateAuthorizationState',
      authorizationState: 'authorizationStateReady',
    });

    client.connectWs();
    return true;
  } catch (error) {
    if (isOfflineNetworkError(error)) {
      sendApiUpdate({
        '@type': 'updateConnectionState',
        connectionState: 'connectionStateConnecting',
      });
      return false;
    }

    // Token expired, try refresh
    try {
      const result = await client.request<SaturnLoginResponse>(
        'POST', '/auth/refresh', undefined, { noAuth: true },
      );
      client.setAccessToken(result.access_token, result.expires_in);

      // Fetch current user after token refresh
      const user = await client.request<SaturnUser>('GET', '/auth/me', undefined, { skipAuthReady: true });
      const apiUser = buildApiUser(user);
      apiUser.isSelf = true;

      sendApiUpdate({
        '@type': 'updateCurrentUser',
        currentUser: apiUser,
        currentUserFullInfo: buildApiUserFullInfo(user),
        saturnRole: user.role,
      });

      sendApiUpdate({
        '@type': 'updateUserStatus',
        userId: user.id,
        status: buildApiUserStatus(user),
      });

      sendApiUpdate({
        '@type': 'updateAuthorizationState',
        authorizationState: 'authorizationStateReady',
      });

      client.connectWs();
      return true;
    } catch (refreshError) {
      if (isOfflineNetworkError(refreshError)) {
        sendApiUpdate({
          '@type': 'updateConnectionState',
          connectionState: 'connectionStateConnecting',
        });
        return false;
      }

      client.clearAuth();
      sendApiUpdate({
        '@type': 'updateAuthorizationState',
        authorizationState: 'authorizationStateWaitPhoneNumber',
      });
      return false;
    }
  }
}

export async function createAuthInvite({ role, maxUses }: { role: string; maxUses: number }) {
  return client.request<{ id: string; code: string }>('POST', '/auth/invites', {
    role,
    max_uses: maxUses,
  });
}

export async function logout() {
  try {
    await client.request('POST', '/auth/logout');
  } catch {
    // Ignore logout errors
  }

  client.disconnectWs();
  client.clearAuth();

  // Clear cached privacy data to prevent leaking between sessions
  const { clearPrivacyCache } = await import('./index');
  clearPrivacyCache();

  sendApiUpdate({
    '@type': 'updateAuthorizationState',
    authorizationState: 'authorizationStateLoggingOut',
  });
}

// Compat wrapper for TG Web A's phone auth — redirects to email flow
export function provideAuthPhoneNumber() {
  // No-op: Saturn uses email auth, not phone
  sendApiUpdate({
    '@type': 'updateAuthorizationState',
    authorizationState: 'authorizationStateWaitPhoneNumber',
  });
}

export function restartAuth() {
  pending2FAEncrypted = undefined;
  client.disconnectWs();
  client.clearAuth();
  sendApiUpdate({
    '@type': 'updateAuthorizationState',
    authorizationState: 'authorizationStateWaitPhoneNumber',
  });
}

// Compat stubs for TG Web A auth methods that Saturn doesn't use
export function provideAuthCode() {
  // No-op: Saturn uses JWT, no SMS code step
}

export async function provideAuthPassword(totpCode: string) {
  const creds = await decryptCredentials();
  if (!creds) {
    sendApiUpdate({
      '@type': 'updateAuthorizationError',
      errorKey: { key: 'ErrorIncorrectPassword' as const },
    });
    return;
  }

  await loginWithEmail({
    email: creds.email,
    password: creds.password,
    totpCode,
  });
}

export function provideAuthRegistration() {
  // No-op: Saturn uses registerWithInvite
}

export function restartAuthWithQr() {
  restartAuth();
}

export function restartAuthWithPasskey() {
  restartAuth();
}
