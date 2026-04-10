import type { SaturnChatListItem, SaturnStickerPack } from '../types';

import { buildApiUser } from '../apiBuilders/users';
import { registerAsset } from '../apiBuilders/symbols';
import * as client from '../client';
import {
  downloadMedia,
  fetchAuthorizations,
  fetchAvailableEffects,
  fetchAvailableReactions,
  fetchChat,
  fetchSavedReactionTags,
  updateSavedReactionTag,
} from './index';

describe('downloadMedia', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('hydrates sticker set assets when the hash starts with stickerSet', async () => {
    const stickerSetId = '5f52fd0a-8c59-4f3b-a9b3-6c26e3e95c51';

    jest.spyOn(client, 'ensureAuth').mockResolvedValue('token');
    const request = jest.spyOn(client, 'request').mockResolvedValue({
      id: stickerSetId,
      title: 'Orbit Basics',
      short_name: 'orbit_basics',
      is_official: true,
      is_animated: false,
      sticker_count: 1,
      stickers: [{
        id: 'eaa67fd2-4bd3-4aa0-95f0-2cd7c0fc7d91',
        pack_id: stickerSetId,
        emoji: '😀',
        file_url: [
          'data:image/svg+xml;charset=UTF-8,',
          '%3Csvg%20xmlns%3D%22http%3A%2F%2Fwww.w3.org%2F2000%2Fsvg%22%20',
          'viewBox%3D%220%200%20128%20128%22%3E%3Ctext%20x%3D%2250%25%22%20',
          'y%3D%2255%25%22%20text-anchor%3D%22middle%22%20font-size%3D%2272%22%3E',
          '%F0%9F%98%80%3C%2Ftext%3E%3C%2Fsvg%3E',
        ].join(''),
        file_type: 'webp',
        width: 128,
        height: 128,
        position: 0,
      }],
      is_installed: true,
      created_at: '2026-04-02T12:59:02.391306Z',
      updated_at: '2026-04-02T12:59:02.391306Z',
    } satisfies SaturnStickerPack);

    const result = await downloadMedia({ url: `stickerSet${stickerSetId}` });

    expect(request).toHaveBeenCalledWith('GET', `/stickers/sets/${stickerSetId}`);
    expect(result).toEqual(expect.objectContaining({
      dataBlob: expect.any(Blob),
      mimeType: 'image/svg+xml',
    }));
  });

  it('preserves unicode svg payloads when decoding data URIs', async () => {
    const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="128" height="128"><text x="50%" y="50%">😀</text></svg>';
    const dataUri = `data:image/svg+xml;charset=UTF-8,${encodeURIComponent(svg)}`;

    registerAsset('unicode-emoji-test', {
      fileName: 'unicode-emoji-test.svg',
      fullUrl: dataUri,
      previewUrl: dataUri,
      mimeType: 'image/svg+xml',
    }, ['document']);

    const result = await downloadMedia({ url: 'documentunicode-emoji-test' });

    expect(result).toEqual(expect.objectContaining({
      dataBlob: expect.any(Blob),
      mimeType: 'image/svg+xml',
    }));

    expect(result!.dataBlob!.size).toBe(Buffer.byteLength(svg, 'utf8'));
  });

  it('falls back to stable media endpoints when a registered photo URL is stale', async () => {
    const mediaId = '5f52fd0a-8c59-4f3b-a9b3-6c26e3e95c51';
    const originalFetch = globalThis.fetch;

    client.init('https://orbit.example/api/v1', jest.fn());
    jest.spyOn(client, 'ensureAuth').mockResolvedValue('token');

    registerAsset(mediaId, {
      fileName: 'photo.jpg',
      fullUrl: 'https://r2.example.com/expired.jpg',
      previewUrl: 'https://r2.example.com/expired-thumb.jpg',
      mimeType: 'image/jpeg',
    }, ['photo', 'document']);

    const fetchMock = jest.fn()
      .mockResolvedValueOnce({
        ok: false,
        headers: new Headers(),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        headers: new Headers({ 'content-type': 'image/jpeg' }),
        blob: async () => new Blob(['image-data'], { type: 'image/jpeg' }),
      } as Response);
    Object.defineProperty(globalThis, 'fetch', {
      configurable: true,
      value: fetchMock,
      writable: true,
    });

    try {
      const result = await downloadMedia({ url: `photo${mediaId}` });

      expect(fetchMock).toHaveBeenNthCalledWith(1, 'https://r2.example.com/expired.jpg', {
        headers: {},
        redirect: 'follow',
      });
      expect(fetchMock).toHaveBeenNthCalledWith(2, `https://orbit.example/api/v1/media/${mediaId}`, {
        headers: {
          Authorization: 'Bearer token',
        },
        redirect: 'follow',
      });
      expect(result).toEqual(expect.objectContaining({
        dataBlob: expect.any(Blob),
        mimeType: 'image/jpeg',
      }));
    } finally {
      if (originalFetch) {
        Object.defineProperty(globalThis, 'fetch', {
          configurable: true,
          value: originalFetch,
          writable: true,
        });
      } else {
        Object.defineProperty(globalThis, 'fetch', {
          configurable: true,
          value: undefined,
          writable: true,
        });
      }
    }
  });

  it('resolves avatar hashes through the registered Saturn avatar assets', async () => {
    const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64"></svg>';
    const dataUri = `data:image/svg+xml;charset=UTF-8,${encodeURIComponent(svg)}`;
    const user = buildApiUser({
      id: 'user-42',
      email: 'orbit@example.com',
      display_name: 'Orbit QA',
      avatar_url: dataUri,
      status: 'online',
      role: 'member',
      is_active: true,
      created_at: '2026-04-03T10:00:00.000Z',
      updated_at: '2026-04-03T10:00:00.000Z',
    });

    jest.spyOn(client, 'ensureAuth').mockResolvedValue('token');

    const result = await downloadMedia({
      url: `avatar${user.id}?${user.avatarPhotoId}`,
    });

    expect(result).toEqual(expect.objectContaining({
      dataBlob: expect.any(Blob),
      mimeType: 'image/svg+xml',
    }));
    expect(result!.dataBlob!.size).toBe(Buffer.byteLength(svg, 'utf8'));
  });
});

