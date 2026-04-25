import {
  memo, useEffect, useRef, useState,
} from '../../../lib/teact/teact';
import { getActions } from '../../../global';

import type { SaturnBot, SaturnBotCreateResponse } from '../../../api/saturn/types';

import buildClassName from '../../../util/buildClassName';
import { copyTextToClipboard } from '../../../util/clipboard';
import { ApiError } from '../../../api/saturn/client';
import { checkBotUsername, createBot } from '../../../api/saturn/methods/bots';

import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import Button from '../../ui/Button';
import InputText from '../../ui/InputText';
import Modal from '../../ui/Modal';
import Spinner from '../../ui/Spinner';

import styles from './CreateBotWizard.module.scss';

type BotWithToken = SaturnBot & { token: string };

type OwnProps = {
  isOpen: boolean;
  onClose: NoneToVoidFunction;
  onCreated: (bot: SaturnBot) => void;
  onInstallRequest?: (bot: BotWithToken) => void;
};

const TOTAL_STEPS = 4;
const DISPLAY_NAME_MIN = 3;
const DISPLAY_NAME_MAX = 64;
const USERNAME_MIN = 3;
const USERNAME_REGEX = /^[a-zA-Z][a-zA-Z0-9_]{2,30}$/;
const DESC_MAX = 256;
const USERNAME_DEBOUNCE_MS = 500;

type UsernameStatus =
  | 'idle'
  | 'too_short'
  | 'invalid_format'
  | 'checking'
  | 'available'
  | 'taken';

