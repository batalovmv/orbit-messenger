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
  const { TextEncoder, TextDecoder } = require('util');
  globalThis.TextEncoder = TextEncoder;
  globalThis.TextDecoder = TextDecoder;
}

// jsdom ships `globalThis.crypto` with `getRandomValues` but no
// `subtle`. Some jsdom builds also make the `crypto` property a
// non-writable getter, so blanket reassignment via `globalThis.crypto =`
// silently fails — patch `subtle` directly instead.
{
  const { webcrypto } = require('crypto');
  if (typeof globalThis.crypto === 'undefined') {
    globalThis.crypto = webcrypto;
  } else if (typeof globalThis.crypto.subtle === 'undefined') {
    try {
      Object.defineProperty(globalThis.crypto, 'subtle', {
        value: webcrypto.subtle,
        configurable: true,
        writable: true,
      });
    } catch {
      globalThis.crypto.subtle = webcrypto.subtle;
    }
  }
}

// jsdom does not ship structuredClone by default on older versions.
// fake-indexeddb needs it for deep-cloning stored records.
if (typeof globalThis.structuredClone === 'undefined') {
  const util = require('node:util');
  if (typeof util.structuredClone === 'function') {
    globalThis.structuredClone = util.structuredClone;
  } else {
    globalThis.structuredClone = (value) => JSON.parse(JSON.stringify(value));
  }
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
