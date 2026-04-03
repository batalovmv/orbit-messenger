import * as saturnClient from '../client';
import { destroy } from './client';

describe('destroy', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('keeps persisted auth during temporary teardown flows', () => {
    const disconnectWs = jest.spyOn(saturnClient, 'disconnectWs').mockImplementation(jest.fn());
    const clearAuth = jest.spyOn(saturnClient, 'clearAuth').mockImplementation(jest.fn());

    destroy(true, true);

    expect(disconnectWs).toHaveBeenCalledTimes(1);
    expect(clearAuth).not.toHaveBeenCalled();
  });

  it('clears auth on full destroy during sign-out', () => {
    const disconnectWs = jest.spyOn(saturnClient, 'disconnectWs').mockImplementation(jest.fn());
    const clearAuth = jest.spyOn(saturnClient, 'clearAuth').mockImplementation(jest.fn());

    destroy();

    expect(disconnectWs).toHaveBeenCalledTimes(1);
    expect(clearAuth).toHaveBeenCalledTimes(1);
  });
});
