// IndexedDB persistence for device-local crypto material.
//
// Database: `orbit-crypto`, version 1.
// Object stores (see docs/phase7-design.md §5):
//
//   identity          — singleton `'self'` record {deviceId, identityKeyPair, createdAt}
//   signed_prekeys    — key = numeric keyId
//   one_time_prekeys  — key = numeric keyId (deleted on consumption)
//   sessions          — key = "peerUserId:peerDeviceId", value = serialized ratchet state
//   verified          — key = peerUserId, value = {identityHash, verifiedAt}
//   message_cache     — key = messageId, value = {plaintext, decryptedAt}

import { openDB, type IDBPDatabase } from 'idb';

import type {
  IdentityKeyPair,
  OneTimePreKey,
  SerializedSession,
  SignedPreKey,
} from './types';

export const DB_NAME = 'orbit-crypto';
export const DB_VERSION = 1;

export const STORE_IDENTITY = 'identity';
export const STORE_SIGNED_PREKEYS = 'signed_prekeys';
export const STORE_ONE_TIME_PREKEYS = 'one_time_prekeys';
export const STORE_SESSIONS = 'sessions';
export const STORE_VERIFIED = 'verified';
export const STORE_MESSAGE_CACHE = 'message_cache';

const SELF_KEY = 'self';

export type IdentityRecord = {
  deviceId: string;
  identityKeyPair: IdentityKeyPair;
  createdAt: number;
};

export type VerifiedRecord = {
  peerUserId: string;
  identityHash: string;
  verifiedAt: number;
};

export type MessageCacheRecord = {
  messageId: string;
  plaintext: string;
  decryptedAt: number;
};

let dbPromise: Promise<IDBPDatabase> | undefined;

function getDB(): Promise<IDBPDatabase> {
  if (!dbPromise) {
    dbPromise = openDB(DB_NAME, DB_VERSION, {
      upgrade(db) {
        if (!db.objectStoreNames.contains(STORE_IDENTITY)) {
          db.createObjectStore(STORE_IDENTITY);
        }
        if (!db.objectStoreNames.contains(STORE_SIGNED_PREKEYS)) {
          db.createObjectStore(STORE_SIGNED_PREKEYS);
        }
        if (!db.objectStoreNames.contains(STORE_ONE_TIME_PREKEYS)) {
          db.createObjectStore(STORE_ONE_TIME_PREKEYS);
        }
        if (!db.objectStoreNames.contains(STORE_SESSIONS)) {
          db.createObjectStore(STORE_SESSIONS);
        }
        if (!db.objectStoreNames.contains(STORE_VERIFIED)) {
          db.createObjectStore(STORE_VERIFIED);
        }
        if (!db.objectStoreNames.contains(STORE_MESSAGE_CACHE)) {
          db.createObjectStore(STORE_MESSAGE_CACHE);
        }
      },
    });
  }
  return dbPromise;
}

/**
 * Resets the internal database handle. Used by tests to start from a clean
 * slate, and by the future "Log out of all devices" flow.
 */
export function resetKeyStoreForTests(): void {
  dbPromise = undefined;
}

// ────────────────────────────────────────────────────────────────────────────
// Identity
// ────────────────────────────────────────────────────────────────────────────

export async function getIdentity(): Promise<IdentityRecord | undefined> {
  const db = await getDB();
  return db.get(STORE_IDENTITY, SELF_KEY) as Promise<IdentityRecord | undefined>;
}

export async function putIdentity(record: IdentityRecord): Promise<void> {
  const db = await getDB();
  await db.put(STORE_IDENTITY, record, SELF_KEY);
}

// ────────────────────────────────────────────────────────────────────────────
// Signed pre-keys
// ────────────────────────────────────────────────────────────────────────────

export async function putSignedPreKey(key: SignedPreKey): Promise<void> {
  const db = await getDB();
  await db.put(STORE_SIGNED_PREKEYS, key, key.keyId);
}

export async function getSignedPreKey(keyId: number): Promise<SignedPreKey | undefined> {
  const db = await getDB();
  return db.get(STORE_SIGNED_PREKEYS, keyId) as Promise<SignedPreKey | undefined>;
}

export async function getLatestSignedPreKey(): Promise<SignedPreKey | undefined> {
  const db = await getDB();
  const all = (await db.getAll(STORE_SIGNED_PREKEYS)) as SignedPreKey[];
  if (all.length === 0) return undefined;
  return all.reduce((a, b) => (a.keyId > b.keyId ? a : b));
}

