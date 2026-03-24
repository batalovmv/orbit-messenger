import type { SaturnLoginResponse, SaturnUser } from '../types';

import { buildApiUser, buildApiUserFullInfo, buildApiUserStatus } from '../apiBuilders/users';
import * as client from '../client';
import { sendApiUpdate } from '../updates/apiUpdateEmitter';

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
  const user = await client.request<SaturnUser>(
    'POST', '/auth/register',
    { invite_code: inviteCode, email, password, display_name: displayName },
    { noAuth: true },
  );

  // After registration, auto-login
  return loginWithEmail({ email, password });
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

    const apiUser = buildApiUser(result.user);
    apiUser.isSelf = true;

    sendApiUpdate({
      '@type': 'updateCurrentUser',
      currentUser: apiUser,
      currentUserFullInfo: buildApiUserFullInfo(result.user),
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
        // Need 2FA code
        sendApiUpdate({
          '@type': 'updateAuthorizationState',
          authorizationState: 'authorizationStateWaitPassword',
        });
        return undefined;
      }

      sendApiUpdate({
        '@type': 'updateAuthorizationError',
        errorKey: { key: 'ErrorIncorrectPassword' },
      });
    }
    return undefined;
  }
}

export async function checkAuth() {
  const storedToken = client.getAccessToken();

  // Try to refresh if no token
  if (!storedToken) {
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
      });

      sendApiUpdate({
        '@type': 'updateAuthorizationState',
        authorizationState: 'authorizationStateReady',
      });

      client.connectWs();
      return true;
    } catch {
      sendApiUpdate({
        '@type': 'updateAuthorizationState',
        authorizationState: 'authorizationStateWaitPhoneNumber',
      });
      return false;
    }
  }

  // Verify existing token
  try {
    const user = await client.request<SaturnUser>('GET', '/auth/me');
    const apiUser = buildApiUser(user);
    apiUser.isSelf = true;

    sendApiUpdate({
      '@type': 'updateCurrentUser',
      currentUser: apiUser,
      currentUserFullInfo: buildApiUserFullInfo(user),
    });

    sendApiUpdate({
      '@type': 'updateAuthorizationState',
      authorizationState: 'authorizationStateReady',
    });

    client.connectWs();
    return true;
  } catch {
    // Token expired, try refresh
    try {
      const result = await client.request<SaturnLoginResponse>(
        'POST', '/auth/refresh', undefined, { noAuth: true },
      );
      client.setAccessToken(result.access_token, result.expires_in);

      sendApiUpdate({
        '@type': 'updateAuthorizationState',
        authorizationState: 'authorizationStateReady',
      });

      client.connectWs();
      return true;
    } catch {
      client.clearAuth();
      sendApiUpdate({
        '@type': 'updateAuthorizationState',
        authorizationState: 'authorizationStateWaitPhoneNumber',
      });
      return false;
    }
  }
}

export async function logout() {
  try {
    await client.request('POST', '/auth/logout');
  } catch {
    // Ignore logout errors
  }

  client.disconnectWs();
  client.clearAuth();

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

export function provideAuthPassword(_password: string) {
  // Used for 2FA — Saturn handles this through loginWithEmail with totpCode
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
