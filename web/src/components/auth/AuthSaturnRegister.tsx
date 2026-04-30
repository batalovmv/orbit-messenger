import { memo, useEffect, useRef, useState } from '../../lib/teact/teact';
import { getActions, withGlobal } from '../../global';

import type { GlobalState } from '../../global/types';

import { pick } from '../../util/iteratees';

import useFlag from '../../hooks/useFlag';
import useLang from '../../hooks/useLang';
import useLastCallback from '../../hooks/useLastCallback';

import Button from '../ui/Button';
import InputText from '../ui/InputText';

type StateProps = {
  auth: GlobalState['auth'];
};

const AuthSaturnRegister = ({ auth }: StateProps) => {
  const { saturnRegister, clearAuthErrorKey } = getActions();
  const lang = useLang();
  const { errorKey } = auth;
  const hasClearedInitialErrorRef = useRef(false);

  const [inviteCode, setInviteCode] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [isLoading, markIsLoading, unmarkIsLoading] = useFlag(false);

  useEffect(() => {
    if (hasClearedInitialErrorRef.current) return;
    hasClearedInitialErrorRef.current = true;

    // Clear stale errors on mount
    if (errorKey) clearAuthErrorKey();
  }, [clearAuthErrorKey, errorKey]);

  useEffect(() => {
    if (errorKey) {
      unmarkIsLoading();
    }
  }, [errorKey, unmarkIsLoading]);

  const trimmedInviteCode = inviteCode.trim();
  const trimmedEmail = email.trim();
  const trimmedDisplayName = displayName.trim();

  const canSubmit = trimmedInviteCode.length > 0
    && trimmedEmail.length > 0
    && password.length >= 6
    && trimmedDisplayName.length > 0
    && !isLoading;

  const handleChange = useLastCallback(() => {
    if (auth.errorKey) {
      clearAuthErrorKey();
    }
  });

  const handleInviteChange = useLastCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    handleChange();
    setInviteCode(e.target.value);
  });

  const handleEmailChange = useLastCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    handleChange();
    setEmail(e.target.value);
  });

  const handlePasswordChange = useLastCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    handleChange();
    setPassword(e.target.value);
  });

  const handleDisplayNameChange = useLastCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    handleChange();
    setDisplayName(e.target.value);
  });

  const handleSubmit = useLastCallback((e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    markIsLoading();
    saturnRegister({
      inviteCode: trimmedInviteCode,
      email: trimmedEmail,
      password,
      displayName: trimmedDisplayName,
    });
    unmarkIsLoading();
  });

  return (
    <div id="auth-registration-form" className="custom-scroll">
      <div className="auth-form">
        <div id="logo" />
        <h1>Orbit Messenger</h1>
        <p className="note">{lang('RegistrationJoinWith') || 'Register with invite code'}</p>
        <p className="note">{lang('RegistrationInviteHelp')}</p>
        <form action="" onSubmit={handleSubmit}>
          <InputText
            id="register-invite-code"
            label={lang('InviteCode')}
            value={inviteCode}
            error={errorKey ? lang.withRegular(errorKey) : undefined}
            autoComplete="off"
            onChange={handleInviteChange}
          />
          <InputText
            id="register-email"
            label="Email"
            value={email}
            inputMode="email"
            autoComplete="email"
            onChange={handleEmailChange}
          />
          <InputText
            id="register-password"
            label={lang('LoginPassword')}
            type="password"
            value={password}
            autoComplete="new-password"
            onChange={handlePasswordChange}
          />
          <InputText
            id="register-display-name"
            label={lang('DisplayName')}
            value={displayName}
            autoComplete="name"
            onChange={handleDisplayNameChange}
          />
          <Button
            type="submit"
            ripple
            isLoading={isLoading}
            disabled={!canSubmit}
          >
            {lang('RegistrationSignUp')}
          </Button>
        </form>
      </div>
    </div>
  );
};

export default memo(withGlobal(
  (global): Complete<StateProps> => (
    pick(global, ['auth'])
  ),
)(AuthSaturnRegister));
