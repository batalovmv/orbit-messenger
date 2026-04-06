import { request } from '../client';
import { sendApiUpdate } from '../updates/apiUpdateEmitter';

type TwoFaStatusResponse = {
  totp_enabled?: boolean;
};

type PasswordInfo = {
  hint?: string;
  hasPassword: boolean;
  hasRecoveryEmail?: boolean;
  pendingResetDate?: number;
};

function sendTwoFaError(error: unknown) {
  if (error instanceof Error && error.message) {
    sendApiUpdate({
      '@type': 'updateTwoFaError',
      messageKey: {
        key: 'ErrorUnexpectedMessage',
        variables: { error: error.message },
      },
    });

    return;
  }

  sendApiUpdate({
    '@type': 'updateTwoFaError',
    messageKey: { key: 'ErrorUnexpected' },
  });
}

async function getTwoFaStatus() {
  const result = await request<TwoFaStatusResponse>('GET', '/auth/me');
  return Boolean(result?.totp_enabled);
}

export async function getPasswordInfo(): Promise<PasswordInfo> {
  try {
    return {
      hasPassword: await getTwoFaStatus(),
      hint: undefined,
      hasRecoveryEmail: false,
      pendingResetDate: undefined,
    };
  } catch {
    return { hasPassword: false };
  }
}

export async function checkPassword(currentPassword: string) {
  try {
    if (await getTwoFaStatus()) {
      return true;
    }

    await request('POST', '/auth/2fa/verify', { code: currentPassword });

    return true;
  } catch (error) {
    sendTwoFaError(error);

    return false;
  }
}

export async function updatePassword(currentPassword: string | undefined, password: string) {
  try {
    await request('POST', '/auth/2fa/setup');
    await request('POST', '/auth/2fa/verify', { code: currentPassword || password });

    return true;
  } catch (error) {
    sendTwoFaError(error);

    return false;
  }
}

export async function clearPassword(currentPassword: string) {
  try {
    await request('POST', '/auth/2fa/disable', { password: currentPassword });

    return true;
  } catch (error) {
    sendTwoFaError(error);

    return false;
  }
}
