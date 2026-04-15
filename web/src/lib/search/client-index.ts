// Client-side search index for E2E chats (Phase 7 Step 8).
//
// E2E messages never hit Meilisearch on the server (see
// `services/messaging/internal/search/indexer.go:122` — the indexer
// skips `type=encrypted`), so any full-text search over E2E history
// has to happen on the client.
//
// Design (docs/phase7-design.md §12):
//
//   * Storage: dedicated IndexedDB database `orbit-client-search`,
//     version 1, single object store `search_index` keyed by a
//     composite `chatId:word` identifier mapping to
//     `{chatId, word, messageIds: number[]}`.
//   * Populate: every time the WS receive path decrypts an E2E
//     message we call `addToIndex(chatId, messageId, plaintext)`.
//     Tokens are the whitespace-split lowercase words of the
//     plaintext. No stemming, no morphology — for a 150-user corp
//     messenger we accept exact-word matches as v1.
//   * Query: `searchClient(chatId, query)` ∩-joins all query tokens
//     and returns the set of matching message ids inside that chat.
//   * Persistence: survives reloads, cleared only on
//     "Log out on all devices" (wired into
//     `lib/crypto/key-store.ts:clearAllCryptoState` in a follow-up).
//
// The module uses `idb` (already a Phase 7 Step 1 dep) so it gets
// the same Promise-based wrapper the crypto key store uses.

import { openDB, type IDBPDatabase } from 'idb';

export const DB_NAME = 'orbit-client-search';
export const DB_VERSION = 1;
export const STORE_INDEX = 'search_index';

type IndexRecord = {
  chatId: string;
  word: string;
  messageIds: number[];
};

let dbPromise: Promise<IDBPDatabase> | undefined;

function getDB(): Promise<IDBPDatabase> {
  if (!dbPromise) {
    dbPromise = openDB(DB_NAME, DB_VERSION, {
      upgrade(db) {
        if (!db.objectStoreNames.contains(STORE_INDEX)) {
          db.createObjectStore(STORE_INDEX);
        }
      },
    });
  }
  return dbPromise;
}

export function resetClientIndexForTests(): void {
  dbPromise = undefined;
}

function compositeKey(chatId: string, word: string): string {
  return `${chatId}:${word}`;
}

// ────────────────────────────────────────────────────────────────────────────
// Tokenization
// ────────────────────────────────────────────────────────────────────────────

// Lowercase + split on anything that isn't a Unicode letter or digit.
// Keeps Russian, English, and any other script — covers MST's user
// base. No stemming / normalization — this is deliberate; we favour
// exact-match precision over recall for v1.
export function tokenize(text: string): string[] {
  if (!text) return [];
  const lowered = text.toLowerCase();
  const out: string[] = [];
  const pattern = /[\p{L}\p{N}]+/gu;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(lowered)) !== null) {
    if (match[0].length >= 2) {
      out.push(match[0]);
    }
  }
  return out;
}

// ────────────────────────────────────────────────────────────────────────────
// Index mutation
// ────────────────────────────────────────────────────────────────────────────

/**
 * Add a decrypted message's tokens to the per-chat inverted index.
 * Idempotent per `(chatId, word, messageId)` triple — duplicate
 * invocations produce the same result without inflating the posting
 * list.
 */
export async function addToIndex(
  chatId: string,
  messageId: number,
  plaintext: string,
): Promise<void> {
  const tokens = Array.from(new Set(tokenize(plaintext)));
  if (tokens.length === 0) return;

  const db = await getDB();
  const tx = db.transaction(STORE_INDEX, 'readwrite');
  const store = tx.objectStore(STORE_INDEX);
  for (const word of tokens) {
    const key = compositeKey(chatId, word);
    const existing = (await store.get(key)) as IndexRecord | undefined;
    if (existing) {
      if (!existing.messageIds.includes(messageId)) {
        existing.messageIds.push(messageId);
        await store.put(existing, key);
      }
    } else {
      await store.put({ chatId, word, messageIds: [messageId] } satisfies IndexRecord, key);
    }
  }
  await tx.done;
}

/**
 * Remove a message from all posting lists for a chat (used when a
 * disappearing timer expires or the user deletes a message locally).
 * Cheap for small indexes; for 150-user corp scale we accept the
 * full-scan cost.
 */
export async function removeFromIndex(chatId: string, messageId: number): Promise<void> {
  const db = await getDB();
  const tx = db.transaction(STORE_INDEX, 'readwrite');
  const store = tx.objectStore(STORE_INDEX);
  const all = (await store.getAll()) as IndexRecord[];
  for (const record of all) {
    if (record.chatId !== chatId) continue;
    const before = record.messageIds.length;
    record.messageIds = record.messageIds.filter((id) => id !== messageId);
    if (record.messageIds.length !== before) {
      const key = compositeKey(record.chatId, record.word);
      if (record.messageIds.length === 0) {
        await store.delete(key);
      } else {
        await store.put(record, key);
      }
    }
  }
  await tx.done;
}

/**
 * Clear every posting list for a chat — used when leaving / deleting
 * an E2E chat, or as part of "Log out of all devices" cleanup.
 */
export async function clearChatIndex(chatId: string): Promise<void> {
  const db = await getDB();
  const tx = db.transaction(STORE_INDEX, 'readwrite');
  const store = tx.objectStore(STORE_INDEX);
  const all = (await store.getAll()) as IndexRecord[];
  for (const record of all) {
    if (record.chatId === chatId) {
      await store.delete(compositeKey(record.chatId, record.word));
    }
  }
  await tx.done;
}

export async function clearAllClientSearchIndex(): Promise<void> {
  const db = await getDB();
  await db.clear(STORE_INDEX);
}

// ────────────────────────────────────────────────────────────────────────────
// Query
// ────────────────────────────────────────────────────────────────────────────

/**
 * Returns the set of message ids inside `chatId` whose decrypted text
 * contains every token in the query. Returns an empty array when any
 * token has no matches (strict AND semantics).
 */
export async function searchClient(chatId: string, query: string): Promise<number[]> {
  const tokens = Array.from(new Set(tokenize(query)));
  if (tokens.length === 0) return [];

  const db = await getDB();
  const store = db.transaction(STORE_INDEX, 'readonly').objectStore(STORE_INDEX);

  let intersection: Set<number> | undefined;
  for (const token of tokens) {
    const record = (await store.get(compositeKey(chatId, token))) as IndexRecord | undefined;
    const ids = record?.messageIds ?? [];
    if (ids.length === 0) return [];
    if (!intersection) {
      intersection = new Set(ids);
    } else {
      const next = new Set<number>();
      for (const id of ids) if (intersection.has(id)) next.add(id);
      intersection = next;
      if (intersection.size === 0) return [];
    }
  }

  return Array.from(intersection ?? []).sort((a, b) => a - b);
}
