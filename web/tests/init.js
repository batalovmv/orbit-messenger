if (typeof window !== 'undefined' && !window.matchMedia) {
  window.matchMedia = (query) => ({
    matches: false,
    media: query,
    onchange: undefined,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
    dispatchEvent() { return false; },
  });
}

if (typeof window !== 'undefined' && !window.scrollTo) {
  window.scrollTo = () => {};
}

if (typeof HTMLCanvasElement !== 'undefined') {
  HTMLCanvasElement.prototype.getContext = () => ({});
}

if (typeof globalThis.CSS === 'undefined') {
  globalThis.CSS = {
    supports() {
      return false;
    },
  };
}

// Node's TextEncoder / TextDecoder are not exposed in jsdom's global by
// default. @noble/* primitives touch them during module init, so we must
// polyfill them before any crypto modules are imported.
if (typeof globalThis.TextEncoder === 'undefined') {
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const { TextEncoder, TextDecoder } = require('util');
  globalThis.TextEncoder = TextEncoder;
  globalThis.TextDecoder = TextDecoder;
}

// jsdom older versions ship without crypto.subtle. Use Node's webcrypto.
if (typeof globalThis.crypto === 'undefined' || typeof globalThis.crypto.subtle === 'undefined') {
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const { webcrypto } = require('crypto');
  globalThis.crypto = webcrypto;
}

if (typeof globalThis.BroadcastChannel === 'undefined') {
  globalThis.BroadcastChannel = class BroadcastChannel {
    constructor() {}
    postMessage() {}
    close() {}
    addEventListener() {}
    removeEventListener() {}
  };
}