// ────────────────────────────────────────────────────────────────────────────
// One-time pre-keys
// ────────────────────────────────────────────────────────────────────────────

export async function putOneTimePreKey(key: OneTimePreKey): Promise<void> {
  const db = await getDB();
  await db.put(STORE_ONE_TIME_PREKEYS, key, key.keyId);
}

export async function getOneTimePreKey(keyId: number): Promise<OneTimePreKey | undefined> {
  const db = await getDB();
  return db.get(STORE_ONE_TIME_PREKEYS, keyId) as Promise<OneTimePreKey | undefined>;
}

export async function deleteOneTimePreKey(keyId: number): Promise<void> {
  const db = await getDB();
  await db.delete(STORE_ONE_TIME_PREKEYS, keyId);
}

export async function countOneTimePreKeys(): Promise<number> {
  const db = await getDB();
  return db.count(STORE_ONE_TIME_PREKEYS);
}

// ────────────────────────────────────────────────────────────────────────────
// Sessions
// ────────────────────────────────────────────────────────────────────────────

function sessionKey(peerUserId: string, peerDeviceId: string): string {
  return `${peerUserId}:${peerDeviceId}`;
}

export async function getSession(
  peerUserId: string,
  peerDeviceId: string,
): Promise<SerializedSession | undefined> {
  const db = await getDB();
  return db.get(STORE_SESSIONS, sessionKey(peerUserId, peerDeviceId)) as Promise<
    SerializedSession | undefined
  >;
}

export async function putSession(
  peerUserId: string,
  peerDeviceId: string,
  blob: SerializedSession,
): Promise<void> {
  const db = await getDB();
  await db.put(STORE_SESSIONS, blob, sessionKey(peerUserId, peerDeviceId));
}

export async function deleteSession(
  peerUserId: string,
  peerDeviceId: string,
): Promise<void> {
  const db = await getDB();
  await db.delete(STORE_SESSIONS, sessionKey(peerUserId, peerDeviceId));
}

// ────────────────────────────────────────────────────────────────────────────
// Verified peers (Safety Numbers confirmation)
// ────────────────────────────────────────────────────────────────────────────

export async function getVerified(peerUserId: string): Promise<VerifiedRecord | undefined> {
  const db = await getDB();
  return db.get(STORE_VERIFIED, peerUserId) as Promise<VerifiedRecord | undefined>;
}

export async function putVerified(record: VerifiedRecord): Promise<void> {
  const db = await getDB();
  await db.put(STORE_VERIFIED, record, record.peerUserId);
}

export async function deleteVerified(peerUserId: string): Promise<void> {
  const db = await getDB();
  await db.delete(STORE_VERIFIED, peerUserId);
}

// ────────────────────────────────────────────────────────────────────────────
// Runtime message cache (never persisted beyond logout)
// ────────────────────────────────────────────────────────────────────────────

export async function putMessagePlaintext(messageId: string, plaintext: string): Promise<void> {
  const db = await getDB();
  const record: MessageCacheRecord = {
    messageId,
    plaintext,
    decryptedAt: Date.now(),
  };
  await db.put(STORE_MESSAGE_CACHE, record, messageId);
}

export async function getMessagePlaintext(messageId: string): Promise<string | undefined> {
  const db = await getDB();
  const record = (await db.get(STORE_MESSAGE_CACHE, messageId)) as MessageCacheRecord | undefined;
  return record?.plaintext;
}

export async function deleteMessagePlaintext(messageId: string): Promise<void> {
  const db = await getDB();
  await db.delete(STORE_MESSAGE_CACHE, messageId);
}

export async function clearAllCryptoState(): Promise<void> {
  const db = await getDB();
  const tx = db.transaction(
    [
      STORE_IDENTITY,
      STORE_SIGNED_PREKEYS,
      STORE_ONE_TIME_PREKEYS,
      STORE_SESSIONS,
      STORE_VERIFIED,
      STORE_MESSAGE_CACHE,
    ],
    'readwrite',
  );
  await Promise.all([
    tx.objectStore(STORE_IDENTITY).clear(),
    tx.objectStore(STORE_SIGNED_PREKEYS).clear(),
    tx.objectStore(STORE_ONE_TIME_PREKEYS).clear(),
    tx.objectStore(STORE_SESSIONS).clear(),
    tx.objectStore(STORE_VERIFIED).clear(),
    tx.objectStore(STORE_MESSAGE_CACHE).clear(),
  ]);
  await tx.done;
}
