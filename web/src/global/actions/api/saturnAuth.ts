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

  const result = await callApi('loginWithEmail', { email, password, totpCode });

  if (result) {
    // Wait a tick for WS to connect and dispatch connectionStateReady
    await new Promise((resolve) => setTimeout(resolve, 500));

    global = getGlobal();
    if (global.connectionState === 'connectionStateReady' && global.auth.state === 'authorizationStateReady') {
      actions.sync();
    }
  }
});

addActionHandler('saturnGoToInvite', (global): ActionReturnType => {
  return updateAuth(global, {
    state: 'authorizationStateWaitRegistration',
    errorKey: undefined,
  });
});

addActionHandler('saturnGoToLogin', (global): ActionReturnType => {
  return updateAuth(global, {
    state: 'authorizationStateWaitPhoneNumber',
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

  let result;
  try {
    result = await callApi('registerWithInvite', {
      inviteCode, email, password, displayName,
    });
  } catch {
    global = getGlobal();
    global = updateAuth(global, {
      isLoading: false,
      errorKey: { key: 'ErrorServerUnavailable' },
    });
    setGlobal(global);
    return;
  }

  if (result) {
    await new Promise((resolve) => setTimeout(resolve, 500));

    global = getGlobal();
    if (global.connectionState === 'connectionStateReady' && global.auth.state === 'authorizationStateReady') {
      actions.sync();
    }
  } else {
    global = getGlobal();
    global = updateAuth(global, { isLoading: false });
    setGlobal(global);
  }
});
