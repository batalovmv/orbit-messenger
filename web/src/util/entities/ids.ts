import { CHANNEL_ID_BASE } from '../../config';
import { toJSNumber } from '../numbers';

// Saturn uses UUID strings as entity IDs — skip BigInt conversion for non-numeric IDs
function isUuid(id: string) {
  return /^[0-9a-f]{8}-/.test(id);
}

// Registry for non-user (group/channel) chat IDs.
const knownChatIds = new Set<string>();
let cacheConsumed = false;

function consumeCachedIds() {
  if (cacheConsumed) return;
  cacheConsumed = true;
  const cached = (window as any).__SATURN_CACHED_CHAT_IDS as string[] | undefined;
  if (cached) {
    for (const id of cached) knownChatIds.add(id);
    delete (window as any).__SATURN_CACHED_CHAT_IDS;
  }
}

export function registerChatId(id: string) { knownChatIds.add(id); }
export function unregisterChatId(id: string) { knownChatIds.delete(id); }

export function isUserId(entityId: string) {
  if (isUuid(entityId)) {
    consumeCachedIds();
    return !knownChatIds.has(entityId);
  }
  return !entityId.startsWith('-');
}

export function isChannelId(entityId: string) {
  if (isUuid(entityId)) return false;
  const n = BigInt(entityId);
  return n < -CHANNEL_ID_BASE;
}

export function toChannelId(mtpId: string) {
  if (isUuid(mtpId)) return mtpId;
  const n = BigInt(mtpId);
  return String(-CHANNEL_ID_BASE - n);
}

export function getRawPeerId(id: string) {
  if (isUuid(id)) return 0n; // Saturn UUIDs — return 0 as BigInt placeholder
  const n = BigInt(id);
  if (isUserId(id)) {
    return n;
  }

  if (isChannelId(id)) {
    return -n - CHANNEL_ID_BASE;
  }

  return n * -1n;
}

export function getPeerIdDividend(peerId: string) {
  if (isUuid(peerId)) return 0; // Saturn UUIDs — deterministic fallback
  return toJSNumber(getRawPeerId(peerId));
}
