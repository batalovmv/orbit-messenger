// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import type { SaturnSticker } from '../types';

import * as client from '../client';
import { fetchCustomEmoji } from './symbols';

describe('fetchCustomEmoji', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('loads missing custom emoji documents from the backend and preserves request order', async () => {
    const firstId = '11111111-1111-4111-8111-111111111111';
    const secondId = '22222222-2222-4222-8222-222222222222';

    jest.spyOn(client, 'request').mockResolvedValue([
      {
        id: secondId,
        pack_id: 'pack-2',
        emoji: '🚀',
        file_url: '/media/custom-rocket.tgs',
        file_type: 'tgs',
        position: 1,
        is_custom_emoji: true,
      },
      {
        id: firstId,
        pack_id: 'pack-1',
        emoji: '🛰️',
        file_url: '/media/custom-satellite.webp',
        file_type: 'webp',
        position: 0,
        is_custom_emoji: true,
      },
    ] satisfies SaturnSticker[]);

    const result = await fetchCustomEmoji({ documentId: [firstId, secondId] });

    expect(client.request).toHaveBeenCalledWith('POST', '/stickers/documents', {
      ids: [firstId, secondId],
    });
    expect(result.map(({ id }) => id)).toEqual([firstId, secondId]);
    expect(result.every((sticker) => sticker.isCustomEmoji)).toBe(true);
    expect(result.every((sticker) => sticker.isFree)).toBe(true);
  });
});
