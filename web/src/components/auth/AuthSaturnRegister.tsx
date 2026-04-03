import { memo, useState } from '../../lib/teact/teact';
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

  const [inviteCode, setInviteCode] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [isLoading, markIsLoading] = useFlag(false);

  const canSubmit = inviteCode.length > 0
    && email.length > 0
    && password.length >= 6
    && displayName.length > 0
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
    saturnRegister({ inviteCode, email, password, displayName });
  });

  return (
    <div id="auth-registration-form" className="custom-scroll">
      <div className="auth-form">
        <div id="logo" />
        <h1>Orbit Messenger</h1>
        <p className="note">{lang('RegistrationJoinWith' as any) || 'Register with invite code'}</p>
        <form action="" onSubmit={handleSubmit}>
          <InputText
            id="register-invite-code"
            label={lang('InviteCode' as any) || 'Invite Code'}
            value={inviteCode}
            error={auth.errorKey ? lang.withRegular(auth.errorKey) : undefined}
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
            label={lang('LoginPassword' as any) || 'Password'}
            type="password"
            value={password}
            autoComplete="new-password"
            onChange={handlePasswordChange}
          />
          <InputText
            id="register-display-name"
            label={lang('LoginRegisterFirstNamePlaceholder') || 'Display Name'}
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
            {lang('RegistrationSignUp' as any) || 'Sign Up'}
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
