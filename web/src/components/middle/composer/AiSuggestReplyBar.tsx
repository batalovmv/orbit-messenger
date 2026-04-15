import { memo, useEffect, useState } from '../../../lib/teact/teact';

import { suggestReply } from '../../../api/saturn/methods/ai';
import buildClassName from '../../../util/buildClassName';

import useFlag from '../../../hooks/useFlag';
import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import Icon from '../../common/icons/Icon';
import Spinner from '../../ui/Spinner';

import styles from './AiSuggestReplyBar.module.scss';

type OwnProps = {
  chatId: string;
  hasText: boolean;
  onSuggestionClick: (text: string) => void;
};

const AiSuggestReplyBar = ({ chatId, hasText, onSuggestionClick }: OwnProps) => {
  const lang = useLang();

  const [suggestions, setSuggestions] = useState<string[] | undefined>();
  const [error, setError] = useState<string | undefined>();
  const [isLoading, startLoading, stopLoading] = useFlag(false);

  // Hide suggestions when the user starts typing — don't overwrite input.
  useEffect(() => {
    if (hasText && suggestions) {
      setSuggestions(undefined);
    }
  }, [hasText, suggestions]);

  // Reset when switching chats.
  useEffect(() => {
    setSuggestions(undefined);
    setError(undefined);
    stopLoading();
  }, [chatId, stopLoading]);

  const handleFetch = useLastCallback(async () => {
    setError(undefined);
    startLoading();
    try {
      const result = await suggestReply({ chatId });
      if (!result || result.length === 0) {
        setError(lang('AiNotConfigured'));
        setSuggestions(undefined);
      } else {
        setSuggestions(result);
      }
    } catch (err) {
      const e = err as Error & { status?: number };
      if (e?.status === 503) {
        setError(lang('AiNotConfigured'));
      } else {
        setError(e?.message || lang('AiSuggestFailed'));
      }
    } finally {
      stopLoading();
    }
  });

  const handlePick = useLastCallback((text: string) => {
    onSuggestionClick(text);
    setSuggestions(undefined);
  });

  if (hasText) return undefined;

  if (isLoading) {
    return (
      <div className={styles.root}>
        <Spinner color="gray" />
        <span className={styles.label}>{lang('AiSuggestLoading')}</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className={buildClassName(styles.root, styles.error)}>
        <Icon name="warning" />
        <span className={styles.label}>{error}</span>
      </div>
    );
  }

  if (suggestions && suggestions.length > 0) {
    return (
      <div className={styles.root}>
        {suggestions.map((text) => (
          <button
            key={text}
            type="button"
            className={styles.chip}
            onClick={() => handlePick(text)}
            title={text}
          >
            {text}
          </button>
        ))}
      </div>
    );
  }

  return (
    <button type="button" className={buildClassName(styles.root, styles.trigger)} onClick={handleFetch}>
      <Icon name="lamp" />
      <span className={styles.label}>{lang('AiSuggestReply')}</span>
    </button>
  );
};

export default memo(AiSuggestReplyBar);
