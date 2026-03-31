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

// Stored credentials for 2FA flow (cleared after successful login or auth restart)
let pending2FACredentials: { email: string; password: string } | undefined;

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
    pending2FACredentials = undefined; // Clear on successful login

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
        pending2FACredentials = { email, password };
        sendApiUpdate({
          '@type': 'updateAuthorizationState',
          authorizationState: 'authorizationStateWaitPassword',
        });
        return undefined;
      }

      const errorKey = e.status === 429
        ? { key: 'FloodWait' as const }
        : { key: 'ErrorIncorrectPassword' as const };

      sendApiUpdate({
        '@type': 'updateAuthorizationError',
        errorKey,
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
        saturnRole: result.user.role,
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
      saturnRole: user.role,
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

      // Fetch current user after token refresh
      const user = await client.request<SaturnUser>('GET', '/auth/me');
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
  pending2FACredentials = undefined;
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
  if (!pending2FACredentials) {
    sendApiUpdate({
      '@type': 'updateAuthorizationError',
      errorKey: { key: 'ErrorIncorrectPassword' as const },
    });
    return;
  }

  await loginWithEmail({
    email: pending2FACredentials.email,
    password: pending2FACredentials.password,
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
