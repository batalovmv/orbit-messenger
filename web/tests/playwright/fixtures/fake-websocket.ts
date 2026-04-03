/**
 * FakeWebSocket injected via context.addInitScript().
 * Replaces the global WebSocket constructor so Saturn client's
 * `new WebSocket(url)` in client.ts creates a controllable fake.
 *
 * - Static constants match real WebSocket (client.ts checks WebSocket.OPEN)
 * - send() is a safe no-op for all outbound frames (auth, ping, typing)
 * - close() fires onclose normally (client handles intentional close via flag)
 * - Exposes window.__TEST_WS__ for WS event injection from tests
 * - Sets window.__TEST_WS_READY__ = true after onopen fires
 */
export const FAKE_WEBSOCKET_SCRIPT = `
(function() {
  window.__TEST_WS_SENT__ = [];
  window.__TEST_WS_READY__ = false;

  class FakeWebSocket {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSING = 2;
    static CLOSED = 3;

    constructor(url) {
      this.url = url;
      this.readyState = FakeWebSocket.CONNECTING;
      this.extensions = '';
      this.protocol = '';
      this.bufferedAmount = 0;
      this.binaryType = 'blob';

      this._onopen = null;
      this._onmessage = null;
      this._onclose = null;
      this._onerror = null;

      window.__TEST_WS__ = this;

      // Fire onopen on next microtask — onopen is assigned synchronously
      // after construction in client.ts, so queueMicrotask is safe
      queueMicrotask(() => {
        this.readyState = FakeWebSocket.OPEN;
        if (this._onopen) {
          this._onopen({ type: 'open', target: this });
        }
        window.__TEST_WS_READY__ = true;
      });
    }

    // Getter/setter pattern for robustness
    get onopen() { return this._onopen; }
    set onopen(fn) {
      this._onopen = fn;
      // If already OPEN when handler is attached, fire immediately
      if (this.readyState === FakeWebSocket.OPEN && fn) {
        queueMicrotask(() => {
          fn({ type: 'open', target: this });
          window.__TEST_WS_READY__ = true;
        });
      }
    }

    get onmessage() { return this._onmessage; }
    set onmessage(fn) { this._onmessage = fn; }

    get onclose() { return this._onclose; }
    set onclose(fn) { this._onclose = fn; }

    get onerror() { return this._onerror; }
    set onerror(fn) { this._onerror = fn; }

    send(data) {
      // Safe no-op — swallow auth frame, pings, typing, stop_typing, etc.
      try {
        window.__TEST_WS_SENT__.push(JSON.parse(data));
      } catch (e) {
        window.__TEST_WS_SENT__.push(data);
      }
    }

    close(code, reason) {
      if (this.readyState === FakeWebSocket.CLOSED) return;
      this.readyState = FakeWebSocket.CLOSED;
      // Fire onclose normally — client protects against unintended reconnect
      // via wsIntentionalClose flag (client.ts:213)
      if (this._onclose) {
        this._onclose({ type: 'close', code: code || 1000, reason: reason || '', wasClean: true, target: this });
      }
    }

    addEventListener(type, listener) {
      // Minimal impl — Saturn client uses direct on* assignment, not addEventListener
      if (type === 'open') this.onopen = listener;
      else if (type === 'message') this.onmessage = listener;
      else if (type === 'close') this.onclose = listener;
      else if (type === 'error') this.onerror = listener;
    }

    removeEventListener() {}
    dispatchEvent() { return true; }
  }

  // Replace global WebSocket
  window.WebSocket = FakeWebSocket;
})();
`;
