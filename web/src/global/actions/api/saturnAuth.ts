import type { ActionReturnType } from '../../types';

import { callApi } from '../../../api/saturn';
import { addActionHandler, getGlobal, setGlobal } from '../../index';
import { updateAuth } from '../../reducers/auth';

addActionHandler('saturnLoginWithEmail', async (global, actions, payload): Promise<void> => {
  const { email, password, totpCode } = payload;

  global = updateAuth(global, {
    isLoading: true,
    errorKey: undefined,
  });
  setGlobal(global);

  await callApi('loginWithEmail', { email, password, totpCode });
});

addActionHandler('saturnGoToInvite', (global): ActionReturnType => {
  return updateAuth(global, {
    state: 'authorizationStateWaitRegistration',
    errorKey: undefined,
  });
});

addActionHandler('saturnValidateInvite', async (global, actions, payload): Promise<void> => {
  const { code } = payload;

  global = updateAuth(global, {
    isLoading: true,
    errorKey: undefined,
  });
  setGlobal(global);

  const result = await callApi('validateInviteCode', { code });

  global = getGlobal();
  if (!result) {
    global = updateAuth(global, { isLoading: false });
    setGlobal(global);
    return;
  }

  global = updateAuth(global, { isLoading: false });
  setGlobal(global);
});

addActionHandler('saturnRegister', async (global, actions, payload): Promise<void> => {
  const { inviteCode, email, password, displayName } = payload;

  global = updateAuth(global, {
    isLoading: true,
    errorKey: undefined,
  });
  setGlobal(global);

  await callApi('registerWithInvite', {
    inviteCode, email, password, displayName,
  });
});
