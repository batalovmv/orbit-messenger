import type { SaturnChatListItem, SaturnStickerPack } from '../types';

import { registerAsset } from '../apiBuilders/symbols';
import * as client from '../client';
import { downloadMedia, fetchAuthorizations, fetchChat } from './index';

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
