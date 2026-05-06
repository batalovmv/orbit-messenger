import {
  memo, useEffect, useRef, useState,
} from '../../lib/teact/teact';
import { getActions, withGlobal } from '../../global';

import type { GlobalState } from '../../global/types';

import { callApi } from '../../api/saturn';
import { preloadImage } from '../../util/files';
import preloadFonts from '../../util/fonts';

import useFlag from '../../hooks/useFlag';
import useLang from '../../hooks/useLang';
import useLastCallback from '../../hooks/useLastCallback';

import Button from '../ui/Button';
import InputText from '../ui/InputText';

import monkeyPath from '../../assets/monkey.svg';

type OIDCConfig = {
  enabled: boolean;
  providerKey: string;
  displayName: string;
};

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
  const hasClearedInitialErrorRef = useRef(false);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [isLoading, markIsLoading, unmarkIsLoading] = useFlag(false);
  const [oidcConfig, setOidcConfig] = useState<OIDCConfig | undefined>(undefined);

  const isEmailValid = email.includes('@') && email.length > 3;
  const canSubmit = isEmailValid && password.length >= 8 && !isLoading;
  const { errorKey } = auth;

  useEffect(() => {
    if (!isPreloadInitiated) {
      isPreloadInitiated = true;
      preloadFonts();
      void preloadImage(monkeyPath);
    }
    if (hasClearedInitialErrorRef.current) return;
    hasClearedInitialErrorRef.current = true;

    // Clear stale errors on mount (e.g. from cache or Chrome credential auto-submit)
    if (errorKey) clearAuthErrorKey();
  }, [clearAuthErrorKey, errorKey]);

  useEffect(() => {
    if (errorKey) {
      unmarkIsLoading();
    }
  }, [errorKey, unmarkIsLoading]);

  // Probe whether SSO is configured. The endpoint always answers 200 with
  // a stable shape; on any failure we leave oidcConfig undefined and fall
  // back to the standard email-only form.
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const cfg = await callApi('fetchOIDCConfig');
      if (!cancelled && cfg) setOidcConfig(cfg as OIDCConfig);
    })();
    return () => { cancelled = true; };
  }, []);

  const handleOIDCSignIn = useLastCallback(() => {
    if (!oidcConfig?.enabled) return;
    const returnTo = window.location.pathname + window.location.search + window.location.hash;
    void callApi('startOIDCAuthorize', oidcConfig.providerKey, returnTo);
  });

  const handleEmailChange = useLastCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    if (errorKey) clearAuthErrorKey();
    setEmail(e.target.value);
  });

  const handlePasswordChange = useLastCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    if (errorKey) clearAuthErrorKey();
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
        {oidcConfig?.enabled && (
          <div className="oidc-sso-block">
            <Button
              ripple
              isText
              className="oidc-sign-in-button"
              onClick={handleOIDCSignIn}
            >
              {lang('OIDCSignInButton', { provider: oidcConfig.displayName })}
            </Button>
            <div className="oidc-divider"><span>{lang('OIDCDivider')}</span></div>
          </div>
        )}
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
          {lang('RegistrationJoinWith')}
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
