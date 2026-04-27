import { IS_ANDROID } from './browser/windowEnvironment';

// Recover taps that the Android Chrome / WebView pipeline cancels with
// `pointercancel` after a quick stationary touch — observed when scroll
// momentum interferes or when the OS gesture manager preempts the click.
// Strict gating keeps this safe: we only synthesize a click on a small
// allowlist of opt-in tappable targets, with a tight time + distance window
// and no multi-touch / drag / contenteditable / scrubber involvement.

const TAP_MAX_DURATION_MS = 250;
const TAP_MAX_MOVEMENT_PX = 8;
const SUPPRESS_NATIVE_CLICK_MS = 350;

const RECOVERABLE_SELECTOR = [
  'button',
  '[role="button"]',
  '.Button',
  '.ListItem-button',
  '.MenuItem',
  'a[href]',
].join(',');

const EXCLUDED_SELECTOR = [
  'input',
  'textarea',
  '[contenteditable="true"]',
  '[contenteditable=""]',
  '[data-no-tap-recovery]',
  '.SeekLine',
  '.Audio',
  '.MediaViewerSlides',
  '.Draggable',
  '.RotationSlider',
  '[role="slider"]',
  '[role="scrollbar"]',
].join(',');

type ActiveTouch = {
  pointerId: number;
  startX: number;
  startY: number;
  startTime: number;
  target: Element | null;
  totalMovement: number;
};

let installed = false;

export function installAndroidTapRecovery() {
  if (installed) return;
  if (typeof window === 'undefined' || !IS_ANDROID) return;
  installed = true;

  let active: ActiveTouch | undefined;
  let suppressClicksUntil = 0;
  let recoveredCount = 0;
  let nativeCount = 0;

  const reset = () => {
    active = undefined;
  };

  const isRecoverable = (target: Element | null): target is HTMLElement => {
    if (!target || !(target instanceof Element)) return false;
    if (target.closest(EXCLUDED_SELECTOR)) return false;
    const recoverable = target.closest(RECOVERABLE_SELECTOR);
    if (!recoverable || !(recoverable instanceof HTMLElement)) return false;
    if (recoverable.hasAttribute('disabled') || recoverable.getAttribute('aria-disabled') === 'true') return false;
    if (!recoverable.isConnected) return false;
    return true;
  };

  window.addEventListener('pointerdown', (e: PointerEvent) => {
    if (e.pointerType !== 'touch' || !e.isPrimary) {
      reset();
      return;
    }
    if (!isRecoverable(e.target as Element)) {
      reset();
      return;
    }
    active = {
      pointerId: e.pointerId,
      startX: e.clientX,
      startY: e.clientY,
      startTime: e.timeStamp || Date.now(),
      target: e.target as Element,
      totalMovement: 0,
    };
  }, { capture: true, passive: true });

  window.addEventListener('pointermove', (e: PointerEvent) => {
    if (!active || e.pointerId !== active.pointerId) return;
    const dx = e.clientX - active.startX;
    const dy = e.clientY - active.startY;
    active.totalMovement = Math.max(active.totalMovement, Math.hypot(dx, dy));
    if (active.totalMovement > TAP_MAX_MOVEMENT_PX) reset();
  }, { capture: true, passive: true });

  window.addEventListener('pointercancel', (e: PointerEvent) => {
    if (!active || e.pointerId !== active.pointerId) {
      reset();
      return;
    }
    const duration = (e.timeStamp || Date.now()) - active.startTime;
    if (duration > TAP_MAX_DURATION_MS) {
      reset();
      return;
    }
    if (active.totalMovement > TAP_MAX_MOVEMENT_PX) {
      reset();
      return;
    }

    // Re-check the element under the finger at cancel time. The original
    // target may have been unmounted (virtualised list), in which case look
    // up the current element at the same coordinates and verify it shares the
    // recoverable signature.
    let target = active.target;
    if (!target || !(target).isConnected) {
      const current = document.elementFromPoint(e.clientX, e.clientY);
      target = current;
    }
    if (!isRecoverable(target as Element)) {
      reset();
      return;
    }
    const tappable = (target as Element).closest<HTMLElement>(RECOVERABLE_SELECTOR);
    if (!tappable) {
      reset();
      return;
    }

    suppressClicksUntil = Date.now() + SUPPRESS_NATIVE_CLICK_MS;
    try {
      tappable.click();
      recoveredCount++;

      if ((window as any).__ORBIT_TAP_RECOVERY_LOG__) {
        // eslint-disable-next-line no-console
        console.warn('[tap-recovery] synthesized click on', tappable, { duration, movement: active.totalMovement });
      }
    } catch {
      // ignore
    }
    reset();
  }, { capture: true, passive: true });

  window.addEventListener('pointerup', () => {
    reset();
  }, { capture: true, passive: true });

  // Suppress only the *trusted* native click that Android may still deliver
  // after a recovered cancel — never our own synthetic click (`isTrusted ===
  // false`), otherwise we cancel the very click we just synthesized and the
  // recovery silently no-ops.
  window.addEventListener('click', (e) => {
    if (!e.isTrusted) return;
    if (Date.now() < suppressClicksUntil) {
      e.stopPropagation();
      e.preventDefault();
      suppressClicksUntil = 0;
      return;
    }
    nativeCount++;
  }, { capture: true });

  // Expose counters for the RUM endpoint to read.
  (window as any).__ORBIT_TAP_STATS__ = () => ({ recovered: recoveredCount, native: nativeCount });
}
