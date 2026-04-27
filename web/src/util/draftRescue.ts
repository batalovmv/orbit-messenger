import { getActions, getGlobal } from '../global';

import type { ApiFormattedText } from '../api/types';
import type { ThreadId } from '../types';

import { forceUpdateCache } from '../global/cache';
import { selectTabState } from '../global/selectors';
import parseHtmlAsFormattedText from './parseHtmlAsFormattedText';

// localStorage rescue contract: written synchronously by `prepareForUpdateReload`
// just before triggering a reload (deploy, stale-chunk, manual update). Read on
// next boot from `restoreRescuedDrafts`. Async IDB cache writes are not
// guaranteed to commit during page tear-down, so localStorage is the bridge.

const RESCUE_KEY = 'tt-draft-rescue-v1';
const MAX_RESCUE_AGE_MS = 7 * 24 * 60 * 60 * 1000; // 7 days

type ComposerFlush = () => void;

type RescueRecord = {
  chatId: string;
  threadId: ThreadId;
  text?: ApiFormattedText;
  htmlSnapshot?: string;
  savedAt: number;
};

type RescueEnvelope = {
  v: 1;
  savedAt: number;
  records: RescueRecord[];
};

const composerFlushes = new Set<ComposerFlush>();

export function registerComposerFlush(flush: ComposerFlush): () => void {
  composerFlushes.add(flush);
  return () => {
    composerFlushes.delete(flush);
  };
}

function flushAllComposers() {
  for (const flush of composerFlushes) {
    try {
      flush();
    } catch {
      // best-effort: a single composer failing must not block update reload
    }
  }
}

function snapshotActiveDrafts(): RescueRecord[] {
  const records: RescueRecord[] = [];
  try {
    const global = getGlobal();
    const byChat = global.messages?.byChatId;
    if (!byChat) return records;

    const now = Date.now();
    for (const chatId of Object.keys(byChat)) {
      const threadsById = byChat[chatId]?.threadsById;
      if (!threadsById) continue;

      for (const threadIdRaw of Object.keys(threadsById)) {
        const thread = threadsById[threadIdRaw as unknown as ThreadId] as
          | { localState?: { draft?: { text?: ApiFormattedText } } } | undefined;
        const draft = thread?.localState?.draft;
        if (!draft?.text || !draft.text.text?.length) continue;

        records.push({
          chatId,
          threadId: threadIdRaw as unknown as ThreadId,
          text: draft.text,
          savedAt: now,
        });
      }
    }
  } catch {
    // ignore
  }
  return records;
}

function appendCurrentEditableSnapshot(records: RescueRecord[]) {
  // Composer keeps live HTML in a contenteditable that hasn't necessarily been
  // debounced into the global draft. Pick it up so the user does not lose the
  // very last keystrokes before a deploy reload.
  if (typeof document === 'undefined') return;

  const inputs = document.querySelectorAll<HTMLElement>(
    '#editable-message-text, #editable-message-text-edit-help, [contenteditable="true"][data-message-input]',
  );
  if (!inputs.length) return;

  const tabState = (() => {
    try {
      return selectTabState(getGlobal());
    } catch {
      return undefined;
    }
  })();
  const chatId = tabState?.messageLists?.[tabState.messageLists.length - 1]?.chatId;
  const threadId = tabState?.messageLists?.[tabState.messageLists.length - 1]?.threadId;
  if (!chatId || threadId === undefined) return;

  const html = inputs[0]?.innerHTML?.trim();
  if (!html) return;

  let parsed: ApiFormattedText | undefined;
  try {
    parsed = parseHtmlAsFormattedText(html);
  } catch {
    parsed = undefined;
  }
  if (!parsed?.text?.length && !html) return;

  const existing = records.find((r) => r.chatId === chatId && String(r.threadId) === String(threadId));
  if (existing) {
    if (parsed?.text?.length && parsed.text.length >= (existing.text?.text?.length ?? 0)) {
      existing.text = parsed;
      existing.htmlSnapshot = html;
    }
    return;
  }
  records.push({
    chatId,
    threadId,
    text: parsed,
    htmlSnapshot: html,
    savedAt: Date.now(),
  });
}

