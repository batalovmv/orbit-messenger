import { useEffect, useState } from '../../../../lib/teact/teact';

import { suggestReply } from '../../../../api/saturn/methods/ai';

import useFlag from '../../../../hooks/useFlag';
import useLang from '../../../../hooks/useLang';
import useLastCallback from '../../../../hooks/useLastCallback';

type ApiErrorLike = Error & { status?: number };

// Map backend error shapes to user-facing copy. 5xx often comes from the
// AI service being transiently down mid-deploy or Claude returning a
// gateway error — we surface a friendly message instead of echoing the
// raw "Internal server error" string the handler happened to include.
function resolveErrorMessage(err: ApiErrorLike, lang: ReturnType<typeof useLang>): string {
  switch (err?.status) {
    case 503: return lang('AiNotConfigured');
    case 429: return lang('AiSuggestRateLimited');
    case 502:
    case 504:
    case 500: return lang('AiSuggestUnavailable');
    default: return lang('AiSuggestFailed');
  }
}

export type AiSuggestReplyState = {
  isOpen: boolean;
  isLoading: boolean;
  suggestions?: string[];
  error?: string;
  open: NoneToVoidFunction;
  close: NoneToVoidFunction;
  requestSuggestions: NoneToVoidFunction;
  pickSuggestion: (text: string) => void;
};

/**
 * Owns the state for Orbit's AI "Suggest reply" feature. Called once at
 * the Composer level so the inline trigger button and the floating
 * tooltip panel stay in sync without prop-drilling.
 *
 * State lifecycle:
 * - Closed, no data on mount.
 * - `open()` flips isOpen and fires `requestSuggestions` — the tooltip
 *   shows spinner until the fetch resolves.
 * - On success: suggestions populated, chips rendered.
 * - On error: error message populated, red strip rendered.
 * - Switching chats resets everything.
 * - Typing into the composer (`hasText=true`) auto-closes: we never
 *   want to overwrite a user's in-progress input.
 */
export default function useAiSuggestReply(
  chatId: string,
  hasText: boolean,
  onInsertSuggestion: (text: string) => void,
): AiSuggestReplyState {
  const lang = useLang();

  const [isOpen, open, close] = useFlag(false);
  const [isLoading, startLoading, stopLoading] = useFlag(false);
  const [suggestions, setSuggestions] = useState<string[] | undefined>();
  const [error, setError] = useState<string | undefined>();

  // Reset on chat switch — stale suggestions across chats are worse than none.
  useEffect(() => {
    close();
    setSuggestions(undefined);
    setError(undefined);
    stopLoading();
  }, [chatId, close, stopLoading]);

  // Auto-close when user starts typing so our panel never covers their input.
  useEffect(() => {
    if (hasText && isOpen) close();
  }, [hasText, isOpen, close]);

  const requestSuggestions = useLastCallback(async () => {
    setError(undefined);
    setSuggestions(undefined);
    startLoading();
    try {
      const result = await suggestReply({ chatId });
      if (!result || result.length === 0) {
        setError(lang('AiNotConfigured'));
      } else {
        setSuggestions(result);
      }
    } catch (err) {
      setError(resolveErrorMessage(err as ApiErrorLike, lang));
    } finally {
      stopLoading();
    }
  });

  const handleOpen = useLastCallback(() => {
    open();
    // Only re-fetch when there's nothing to show. If the user already
    // opened once this session and closed without picking, reuse the
    // same chips instead of burning another Claude call.
    if (!suggestions && !error && !isLoading) {
      void requestSuggestions();
    }
  });

  const pickSuggestion = useLastCallback((text: string) => {
    onInsertSuggestion(text);
    setSuggestions(undefined);
    setError(undefined);
    close();
  });

  return {
    isOpen,
    isLoading,
    suggestions,
    error,
    open: handleOpen,
    close,
    requestSuggestions,
    pickSuggestion,
  };
}
