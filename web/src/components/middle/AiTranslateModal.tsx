import { memo, useEffect, useRef, useState } from '../../lib/teact/teact';
import { withGlobal } from '../../global';

import { translateMessages } from '../../api/saturn/methods/ai';
import { selectChatMessage } from '../../global/selectors';

import useFlag from '../../hooks/useFlag';
import useLang from '../../hooks/useLang';
import useLastCallback from '../../hooks/useLastCallback';

import Button from '../ui/Button';
import Modal from '../ui/Modal';
import Spinner from '../ui/Spinner';

type Language = 'en' | 'ru' | 'es' | 'de' | 'fr';

const LANGUAGE_OPTIONS: Array<{ value: Language; label: string }> = [
  { value: 'en', label: 'EN' },
  { value: 'ru', label: 'RU' },
  { value: 'es', label: 'ES' },
  { value: 'de', label: 'DE' },
  { value: 'fr', label: 'FR' },
];

const MAX_MESSAGES = 50;

type OwnProps = {
  isOpen: boolean;
  chatId?: string;
  messageIds?: number[];
  onClose: NoneToVoidFunction;
};

type StateProps = {
  translatableIds: number[];
};

const AiTranslateModal = ({
  isOpen, chatId, messageIds, translatableIds, onClose,
}: OwnProps & StateProps) => {
  const lang = useLang();

  const [targetLanguage, setTargetLanguage] = useState<Language>('en');
  const [output, setOutput] = useState('');
  const [error, setError] = useState<string | undefined>();
  const [isStreaming, startStreaming, stopStreaming] = useFlag(false);

  const abortedRef = useRef(false);

  useEffect(() => {
    if (isOpen) {
      setOutput('');
      setError(undefined);
      abortedRef.current = false;
    } else {
      abortedRef.current = true;
    }
  }, [isOpen]);

  const handleGenerate = useLastCallback(async () => {
    if (!chatId || translatableIds.length === 0) return;

    abortedRef.current = false;
    setOutput('');
    setError(undefined);
    startStreaming();

    try {
      const ids = translatableIds.slice(0, MAX_MESSAGES).map(String);
      const generator = translateMessages({
        chatId,
        messageIds: ids,
        targetLanguage,
      });

      let accumulated = '';
      for await (const chunk of generator) {
        if (abortedRef.current) break;
        accumulated += chunk;
        setOutput(accumulated);
      }

      if (!accumulated && !abortedRef.current) {
        setError(lang('AiNotConfigured'));
      }
    } catch (err) {
      const e = err as Error & { status?: number };
      if (e?.status === 503) {
        setError(lang('AiNotConfigured'));
      } else {
        setError(e?.message || lang('AiTranslateFailed'));
      }
    } finally {
      stopStreaming();
    }
  });

  const handleClose = useLastCallback(() => {
    abortedRef.current = true;
    stopStreaming();
    onClose();
  });

  const handleCopyAll = useLastCallback(() => {
    if (output) {
      navigator.clipboard?.writeText(output).catch(() => { /* noop */ });
    }
  });

  const selectedCount = messageIds?.length ?? 0;
  const translatableCount = translatableIds.length;
  const skippedCount = selectedCount - translatableCount;

  return (
    <Modal
      isOpen={isOpen}
      onClose={handleClose}
      title={lang('AiTranslateTitle')}
      hasCloseButton
      className="ai-translate-modal"
    >
      <div style="padding: 1rem; max-width: 480px">
        <div style="margin-bottom: 1rem">
          <p style="font-size: 0.8125rem; color: var(--color-text-secondary); margin-bottom: 0.5rem">
            {lang('AiTranslateLanguage')}
          </p>
          <div style="display: flex; gap: 0.5rem; flex-wrap: wrap">
            {LANGUAGE_OPTIONS.map(({ value, label }) => (
              <Button
                key={value}
                size="smaller"
                color={targetLanguage === value ? 'primary' : 'translucent'}
                onClick={() => setTargetLanguage(value)}
                disabled={isStreaming}
              >
                {label}
              </Button>
            ))}
          </div>
        </div>

        <div style="margin-bottom: 0.75rem; font-size: 0.75rem; color: var(--color-text-secondary)">
          {lang('AiTranslateSelectedCount', { count: translatableCount })}
          {skippedCount > 0 && ` — ${lang('AiTranslateSkipped', { count: skippedCount })}`}
        </div>

        <div style="margin-bottom: 1rem; display: flex; gap: 0.5rem">
          <Button
            color="primary"
            onClick={handleGenerate}
            disabled={isStreaming || translatableCount === 0}
          >
            {isStreaming ? lang('AiTranslateStreaming') : lang('AiTranslateGenerate')}
          </Button>
          {output && !isStreaming && (
            <Button color="translucent" onClick={handleCopyAll}>
              {lang('AiTranslateCopy')}
            </Button>
          )}
        </div>

        {error && (
          <div style="padding: 0.75rem; background: var(--color-error); color: white; border-radius: 0.5rem; margin-bottom: 1rem">
            {error}
          </div>
        )}

        {(output || isStreaming) && (
          <div style="padding: 1rem; background: var(--color-background-secondary); border-radius: 0.5rem; white-space: pre-wrap; min-height: 4rem; max-height: 24rem; overflow-y: auto">
            {output}
            {isStreaming && (
              <span style="display: inline-block; margin-left: 0.25rem; vertical-align: middle">
                <Spinner color="gray" />
              </span>
            )}
          </div>
        )}

        <div style="margin-top: 1rem; font-size: 0.75rem; color: var(--color-text-secondary)">
          {lang('AiGeneratedDisclaimer')}
        </div>
      </div>
    </Modal>
  );
};

export default memo(withGlobal<OwnProps>(
  (global, { chatId, messageIds }): Complete<StateProps> => {
    if (!chatId || !messageIds || messageIds.length === 0) {
      return { translatableIds: [] };
    }
    const ids: number[] = [];
    for (const id of messageIds) {
      const message = selectChatMessage(global, chatId, id);
      if (message?.content?.text?.text) {
        ids.push(id);
      }
    }
    return { translatableIds: ids };
  },
)(AiTranslateModal));
