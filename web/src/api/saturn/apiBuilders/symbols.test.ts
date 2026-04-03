import type { SaturnStickerPack } from '../types';

import {
  buildApiStickerSet,
  buildStickerFromSerializedMessage,
  getRegisteredAsset,
} from './symbols';

describe('sticker builders', () => {
  it('maps animated sticker set covers from orbit format hints', () => {
    const stickerSet = buildApiStickerSet({
      id: 'pack-animated',
      title: 'Orbit Animated',
      short_name: 'orbit_animated',
      is_official: false,
      is_animated: true,
      sticker_count: 12,
      thumbnail_url: '/media/cover-animated#orbit-format=tgs',
      created_at: '2026-04-03T10:00:00.000Z',
      updated_at: '2026-04-03T10:00:00.000Z',
    } satisfies SaturnStickerPack);

    expect(stickerSet.hasThumbnail).toBe(true);
    expect(stickerSet.hasAnimatedThumb).toBe(true);
    expect(stickerSet.hasVideoThumb).toBeUndefined();
    expect(stickerSet.hasStaticThumb).toBeUndefined();
    expect(getRegisteredAsset('pack-animated', 'stickerSet')).toEqual(expect.objectContaining({
      mimeType: 'application/x-tgsticker',
      fullUrl: expect.stringContaining('/media/cover-animated#orbit-format=tgs'),
    }));
  });

  it('falls back to lazy first-sticker cover loading when a pack has no thumbnail', () => {
    const stickerSet = buildApiStickerSet({
      id: 'pack-empty-cover',
      title: 'Orbit Empty',
      short_name: 'orbit_empty',
      is_official: false,
      is_animated: false,
      sticker_count: 4,
      created_at: '2026-04-03T10:00:00.000Z',
      updated_at: '2026-04-03T10:00:00.000Z',
    } satisfies SaturnStickerPack);

    expect(stickerSet.hasThumbnail).toBe(false);
    expect(stickerSet.hasAnimatedThumb).toBeUndefined();
    expect(stickerSet.hasVideoThumb).toBeUndefined();
    expect(stickerSet.hasStaticThumb).toBeUndefined();
    expect(getRegisteredAsset('pack-empty-cover', 'stickerSet')).toBeUndefined();
  });

  it('registers serialized sticker assets with the correct mime type', () => {
    buildStickerFromSerializedMessage({
      id: 'sticker-video',
      is_lottie: false,
      is_video: true,
      url: '/media/sticker-video',
      preview_url: '/media/sticker-video/thumbnail',
      width: 512,
      height: 512,
    });

    expect(getRegisteredAsset('sticker-video', 'sticker')).toEqual(expect.objectContaining({
      mimeType: 'video/webm',
      fullUrl: expect.stringContaining('/media/sticker-video'),
      previewUrl: expect.stringContaining('/media/sticker-video/thumbnail'),
    }));
  });
});