const CreateBotWizard = ({
  isOpen, onClose, onCreated, onInstallRequest,
}: OwnProps) => {
  const { showNotification, openChat } = getActions();
  const lang = useLang();

  const [step, setStep] = useState<1 | 2 | 3 | 4>(1);
  const [displayName, setDisplayName] = useState('');
  const [username, setUsername] = useState('');
  const [description, setDescription] = useState('');
  const [usernameStatus, setUsernameStatus] = useState<UsernameStatus>('idle');
  const [isCreating, setIsCreating] = useState(false);
  const [createError, setCreateError] = useState<string | undefined>();
  const [createdBot, setCreatedBot] = useState<BotWithToken | undefined>();
  const [isTokenRevealed, setIsTokenRevealed] = useState(false);

  const usernameRef = useRef<string>(username);
  usernameRef.current = username;

  const resetState = useLastCallback(() => {
    setStep(1);
    setDisplayName('');
    setUsername('');
    setDescription('');
    setUsernameStatus('idle');
    setIsCreating(false);
    setCreateError(undefined);
    setCreatedBot(undefined);
    setIsTokenRevealed(false);
  });

  useEffect(() => {
    if (!isOpen) {
      resetState();
    }
  }, [isOpen, resetState]);

  useEffect(() => {
    const trimmed = username.trim();
    if (!trimmed) {
      setUsernameStatus('idle');
      return undefined;
    }
    if (trimmed.length < USERNAME_MIN) {
      setUsernameStatus('too_short');
      return undefined;
    }
    if (!USERNAME_REGEX.test(trimmed)) {
      setUsernameStatus('invalid_format');
      return undefined;
    }

    setUsernameStatus('checking');
    const controller = new AbortController();
    const timer = window.setTimeout(async () => {
      try {
        const res = await checkBotUsername(trimmed, controller.signal);
        if (controller.signal.aborted) return;
        if (!res || !res.valid) {
          setUsernameStatus('invalid_format');
          return;
        }
        setUsernameStatus(res.available ? 'available' : 'taken');
      } catch (e) {
        if (controller.signal.aborted) return;
        setUsernameStatus('idle');
      }
    }, USERNAME_DEBOUNCE_MS);

    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [username]);

  const handleCreate = useLastCallback(async () => {
    if (isCreating) return;
    setIsCreating(true);
    setCreateError(undefined);
    try {
      const trimmedDesc = description.trim();
      const res = await createBot({
        username: username.trim(),
        display_name: displayName.trim(),
        description: trimmedDesc || undefined,
      }) as SaturnBotCreateResponse | undefined;
      if (!res?.bot || !res.token) {
        setCreateError(lang('BotWizardCreateFailed'));
        setIsCreating(false);
        return;
      }
      setCreatedBot({ ...res.bot, token: res.token });
      setStep(4);
      onCreated(res.bot);
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : (e instanceof Error ? e.message : String(e));
      setCreateError(msg || lang('BotWizardCreateFailed'));
    } finally {
      setIsCreating(false);
    }
  });

  const handleCopyToken = useLastCallback(() => {
    if (!createdBot?.token) return;
    copyTextToClipboard(createdBot.token);
    showNotification({ message: lang('BotWizardTokenCopied') });
  });

  const handleOpenInChat = useLastCallback(() => {
    if (!createdBot) return;
    openChat({ id: createdBot.user_id });
    onClose();
  });

  const handleInstallToGroup = useLastCallback(() => {
    if (!createdBot) return;
    onInstallRequest?.(createdBot);
    onClose();
  });

  const handleBack = useLastCallback(() => {
    if (step === 1) return;
    setStep((step - 1) as 1 | 2 | 3);
  });

  const handleNext = useLastCallback(() => {
    if (step === 1) {
      if (!isStep1Valid()) return;
      setStep(2);
    } else if (step === 2) {
      if (!isStep2Valid()) return;
      setStep(3);
    } else if (step === 3) {
      handleCreate();
    }
  });

  const handleSkipDescription = useLastCallback(() => {
    if (step !== 3) return;
    setDescription('');
    handleCreate();
  });

  function isStep1Valid() {
    const t = displayName.trim();
    return t.length >= DISPLAY_NAME_MIN && t.length <= DISPLAY_NAME_MAX;
  }

  function isStep2Valid() {
    return usernameStatus === 'available';
  }

  function renderProgress() {
    return (
      <div className={styles.progress}>
        {[1, 2, 3, 4].map((n) => (
          <div
            key={n}
            className={buildClassName(
              styles.dot,
              n === step && styles.dotActive,
              n < step && styles.dotDone,
            )}
          />
        ))}
      </div>
    );
  }

  function renderStep1() {
    const trimmed = displayName.trim();
    const tooShort = trimmed.length > 0 && trimmed.length < DISPLAY_NAME_MIN;
    return (
      <>
        {renderProgress()}
        <h3 className={styles.title}>{lang('BotWizardStep1Title')}</h3>
        <div className={styles.body}>
          <InputText
            label={lang('BotDisplayName')}
            value={displayName}
            maxLength={DISPLAY_NAME_MAX}
            error={tooShort ? lang('BotWizardStep1NameTooShort') : undefined}
            onChange={(e) => setDisplayName((e.target as HTMLInputElement).value)}
          />
          <p className={styles.hint}>{lang('BotWizardStep1Hint')}</p>
        </div>
      </>
    );
  }

  function renderStep2() {
    return (
      <>
        {renderProgress()}
        <h3 className={styles.title}>{lang('BotWizardStep2Title')}</h3>
        <div className={styles.body}>
          <div className={styles.usernameRow}>
            <span className={styles.usernamePrefix}>@</span>
            <InputText
              className={styles.usernameInput}
              label={lang('BotUsername')}
              value={username}
              maxLength={32}
              onChange={(e) => setUsername((e.target as HTMLInputElement).value.trim())}
            />
          </div>
          <div className={styles.statusLine}>{renderUsernameStatus()}</div>
          <p className={styles.hint}>{lang('BotWizardStep2Hint')}</p>
          <p className={styles.hint}>{lang('BotWizardStep2HintSuffix')}</p>
        </div>
      </>
    );
  }

  function renderUsernameStatus() {
    switch (usernameStatus) {
      case 'checking':
        return (
          <span className={styles.statusMuted}>
            <Spinner color="gray" />
            {' '}
            {lang('BotWizardUsernameChecking')}
          </span>
        );
      case 'available':
        return <span className={styles.statusOk}>{lang('BotWizardUsernameAvailable')}</span>;
      case 'taken':
        return <span className={styles.statusError}>{lang('BotWizardUsernameTaken')}</span>;
      case 'invalid_format':
        return <span className={styles.statusError}>{lang('BotWizardUsernameInvalid')}</span>;
      case 'too_short':
        return <span className={styles.statusMuted}>{lang('BotWizardUsernameTooShort')}</span>;
      default:
        return <span className={styles.statusMuted}>&nbsp;</span>;
    }
  }

  function renderStep3() {
    const overLimit = description.length > DESC_MAX;
    return (
      <>
        {renderProgress()}
        <h3 className={styles.title}>{lang('BotWizardStep3Title')}</h3>
        <div className={styles.body}>
          <p className={styles.subtitle}>{lang('BotWizardStep3Subtitle')}</p>
          <textarea
            className={styles.descTextarea}
            value={description}
            placeholder={lang('BotWizardDescPlaceholder')}
            maxLength={DESC_MAX + 64}
            onChange={(e) => setDescription((e.target as HTMLTextAreaElement).value)}
          />
          <span className={buildClassName(styles.charCounter, overLimit && styles.statusError)}>
            {`${description.length} / ${DESC_MAX}`}
          </span>
          {createError && <p className={styles.statusError}>{createError}</p>}
        </div>
      </>
    );
  }

  function renderStep4() {
    if (!createdBot) return undefined;
    return (
      <>
        {renderProgress()}
        <h3 className={styles.title}>{lang('BotWizardStep4Title')}</h3>
        <div className={styles.body}>
          <p className={styles.subtitle}>
            {createdBot.display_name}
            {' · @'}
            {createdBot.username}
          </p>
          <div className={styles.tokenBox}>
            <span className={styles.tokenLabel}>{lang('BotWizardStep4TokenLabel')}</span>
            <button
              type="button"
              className={buildClassName(styles.token, isTokenRevealed && styles.tokenRevealed)}
              onClick={() => setIsTokenRevealed((v) => !v)}
              onBlur={() => setIsTokenRevealed(false)}
              aria-label={lang('BotWizardStep4TokenLabel')}
            >
              {createdBot.token}
            </button>
            <div className={styles.tokenActions}>
              <Button size="smaller" color="primary" onClick={handleCopyToken}>
                {lang('BotWizardCopyToken')}
              </Button>
            </div>
          </div>
          <p className={styles.warning}>{lang('BotWizardStep4Warning')}</p>
        </div>
      </>
    );
  }

  function renderFooter() {
    if (step === 4) {
      return (
        <div className={styles.footer}>
          <Button color="translucent" onClick={handleOpenInChat}>
            {lang('BotWizardStep4OpenChat')}
          </Button>
          {onInstallRequest && (
            <Button color="translucent" onClick={handleInstallToGroup}>
              {lang('BotWizardStep4Install')}
            </Button>
          )}
          <Button color="primary" onClick={onClose}>
            {lang('BotWizardStep4Done')}
          </Button>
        </div>
      );
    }

    const canGoNext = step === 1
      ? isStep1Valid()
      : step === 2
        ? isStep2Valid()
        : description.length <= DESC_MAX;

    return (
      <div className={styles.footer}>
        {step === 3 && (
          <span className={styles.footerLeft}>
            <Button color="translucent" onClick={handleSkipDescription} disabled={isCreating}>
              {lang('BotWizardStep3Skip')}
            </Button>
          </span>
        )}
        {step > 1 && (
          <Button color="translucent" onClick={handleBack} disabled={isCreating}>
            {lang('BotWizardBack')}
          </Button>
        )}
        <Button
          color="primary"
          onClick={handleNext}
          disabled={!canGoNext || isCreating}
          isLoading={step === 3 && isCreating}
        >
          {step === 3
            ? (isCreating ? lang('BotWizardCreating') : lang('BotWizardCreate'))
            : lang('BotWizardNext')}
        </Button>
      </div>
    );
  }

  function renderStep() {
    switch (step) {
      case 1: return renderStep1();
      case 2: return renderStep2();
      case 3: return renderStep3();
      case 4: return renderStep4();
      default: return undefined;
    }
  }

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={`${lang('CreateBot')} · ${step}/${TOTAL_STEPS}`}
      hasCloseButton
      noBackdropClose={isCreating}
    >
      <div className={styles.root}>
        {renderStep()}
      </div>
      {renderFooter()}
    </Modal>
  );
};

export default memo(CreateBotWizard);
