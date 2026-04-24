// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import type { SaturnLoginResponse, SaturnUser } from '../types';

import * as client from '../client';
import * as apiUpdateEmitter from '../updates/apiUpdateEmitter';
import { checkAuth } from './auth';

const mockUser: SaturnUser = {
  id: 'user-1',
  email: 'admin@orbit.local',
  display_name: 'Admin',
  status: 'online',
  role: 'admin',
  is_active: true,
  created_at: '2026-04-03T00:00:00.000Z',
  updated_at: '2026-04-03T00:00:00.000Z',
};

describe('checkAuth', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('bypasses the auth gate when validating a restored access token', async () => {
    jest.spyOn(client, 'getAccessToken').mockReturnValue('persisted-token');
    const request = jest.spyOn(client, 'request').mockResolvedValue(mockUser);
    jest.spyOn(client, 'connectWs').mockImplementation(jest.fn());
    jest.spyOn(apiUpdateEmitter, 'sendApiUpdate').mockImplementation(jest.fn());

    const result = await checkAuth();

    expect(result).toBe(true);
    expect(request).toHaveBeenCalledWith('GET', '/auth/me', undefined, { skipAuthReady: true });
  });

  it('bypasses the auth gate after a refresh fallback before fetching /auth/me', async () => {
    const refreshed = {
      access_token: 'new-token',
      expires_in: 900,
      user: mockUser,
    } satisfies SaturnLoginResponse;

    jest.spyOn(client, 'getAccessToken').mockReturnValue('expired-token');
    const request = jest.spyOn(client, 'request')
      .mockRejectedValueOnce(new Error('expired'))
      .mockResolvedValueOnce(refreshed)
      .mockResolvedValueOnce(mockUser);
    jest.spyOn(client, 'setAccessToken').mockImplementation(jest.fn());
    jest.spyOn(client, 'connectWs').mockImplementation(jest.fn());
    jest.spyOn(apiUpdateEmitter, 'sendApiUpdate').mockImplementation(jest.fn());

    const result = await checkAuth();

    expect(result).toBe(true);
    expect(request).toHaveBeenNthCalledWith(1, 'GET', '/auth/me', undefined, { skipAuthReady: true });
    expect(request).toHaveBeenNthCalledWith(2, 'POST', '/auth/refresh', undefined, { noAuth: true });
    expect(request).toHaveBeenNthCalledWith(3, 'GET', '/auth/me', undefined, { skipAuthReady: true });
  });
});
