// Lightweight Web Vitals collector — no third-party libs. Captures the
// metrics we actually want for an internal IM PWA: FCP, LCP, INP, CLS,
// TTFB, plus long-task counts and JS heap when supported. One beacon per
// tab visit, posted on `pagehide` / `visibilitychange:hidden` via
// navigator.sendBeacon so the request survives tear-down.

import { IS_ANDROID, IS_IOS, IS_PWA } from './browser/windowEnvironment';

type Vitals = {
  url: string;
  build?: string;
  platform: 'ios' | 'android' | 'desktop' | 'unknown';
  isPWA: boolean;
  effectiveType?: string;
  deviceMemory?: number;
  fcp?: number;
  lcp?: number;
  inp?: number;
  cls?: number;
  ttfb?: number;
  longTasks?: number;
  memoryMb?: number;
  tapRecover?: number;
  tapNative?: number;
};

const RUM_ENDPOINT = '/api/v1/rum';

let installed = false;
let beaconSent = false;

function detectPlatform(): Vitals['platform'] {
  if (IS_IOS) return 'ios';
  if (IS_ANDROID) return 'android';
  if (typeof window !== 'undefined') return 'desktop';
  return 'unknown';
}

export function installWebVitals() {
  if (installed) return;
  if (typeof window === 'undefined' || typeof PerformanceObserver === 'undefined') return;
  installed = true;

  const vitals: Vitals = {
    url: typeof location !== 'undefined' ? location.pathname : '',
    build: typeof APP_VERSION !== 'undefined' ? APP_VERSION : undefined,
    platform: detectPlatform(),
    isPWA: IS_PWA,
  };

  const conn = (navigator as unknown as { connection?: { effectiveType?: string } }).connection;
  if (conn?.effectiveType) vitals.effectiveType = conn.effectiveType;
  const deviceMemory = (navigator as unknown as { deviceMemory?: number }).deviceMemory;
  if (typeof deviceMemory === 'number') vitals.deviceMemory = deviceMemory;

  // TTFB from navigation timing.
  try {
    const nav = performance.getEntriesByType('navigation')[0] as PerformanceNavigationTiming | undefined;
    if (nav && nav.responseStart > 0) {
      vitals.ttfb = nav.responseStart;
    }
  } catch { /* ignore */ }

  let longTasks = 0;
  let clsValue = 0;
  let lastInputDelay = 0;
  let lastLcp = 0;

  // FCP / LCP / INP / CLS / longtask observers — each wrapped because some
  // older Safari builds throw on unsupported entryTypes instead of returning
  // an empty result.
  const observe = (type: string, cb: (entry: PerformanceEntry) => void) => {
    try {
      const obs = new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) cb(entry);
      });
      obs.observe({ type, buffered: true } as PerformanceObserverInit);
      return obs;
    } catch {
      return undefined;
    }
  };

  observe('paint', (entry) => {
    if (entry.name === 'first-contentful-paint') {
      vitals.fcp = entry.startTime;
    }
  });

  observe('largest-contentful-paint', (entry) => {
    // LCP can fire multiple times — keep the last reported value.
    lastLcp = (entry as PerformanceEntry & { renderTime?: number; loadTime?: number }).renderTime
      || (entry as PerformanceEntry & { loadTime?: number }).loadTime
      || entry.startTime;
    vitals.lcp = lastLcp;
  });

  observe('layout-shift', (entry) => {
    const ls = entry as PerformanceEntry & { hadRecentInput?: boolean; value?: number };
    if (!ls.hadRecentInput && typeof ls.value === 'number') {
      clsValue += ls.value;
      vitals.cls = clsValue;
    }
  });

  observe('longtask', () => {
    longTasks++;
    vitals.longTasks = longTasks;
  });

  // INP / FID approximation via `event` timing (where supported).
  observe('event', (entry) => {
    const ev = entry as PerformanceEntry & { duration: number; interactionId?: number };
    if (ev.interactionId && ev.duration > lastInputDelay) {
      lastInputDelay = ev.duration;
      vitals.inp = lastInputDelay;
    }
  });

  // JS heap (Chromium only).
  const memory = (performance as unknown as { memory?: { usedJSHeapSize: number } }).memory;
  if (memory && typeof memory.usedJSHeapSize === 'number') {
    // Snapshot at flush time, not at install.
    Object.defineProperty(vitals, '__memorySnapshot', {
      value: () => Math.round(memory.usedJSHeapSize / (1024 * 1024)),
    });
  }

  const flush = () => {
    if (beaconSent) return;
    beaconSent = true;

    const snap = (vitals as Vitals & { __memorySnapshot?: () => number }).__memorySnapshot;
    if (snap) {
      try {
        vitals.memoryMb = snap();
      } catch {
        // ignore
      }
    }

    const tapStats = (window as unknown as {
      __ORBIT_TAP_STATS__?: () => { recovered: number; native: number };
    }).__ORBIT_TAP_STATS__;
    if (tapStats) {
      try {
        const stats = tapStats();
        vitals.tapRecover = stats.recovered;
        vitals.tapNative = stats.native;
      } catch { /* ignore */ }
    }

    // Drop the helper before serializing.
    delete (vitals as Vitals & { __memorySnapshot?: () => number }).__memorySnapshot;

    const payload = JSON.stringify(vitals);
    try {
      if (navigator.sendBeacon) {
        const blob = new Blob([payload], { type: 'application/json' });
        navigator.sendBeacon(RUM_ENDPOINT, blob);
        return;
      }
    } catch { /* fall through to fetch */ }
    try {
      void fetch(RUM_ENDPOINT, {
        method: 'POST',
        body: payload,
        headers: { 'Content-Type': 'application/json' },
        keepalive: true,
        credentials: 'include',
      });
    } catch { /* best-effort */ }
  };

  // Fire on the earliest reliable signal — pagehide on iOS, visibilitychange
  // elsewhere. Both are racy on tear-down; sendBeacon survives either.
  window.addEventListener('pagehide', flush, { capture: true });
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') flush();
  }, { capture: true });
}
