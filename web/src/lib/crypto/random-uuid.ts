// Tiny UUID v4 helper. Uses `crypto.randomUUID()` when available (browsers,
// modern Node, jsdom 22+), otherwise falls back to a manual v4 built from
// `crypto.getRandomValues`.

export function randomUUID(): string {
  const maybeNative = (globalThis as unknown as { crypto?: Crypto }).crypto;
  if (maybeNative && typeof maybeNative.randomUUID === 'function') {
    return maybeNative.randomUUID();
  }
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex: string[] = [];
  for (let i = 0; i < 16; i++) hex.push(bytes[i].toString(16).padStart(2, '0'));
  return `${hex.slice(0, 4).join('')}-${hex.slice(4, 6).join('')}-${hex.slice(6, 8).join('')}-${hex.slice(8, 10).join('')}-${hex.slice(10, 16).join('')}`;
}