function writeRescue(records: RescueRecord[]) {
  if (!records.length) {
    try {
      localStorage.removeItem(RESCUE_KEY);
    } catch {
      // ignore
    }
    return;
  }
  const envelope: RescueEnvelope = { v: 1, savedAt: Date.now(), records };
  try {
    localStorage.setItem(RESCUE_KEY, JSON.stringify(envelope));
  } catch {
    // Quota exceeded or disabled (private mode). Best-effort: write smaller text-only
    // copies to maximise the chance of saving at least the message bodies.
    try {
      const trimmed: RescueEnvelope = {
        v: 1,
        savedAt: envelope.savedAt,
        records: records.map((r) => ({
          chatId: r.chatId, threadId: r.threadId, text: r.text, savedAt: r.savedAt,
        })),
      };
      localStorage.setItem(RESCUE_KEY, JSON.stringify(trimmed));
    } catch {
      // give up
    }
  }
}

// Synchronous-ish path called from the update banner / SW staleChunkDetected.
// 1. Flush composer hooks → updates global draft state synchronously.
// 2. Snapshot active drafts (and the current contenteditable) into localStorage.
// 3. Kick `forceUpdateCache(true)` so the global cache hits IDB best-effort.
// 4. Caller awaits a short grace, then reloads.
export function prepareUpdateRescue() {
  flushAllComposers();
  const records = snapshotActiveDrafts();
  appendCurrentEditableSnapshot(records);
  writeRescue(records);
  try {
    forceUpdateCache(false);
  } catch {
    // best-effort
  }
}

export function readRescue(): RescueRecord[] {
  try {
    const raw = localStorage.getItem(RESCUE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as RescueEnvelope;
    if (!parsed || parsed.v !== 1 || !Array.isArray(parsed.records)) return [];
    if (Date.now() - parsed.savedAt > MAX_RESCUE_AGE_MS) {
      localStorage.removeItem(RESCUE_KEY);
      return [];
    }
    return parsed.records;
  } catch {
    return [];
  }
}

export function clearRescue() {
  try {
    localStorage.removeItem(RESCUE_KEY);
  } catch {
    // ignore
  }
}

// Boot-time restore: replay rescue records as `saveDraft` dispatches if they
// are newer than what IDB already restored. Skipped when the cached draft
// matches or post-dates the rescue (user managed to save before reload).
export function restoreRescuedDrafts() {
  const records = readRescue();
  if (!records.length) {
    return;
  }

  const dispatch = getActions();
  if (!dispatch?.saveDraft) {
    // Actions not wired yet — keep rescue for next boot attempt.
    return;
  }

  let restored = 0;
  for (const record of records) {
    if (!record.text || !record.text.text?.length) continue;
    let existingDraftDate = 0;
    try {
      const global = getGlobal();
      const existing = global.messages?.byChatId?.[record.chatId]
        ?.threadsById?.[record.threadId as unknown as keyof object] as
        | { localState?: { draft?: { date?: number; text?: ApiFormattedText } } } | undefined;
      existingDraftDate = (existing?.localState?.draft?.date ?? 0) * 1000;
    } catch {
      existingDraftDate = 0;
    }
    if (existingDraftDate > record.savedAt) continue;

    try {
      dispatch.saveDraft({
        chatId: record.chatId,
        threadId: record.threadId,
        text: record.text,
      });
      restored++;
    } catch {
      // best-effort — continue with the rest
    }
  }

  // Clear rescue once we have made a restore attempt: keeping it would replay
  // on every reload. If the dispatch failed silently, the next boot will
  // surface no draft and the user retypes — strictly better than infinite loop.
  clearRescue();

  if (restored && typeof window !== 'undefined' && (window as any).Notification) {
    // No-op: actual UX surfacing handled in the banner once mounted.
  }
}
