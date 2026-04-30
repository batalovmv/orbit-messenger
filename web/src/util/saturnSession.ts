export const SATURN_HAS_SESSION_KEY = 'saturn_has_session';

export function hasSaturnSessionHint(): boolean {
  try {
    return localStorage.getItem(SATURN_HAS_SESSION_KEY) === '1';
  } catch {
    return false;
  }
}

export function setSaturnSessionHint() {
  try {
    localStorage.setItem(SATURN_HAS_SESSION_KEY, '1');
  } catch {
    // Ignore storage failures.
  }
}

export function clearSaturnSessionHint() {
  try {
    localStorage.removeItem(SATURN_HAS_SESSION_KEY);
  } catch {
    // Ignore storage failures.
  }
}

export function isOfflineNetworkError(error?: unknown): boolean {
  if (typeof navigator !== 'undefined' && !navigator.onLine) {
    return true;
  }

  return error instanceof TypeError;
}
