import { memo, useEffect, useState } from '../../../lib/teact/teact';

import { translateMessages } from '../../../api/saturn/methods/ai';
import { resolveMessageUuid } from '../../../api/saturn/methods/messages';
import buildClassName from '../../../util/buildClassName';

import useLang from '../../../hooks/useLang';

import Icon from '../../common/icons/Icon';
import Spinner from '../../ui/Spinner';

import styles from './AiTranslateInline.module.scss';

// ---------------------------------------------------------------------------
// Module-level translation store. A context-menu action elsewhere in the
// tree calls `startMessageTranslation(...)`, which updates this Map and
// notifies every mounted AiTranslateInline subscribing to that message id.
// The cache is intentionally outside the Teact global store — we don't
// want each chat-list scroll pushing updates through withGlobal for what
// is strictly per-session, per-message UI state.
// ---------------------------------------------------------------------------

type TranslationEntry =
  | { status: 'loading'; targetLang: string; text: string }
  | { status: 'done'; targetLang: string; text: string }
  | { status: 'error'; targetLang: string; messageKey: string };

const cache = new Map<number, TranslationEntry>();
const listeners = new Map<number, Set<() => void>>();

function subscribe(messageId: number, cb: () => void): () => void {
  let set = listeners.get(messageId);
  if (!set) {
    set = new Set();
    listeners.set(messageId, set);
  }
  set.add(cb);
  return () => { set!.delete(cb); if (set!.size === 0) listeners.delete(messageId); };
}

function updateEntry(messageId: number, entry: TranslationEntry | undefined) {
  if (entry) {
    cache.set(messageId, entry);
  } else {
    cache.delete(messageId);
  }
  const set = listeners.get(messageId);
  if (!set) return;
  for (const cb of set) cb();
}

/**
 * Kick off (or restart) translation for a specific message. Safe to call
 * repeatedly — we always reset state, so a second call to translate into
 * a different language replaces the prior result without a stale-chunk
 * race. Trigger comes from MessageContextMenu.
 */
export function startMessageTranslation(
  chatId: string,
  messageId: number,
  targetLang: string,
): void {
  // Saturn backend wants the message UUID, not the Telegram Web numeric
  // (= sequence_number) id that the rest of the app uses. The mapping
  // lives in saturn/apiBuilders/messages and is populated as messages
  // get hydrated; if it's missing the message hasn't been loaded yet.
  const uuid = resolveMessageUuid(chatId, messageId);
  if (!uuid) {
    updateEntry(messageId, { status: 'error', targetLang, messageKey: 'AiTranslateFailed' });
    return;
  }

  updateEntry(messageId, { status: 'loading', targetLang, text: '' });

  (async () => {
    try {
      let accumulated = '';
      for await (const chunk of translateMessages({
        chatId,
        messageIds: [uuid],
        targetLanguage: targetLang,
      })) {
        accumulated += chunk;
        // Mid-stream updates — lets the UI render tokens as they land.
        updateEntry(messageId, { status: 'loading', targetLang, text: accumulated });
      }
      if (!accumulated.trim()) {
        updateEntry(messageId, { status: 'error', targetLang, messageKey: 'AiNotConfigured' });
        return;
      }
      updateEntry(messageId, { status: 'done', targetLang, text: accumulated });
    } catch (err) {
      const e = err as Error & { status?: number };
      const messageKey = e?.status === 503 ? 'AiNotConfigured'
        : e?.status === 429 ? 'AiSuggestRateLimited'
        : e?.status === 502 || e?.status === 500 || e?.status === 504 ? 'AiSuggestUnavailable'
        : 'AiTranslateFailed';
      updateEntry(messageId, { status: 'error', targetLang, messageKey });
    }
  })();
}

/** Tells callers whether a translation has already been requested for this
 * message (used to toggle the context-menu item between "Translate" and
 * "Hide translation"). */
export function hasMessageTranslation(messageId: number): boolean {
  return cache.has(messageId);
}

/** Remove the cached translation so the inline block disappears. */
export function clearMessageTranslation(messageId: number): void {
  if (cache.has(messageId)) {
    updateEntry(messageId, undefined);
  }
}

// ---------------------------------------------------------------------------
// Inline UI. Mounted under every text message; renders nothing until the
// message has been translated at least once this session.
// ---------------------------------------------------------------------------

type OwnProps = {
  messageId: number;
};

const AiTranslateInline = ({ messageId }: OwnProps) => {
  const lang = useLang();

  const [entry, setEntry] = useState<TranslationEntry | undefined>(() => cache.get(messageId));

  useEffect(() => {
    setEntry(cache.get(messageId));
    return subscribe(messageId, () => setEntry(cache.get(messageId)));
  }, [messageId]);

  if (!entry) return undefined;

  if (entry.status === 'loading' && !entry.text) {
    return (
      <div className={styles.root}>
        <Spinner color="gray" />
        <span className={styles.label}>{lang('AiTranslateStreaming')}</span>
      </div>
    );
  }

  if (entry.status === 'error') {
    return (
      <div className={buildClassName(styles.root, styles.error)}>
        <Icon name="warning" />
        <span className={styles.label}>{lang(entry.messageKey as Parameters<typeof lang>[0])}</span>
      </div>
    );
  }

  // status === 'loading' (with partial text) or 'done' — same shape,
  // a tiny spinner indicates the stream is still landing.
  const isStreaming = entry.status === 'loading';
  return (
    <div className={buildClassName(styles.root, styles.done)}>
      <div className={styles.header}>
        <Icon name="language" />
        <span className={styles.headerLabel}>{entry.targetLang.toUpperCase()}</span>
        {isStreaming && <Spinner color="gray" />}
      </div>
      <p className={styles.text} dir="auto">{entry.text}</p>
    </div>
  );
};

export default memo(AiTranslateInline);
