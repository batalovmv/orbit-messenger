import type { SaturnStickerPack } from '../types';

import {
  buildApiSticker,
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

  it('does not register animated sticker files as preview images without an explicit preview URL', () => {
    buildApiSticker({
      id: 'sticker-animated',
      pack_id: 'pack-animated',
      emoji: '🔥',
      file_url: '/media/sticker-animated',
      file_type: 'tgs',
      width: 512,
      height: 512,
      position: 0,
    });

    expect(getRegisteredAsset('sticker-animated', 'sticker')).toEqual(expect.objectContaining({
      fullUrl: expect.stringContaining('/media/sticker-animated'),
      previewUrl: undefined,
      mimeType: 'application/x-tgsticker',
    }));
  });

  it('treats pack thumbnails without format hints as static covers', () => {
    const stickerSet = buildApiStickerSet({
      id: 'pack-static-thumb',
      title: 'Orbit Static Thumb',
      short_name: 'orbit_static_thumb',
      thumbnail_url: '/media/pack-static-thumb/thumbnail',
      is_official: false,
      is_animated: true,
      sticker_count: 1,
      stickers: [{
        id: 'sticker-animated-cover',
        pack_id: 'pack-static-thumb',
        emoji: '✨',
        file_url: '/media/sticker-animated-cover',
        file_type: 'tgs',
        width: 512,
        height: 512,
        position: 0,
      }],
      created_at: '2026-04-03T10:00:00.000Z',
      updated_at: '2026-04-03T10:00:00.000Z',
    } satisfies SaturnStickerPack);

    expect(stickerSet.hasStaticThumb).toBe(true);
    expect(stickerSet.hasAnimatedThumb).toBeUndefined();
    expect(getRegisteredAsset('pack-static-thumb', 'stickerSet')).toEqual(expect.objectContaining({
      mimeType: 'image/webp',
      fullUrl: expect.stringContaining('/media/pack-static-thumb/thumbnail'),
    }));
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

  it('defaults Saturn sticker flags needed by custom emoji rendering', () => {
    const sticker = buildApiSticker({
      id: 'custom-emoji-1',
      pack_id: 'pack-emoji',
      emoji: '🛰️',
      file_url: '/media/custom-emoji-1.webp',
      file_type: 'webp',
      position: 0,
      is_custom_emoji: true,
      should_use_text_color: true,
    });

    expect(sticker.isCustomEmoji).toBe(true);
    expect(sticker.isFree).toBe(true);
    expect(sticker.shouldUseTextColor).toBe(true);
  });
});
