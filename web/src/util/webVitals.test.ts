import * as saturnClient from '../api/saturn/client';
import { installWebVitals, resetWebVitalsForTest } from './webVitals';

class TestPerformanceObserver {
  constructor(_callback: PerformanceObserverCallback) {}

  observe(_options: PerformanceObserverInit) {}

  disconnect() {}

  takeRecords() {
    return [];
  }
}

describe('installWebVitals', () => {
  const originalPerformanceObserver = window.PerformanceObserver;
  const originalFetch = window.fetch;

  beforeEach(() => {
    jest.spyOn(saturnClient, 'getBaseUrl').mockReturnValue('https://orbit.example/api/v1');
    jest.spyOn(saturnClient, 'getSessionId').mockReturnValue('session-1');
    Object.defineProperty(window, 'PerformanceObserver', {
      configurable: true,
      value: TestPerformanceObserver,
    });
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      value: 'hidden',
    });
    window.fetch = jest.fn().mockResolvedValue({ status: 204, ok: true } as Response);
    resetWebVitalsForTest();
  });

  afterEach(() => {
    resetWebVitalsForTest();
    jest.restoreAllMocks();
    Object.defineProperty(window, 'PerformanceObserver', {
      configurable: true,
      value: originalPerformanceObserver,
    });
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      value: 'visible',
    });
    window.fetch = originalFetch;
  });

  it('sends RUM with the current access token', () => {
    jest.spyOn(saturnClient, 'getAccessToken').mockReturnValue('access-token');

    installWebVitals();
    document.dispatchEvent(new Event('visibilitychange'));

    expect(window.fetch).toHaveBeenCalledWith('https://orbit.example/api/v1/rum', expect.objectContaining({
      method: 'POST',
      keepalive: true,
      credentials: 'include',
      headers: expect.objectContaining({
        Authorization: 'Bearer access-token',
        'X-Session-ID': 'session-1',
      }),
    }));
  });

  it('does not send an anonymous RUM request', () => {
    jest.spyOn(saturnClient, 'getAccessToken').mockReturnValue(undefined);

    installWebVitals();
    document.dispatchEvent(new Event('visibilitychange'));

    expect(window.fetch).not.toHaveBeenCalled();
  });
});
