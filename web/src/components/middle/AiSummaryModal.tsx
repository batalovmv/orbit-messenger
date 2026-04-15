import type { FC } from '../../lib/teact/teact';
import {
  memo, useCallback, useEffect, useRef, useState,
} from '../../lib/teact/teact';

import useFlag from '../../hooks/useFlag';
import useLang from '../../hooks/useLang';
import useLastCallback from '../../hooks/useLastCallback';

import Button from '../ui/Button';
import Modal from '../ui/Modal';
import Spinner from '../ui/Spinner';

import { summarizeChat } from '../../api/saturn/methods/ai';

type OwnProps = {
  isOpen: boolean;
  chatId: string;
  onClose: NoneToVoidFunction;
};

type TimeRange = '1h' | '6h' | '24h' | '7d';
type Language = 'ru' | 'en';

const TIME_RANGE_OPTIONS: Array<{ value: TimeRange; label: string }> = [
  { value: '1h', label: '1 hour' },
  { value: '6h', label: '6 hours' },
  { value: '24h', label: '24 hours' },
  { value: '7d', label: '7 days' },
];

const LANGUAGE_OPTIONS: Array<{ value: Language; label: string }> = [
  { value: 'ru', label: 'RU' },
  { value: 'en', label: 'EN' },
];

// AiSummaryModal is the first Phase 8A UI consumer. It opens from the chat
// header AI button and lets the user pick a time range + output language,
// then streams a Claude-generated summary via Server-Sent Events.
//
// Architecture: keeps the AsyncGenerator consumption loop inside a ref so
// we can abort it cleanly when the user closes the modal mid-stream. The
// streamed text is rendered into plain state — no Markdown parsing yet
// (Claude returns plain text summaries which read fine as-is).
//
// Error handling:
//   - 503 from AI service → shows "AI is not configured" banner. Typically
//     means Saturn.ac has not received real ANTHROPIC_API_KEY yet. The
//     backend returns the same error for OPENAI_API_KEY missing.
//   - Network / parse errors → shows error text, leaves modal open so user
//     can retry.
const AiSummaryModal: FC<OwnProps> = ({ isOpen, chatId, onClose }) => {
  const lang = useLang();

  const [timeRange, setTimeRange] = useState<TimeRange>('1h');
  const [language, setLanguage] = useState<Language>('ru');
  const [summary, setSummary] = useState('');
  const [error, setError] = useState<string | undefined>();
  const [isStreaming, startStreaming, stopStreaming] = useFlag(false);

  // Used to abort an in-progress stream when the modal closes or the user
  // clicks "Generate" again. We can't pass AbortSignal through the current
  // Saturn client yet, so we just stop consuming the generator — the fetch
  // will be garbage-collected when the reader is no longer referenced.
  const abortedRef = useRef(false);

  // Reset state when the modal re-opens.
  useEffect(() => {
    if (isOpen) {
      setSummary('');
      setError(undefined);
      abortedRef.current = false;
    } else {
      abortedRef.current = true;
    }
  }, [isOpen]);

  const handleGenerate = useLastCallback(async () => {
    abortedRef.current = false;
    setSummary('');
    setError(undefined);
    startStreaming();

    try {
      const generator = summarizeChat({
        chatId,
        timeRange,
        language,
      });

      let accumulated = '';
      for await (const chunk of generator) {
        if (abortedRef.current) break;
        accumulated += chunk;
        setSummary(accumulated);
      }

      if (!accumulated && !abortedRef.current) {
        // Empty stream usually means 503 ai_unavailable — the backend
        // closed the connection without any delta. Display a clear
        // "not configured" message.
        setError('AI service is not configured. Please contact your administrator.');
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      setError(message);
    } finally {
      stopStreaming();
    }
  });

  const handleClose = useCallback(() => {
    abortedRef.current = true;
    stopStreaming();
    onClose();
  }, [onClose, stopStreaming]);

  return (
    <Modal
      isOpen={isOpen}
      onClose={handleClose}
      title="AI Summary"
      hasCloseButton
      className="ai-summary-modal"
    >
      <div style="padding: 1rem; max-width: 480px">
        <div style="margin-bottom: 1rem">
          <p style="font-size: 0.8125rem; color: var(--color-text-secondary); margin-bottom: 0.5rem">
            Time range
          </p>
          <div style="display: flex; gap: 0.5rem; flex-wrap: wrap">
            {TIME_RANGE_OPTIONS.map(({ value, label }) => (
              <Button
                key={value}
                size="smaller"
                color={timeRange === value ? 'primary' : 'translucent'}
                onClick={() => setTimeRange(value)}
                disabled={isStreaming}
              >
                {label}
              </Button>
            ))}
          </div>
        </div>

        <div style="margin-bottom: 1rem">
          <p style="font-size: 0.8125rem; color: var(--color-text-secondary); margin-bottom: 0.5rem">
            Language
          </p>
          <div style="display: flex; gap: 0.5rem">
            {LANGUAGE_OPTIONS.map(({ value, label }) => (
              <Button
                key={value}
                size="smaller"
                color={language === value ? 'primary' : 'translucent'}
                onClick={() => setLanguage(value)}
                disabled={isStreaming}
              >
                {label}
              </Button>
            ))}
          </div>
        </div>

        <div style="margin-bottom: 1rem">
          <Button
            color="primary"
            onClick={handleGenerate}
            disabled={isStreaming}
          >
            {isStreaming ? 'Generating…' : 'Generate summary'}
          </Button>
        </div>

        {error && (
          <div style="padding: 0.75rem; background: var(--color-error); color: white; border-radius: 0.5rem; margin-bottom: 1rem">
            {error}
          </div>
        )}

        {(summary || isStreaming) && (
          <div style="padding: 1rem; background: var(--color-background-secondary); border-radius: 0.5rem; white-space: pre-wrap; min-height: 4rem">
            {summary}
            {isStreaming && (
              <span style="display: inline-block; margin-left: 0.25rem; vertical-align: middle">
                <Spinner color="gray" />
              </span>
            )}
          </div>
        )}

        <div style="margin-top: 1rem; font-size: 0.75rem; color: var(--color-text-secondary)">
          {lang.isRtl ? '' : 'Generated by Claude. May contain inaccuracies.'}
        </div>
      </div>
    </Modal>
  );
};

export default memo(AiSummaryModal);
