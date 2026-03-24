import type { ActionReturnType } from '../../types';

import { callApi } from '../../../api/saturn';
import { addActionHandler } from '../../index';
import { updateAuth } from '../../reducers/auth';

addActionHandler('saturnLoginWithEmail', async (global, actions, payload): Promise<void> => {
  const { email, password, totpCode } = payload;

  global = updateAuth(global, {
    isLoading: true,
    errorKey: undefined,
  });

  void callApi('loginWithEmail', { email, password, totpCode });
});

addActionHandler('saturnGoToInvite', (global): ActionReturnType => {
  return updateAuth(global, {
    state: 'authorizationStateWaitRegistration',
  });
});

addActionHandler('saturnValidateInvite', async (global, actions, payload): Promise<void> => {
  const { code } = payload;
  const result = await callApi('validateInviteCode', { code });
  if (!result) return;

  // Store invite code validation result — will be used by register
});

addActionHandler('saturnRegister', async (global, actions, payload): Promise<void> => {
  const { inviteCode, email, password, displayName } = payload;

  global = updateAuth(global, {
    isLoading: true,
    errorKey: undefined,
  });

  void callApi('registerWithInvite', {
    inviteCode, email, password, displayName,
  });
});
