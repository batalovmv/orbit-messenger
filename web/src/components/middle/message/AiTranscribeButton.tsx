import { memo, useState } from '../../../lib/teact/teact';

import { transcribeVoice } from '../../../api/saturn/methods/ai';
import buildClassName from '../../../util/buildClassName';

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

const COLLAPSE_THRESHOLD = 500;

const AiTranscribeButton = ({ mediaId }: OwnProps) => {
  const lang = useLang();

  const [cached, setCached] = useState<CacheEntry | undefined>(() => transcriptionCache.get(mediaId));
  const [error, setError] = useState<string | undefined>();
  const [isLoading, startLoading, stopLoading] = useFlag(false);
  const [isExpanded, expand, collapse] = useFlag(true);

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