describe('fetchChat', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('hydrates direct chat fallback title and peer user id from the requested user', async () => {
    jest.spyOn(client, 'request').mockResolvedValue({
      id: 'chat-direct-1',
      type: 'direct',
      name: '',
      description: '',
      is_encrypted: false,
      max_members: 2,
      created_at: '2026-04-03T10:00:00.000Z',
      updated_at: '2026-04-03T10:00:00.000Z',
      default_permissions: 255,
      slow_mode_seconds: 0,
      is_signatures: false,
      member_count: 2,
      unread_count: 0,
    } satisfies SaturnChatListItem);

    const result = await fetchChat({
      type: 'user',
      user: {
        id: 'user-42',
        firstName: 'Orbit',
        lastName: 'QA',
      },
    });

    expect(result?.chat).toEqual(expect.objectContaining({
      id: 'chat-direct-1',
      type: 'chatTypePrivate',
      title: 'Orbit QA',
      peerUserId: 'user-42',
    }));
  });
});

describe('fetchAuthorizations', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('maps Saturn auth sessions into Telegram authorization records', async () => {
    jest.spyOn(client, 'request').mockResolvedValue({
      sessions: [{
        id: 'session-1',
        ip_address: '127.0.0.1',
        user_agent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 '
          + '(KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36',
        created_at: '2026-04-03T12:00:00.000Z',
      }, {
        id: 'session-2',
        ip_address: '10.0.0.2',
        user_agent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 '
          + '(KHTML, like Gecko) Version/17.0 Safari/605.1.15',
        created_at: '2026-04-02T12:00:00.000Z',
      }],
    });

    const result = await fetchAuthorizations();

    expect(result).toEqual({
      ttlDays: 183,
      authorizations: {
        'session-1': expect.objectContaining({
          hash: 'session-1',
          isCurrent: true,
          deviceModel: 'Chrome',
          platform: 'Windows',
          systemVersion: '10.0',
          appName: 'Orbit Web',
          appVersion: '123.0.0.0',
          ip: '127.0.0.1',
        }),
        'session-2': expect.objectContaining({
          hash: 'session-2',
          isCurrent: false,
          deviceModel: 'Safari',
          platform: 'macOS',
          systemVersion: '10.15.7',
          appVersion: '17.0',
          ip: '10.0.0.2',
        }),
      },
    });
  });
});

describe('reaction animation fallbacks', () => {
  it('hydrates available reactions with local animated assets', async () => {
    const reactions = await fetchAvailableReactions();
    const firstReaction = reactions?.[0];

    expect(firstReaction).toEqual(expect.objectContaining({
      centerIcon: expect.objectContaining({
        mimeType: 'application/x-tgsticker',
      }),
      aroundAnimation: expect.objectContaining({
        mimeType: 'application/x-tgsticker',
      }),
      selectAnimation: expect.objectContaining({
        mimeType: 'application/x-tgsticker',
      }),
      appearAnimation: expect.objectContaining({
        mimeType: 'application/x-tgsticker',
      }),
    }));
  });

  it('exposes local effect metadata for composer effects', async () => {
    const result = await fetchAvailableEffects();

    expect(result?.effects[0]).toEqual(expect.objectContaining({
      emoticon: expect.any(String),
      effectAnimationId: expect.any(String),
      effectStickerId: expect.any(String),
    }));
  });
});

describe('saved reaction tags fallback', () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  afterEach(() => {
    window.localStorage.clear();
  });

  it('stores and returns saved reaction tags from local storage', async () => {
    await updateSavedReactionTag({
      reaction: {
        type: 'emoji',
        emoticon: '🔥',
      },
      title: 'Urgent',
    });

    const result = await fetchSavedReactionTags();

    expect(result).toEqual({
      hash: expect.stringContaining('orbit-saved-reaction-tags-v1'),
      tags: [{
        reaction: {
          type: 'emoji',
          emoticon: '🔥',
        },
        title: 'Urgent',
        count: 1,
      }],
    });

    await expect(fetchSavedReactionTags({ hash: result!.hash })).resolves.toBeUndefined();
  });
});
