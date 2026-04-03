import {
  memo, useEffect, useRef, useState,
} from '../../lib/teact/teact';
import { getActions, withGlobal } from '../../global';

import type { GlobalState } from '../../global/types';

import { preloadImage } from '../../util/files';
import preloadFonts from '../../util/fonts';

import useFlag from '../../hooks/useFlag';
import useLang from '../../hooks/useLang';
import useLastCallback from '../../hooks/useLastCallback';

import Button from '../ui/Button';
import InputText from '../ui/InputText';

import monkeyPath from '../../assets/monkey.svg';

type StateProps = {
  auth: GlobalState['auth'];
  connectionState: GlobalState['connectionState'];
};

let isPreloadInitiated = false;

const AuthEmailLogin = ({
  auth,
  connectionState,
}: StateProps) => {
  const {
    clearAuthErrorKey,
    saturnLoginWithEmail,
    saturnGoToInvite,
  } = getActions();

  const lang = useLang();

  const emailRef = useRef<HTMLInputElement>();
  const passwordRef = useRef<HTMLInputElement>();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [isLoading, markIsLoading, unmarkIsLoading] = useFlag(false);

  const canSubmit = email.length > 3 && password.length >= 8 && !isLoading;
  useEffect(() => {
    if (!isPreloadInitiated) {
      isPreloadInitiated = true;
      preloadFonts();
      void preloadImage(monkeyPath);
    }
  }, []);

  useEffect(() => {
    if (auth.errorKey) {
      unmarkIsLoading();
    }
  }, [auth.errorKey]);

  const handleEmailChange = useLastCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    if (auth.errorKey) clearAuthErrorKey();
    setEmail(e.target.value);
  });

  const handlePasswordChange = useLastCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    if (auth.errorKey) clearAuthErrorKey();
    setPassword(e.target.value);
  });

  const handleSubmit = useLastCallback((e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    markIsLoading();
    saturnLoginWithEmail({ email, password });
  });

  const handleGoToInvite = useLastCallback(() => {
    saturnGoToInvite();
  });

  return (
    <div id="auth-phone-number-form" className="custom-scroll">
      <div className="auth-form">
        <div id="logo" />
        <h1>Orbit Messenger</h1>
        <p className="note">{lang('LoginSubtitle')}</p>
        <form action="" onSubmit={handleSubmit}>
          <InputText
            ref={emailRef}
            id="sign-in-email"
            label={lang('LoginEmail')}
            value={email}
            inputMode="email"
            error={auth.errorKey ? lang.withRegular(auth.errorKey) : undefined}
            autoComplete="email"
            onChange={handleEmailChange}
          />
          <InputText
            ref={passwordRef}
            id="sign-in-password"
            label={lang('LoginPassword')}
            type="password"
            value={password}
            autoComplete="current-password"
            onChange={handlePasswordChange}
          />
          <Button
            type="submit"
            ripple
            isLoading={isLoading}
            disabled={!canSubmit}
          >
            {lang('LoginNext')}
          </Button>
        </form>
        <Button isText className="auth-register-link" onClick={handleGoToInvite}>
          {lang('RegistrationJoinWith' as any)}
        </Button>
      </div>
    </div>
  );
};

export default memo(withGlobal(
  (global): StateProps => {
    return {
      auth: global.auth,
      connectionState: global.connectionState,
    };
  },
)(AuthEmailLogin));
