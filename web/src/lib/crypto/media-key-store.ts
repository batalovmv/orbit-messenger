// Phase 7.1: in-memory registry of per-media AES keys recovered from
// decrypted E2E payloads. Lives outside the Saturn/crypto modules because
// both the receive path (`wsHandler`) and the download path
// (`api/saturn/methods/index.ts#downloadMedia`) need to reach it without
// pulling the heavy crypto chunk into their bundles.
//
// Keys are intentionally *not* persisted. If the user reloads the tab the
// envelope is re-decrypted from the stored ciphertext on the server, which
// re-populates this map. Keeping the registry ephemeral avoids leaking
// content keys onto disk beyond what the ratchet session already stores.
//
// The map is capped with a simple LRU to bound memory on chats with large
// media histories.

const MAX_ENTRIES = 512;

export type EncryptedMediaMaterial = {
  key: Uint8Array;
  nonce: Uint8Array;
  mime: string;
  filename?: string;
  size: number;
  width?: number;
  height?: number;
  duration?: number;
};

const store = new Map<string, EncryptedMediaMaterial>();

export function registerEncryptedMedia(mediaId: string, material: EncryptedMediaMaterial): void {
  if (store.has(mediaId)) {
    store.delete(mediaId);
  } else if (store.size >= MAX_ENTRIES) {
    const oldest = store.keys().next().value;
    if (oldest !== undefined) store.delete(oldest);
  }
  store.set(mediaId, material);
}

export function getEncryptedMedia(mediaId: string): EncryptedMediaMaterial | undefined {
  const entry = store.get(mediaId);
  if (entry) {
    // Refresh LRU order on read.
    store.delete(mediaId);
    store.set(mediaId, entry);
  }
  return entry;
}

export function hasEncryptedMedia(mediaId: string): boolean {
  return store.has(mediaId);
}

// Used by tests only.
export function __clearEncryptedMediaStore(): void {
  store.clear();
}
