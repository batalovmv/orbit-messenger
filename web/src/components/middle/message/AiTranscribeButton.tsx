import { memo, useEffect, useState } from '../../../lib/teact/teact';

import buildClassName from '../../../util/buildClassName';
import { fetchAiCapabilities, transcribeVoice } from '../../../api/saturn/methods/ai';

import useFlag from '../../../hooks/useFlag';
import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import Icon from '../../common/icons/Icon';
import Spinner from '../../ui/Spinner';

import styles from './AiTranscribeButton.module.scss';

type OwnProps = {
  mediaId: string;
};

type CacheEntry = { text: string; language?: string };

// Session-scoped cache — survives component unmount/remount within a session.
// Prevents duplicate API calls when a voice message re-renders (e.g. scroll).
const transcriptionCache = new Map<string, CacheEntry>();

// Module-level capability cache. We probe /ai/capabilities once per session
// so every voice message can hide the Transcribe button when Whisper is not
// configured on this deployment (OPENAI_API_KEY missing on Saturn). Default
// `true` keeps the button visible while the probe is in-flight or if it
// fails — better to show a button that 503s than to hide a working one.
let isWhisperAvailable = true;
let capabilitiesProbe: Promise<void> | undefined;
const capabilitySubscribers = new Set<(available: boolean) => void>();

function ensureCapabilitiesProbe() {
  if (capabilitiesProbe) return capabilitiesProbe;
  capabilitiesProbe = fetchAiCapabilities()
    .then((caps) => {
      if (caps && caps.whisper_configured === false) {
        isWhisperAvailable = false;
        capabilitySubscribers.forEach((notify) => notify(false));
      }
    })
    .catch(() => {
      // Probe failure: leave the button visible. Click will still surface
      // the 503 banner via the existing error path.
    });
  return capabilitiesProbe;
}

const COLLAPSE_THRESHOLD = 500;

const AiTranscribeButton = ({ mediaId }: OwnProps) => {
  const lang = useLang();

  const [cached, setCached] = useState<CacheEntry | undefined>(() => transcriptionCache.get(mediaId));
  const [error, setError] = useState<string | undefined>();
  const [isLoading, startLoading, stopLoading] = useFlag(false);
  const [isExpanded, expand, collapse] = useFlag(true);
  const [isWhisperHidden, setIsWhisperHidden] = useState(!isWhisperAvailable);

  useEffect(() => {
    if (!isWhisperAvailable) return undefined;
    ensureCapabilitiesProbe();
    const onChange = (available: boolean) => setIsWhisperHidden(!available);
    capabilitySubscribers.add(onChange);
    return () => {
      capabilitySubscribers.delete(onChange);
    };
  }, []);

  const handleTranscribe = useLastCallback(async () => {
    setError(undefined);
    startLoading();

    try {
      const result = await transcribeVoice({ mediaId });
      if (!result?.text) {
        setError(lang('AiNotConfigured'));
        return;
      }
      const entry: CacheEntry = { text: result.text, language: result.language };
      transcriptionCache.set(mediaId, entry);
      setCached(entry);
      expand();
    } catch (err) {
      const e = err as Error & { status?: number };
      if (e?.status === 503) {
        setError(lang('AiNotConfigured'));
      } else {
        setError(e?.message || lang('AiTranscribeFailed'));
      }
    } finally {
      stopLoading();
    }
  });

  const handleToggle = useLastCallback(() => {
    if (isExpanded) {
      collapse();
    } else {
      expand();
    }
  });

  if (isWhisperHidden && !cached) {
    return undefined;
  }

  if (isLoading) {
    return (
      <div className={styles.root}>
        <Spinner color="gray" />
        <span className={styles.statusText}>{lang('AiTranscribing')}</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className={buildClassName(styles.root, styles.error)}>
        <Icon name="warning" />
        <span className={styles.statusText}>{error}</span>
        <button type="button" className={styles.retryButton} onClick={handleTranscribe}>
          {lang('AiTranscribeRetry')}
        </button>
      </div>
    );
  }

  if (cached) {
    const isLong = cached.text.length > COLLAPSE_THRESHOLD;
    const displayText = isExpanded || !isLong ? cached.text : `${cached.text.slice(0, COLLAPSE_THRESHOLD)}…`;
    return (
      <div className={buildClassName(styles.root, styles.done)}>
        <p className={styles.transcriptionText} dir="auto">{displayText}</p>
        {isLong && (
          <button type="button" className={styles.toggleButton} onClick={handleToggle}>
            {isExpanded ? lang('AiTranscribeCollapse') : lang('AiTranscribeExpand')}
          </button>
        )}
      </div>
    );
  }

  return (
    <button
      type="button"
      className={buildClassName(styles.root, styles.triggerButton)}
      onClick={handleTranscribe}
    >
      <Icon name="transcribe" />
      <span>{lang('AiTranscribeButton')}</span>
    </button>
  );
};

export default memo(AiTranscribeButton);
