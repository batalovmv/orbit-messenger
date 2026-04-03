import type {
  ApiDocument,
  ApiPhotoSize,
  ApiSticker,
  ApiStickerSet,
  ApiStickerSetInfo,
  ApiThumbnail,
  ApiVideo,
} from '../../types';
import type {
  SaturnSavedGIF,
  SaturnSticker,
  SaturnStickerPack,
  SaturnTenorGIF,
} from '../types';

import { getEmojiImagePath } from '../../../util/emoji/emoji';
import { getBaseUrl } from '../client';

type AssetKind = 'avatar' | 'document' | 'photo' | 'profile' | 'sticker' | 'stickerSet';

export type RegisteredAsset = {
  fileName?: string;
  fullUrl?: string;
  mimeType?: string;
  previewUrl?: string;
  thumbnailDataUri?: string;
};

type SerializedRichMessageContent = SerializedStickerMessage | SerializedGifMessage;

type SerializedStickerMessage = {
  orbit_rich: true;
  kind: 'sticker';
  sticker: {
    emoji?: string;
    height?: number;
    id: string;
    is_custom_emoji?: boolean;
    is_lottie: boolean;
    is_video: boolean;
    preview_url?: string;
    set_id?: string;
    set_short_name?: string;
    url?: string;
    width?: number;
  };
};

type SerializedGifMessage = {
  orbit_rich: true;
  kind: 'gif';
  gif: {
    duration?: number;
    file_name?: string;
    height?: number;
    id: string;
    mime_type?: string;
    preview_url?: string;
    size?: number;
    url?: string;
    width?: number;
  };
};

const assetRegistry = new Map<string, RegisteredAsset>();

function toAssetKey(kind: AssetKind, id: string) {
  return `${kind}:${id}`;
}

function toAbsoluteUrl(url?: string) {
  if (!url) return undefined;
  if (/^(?:https?:|data:|blob:)/.test(url)) {
    return url;
  }
  return `${getBaseUrl()}${url}`;
}

function makeSvgDataUri(text: string, background = '#ffffff', foreground = '#111827') {
  const svg = [
    '<svg xmlns="http://www.w3.org/2000/svg" width="128" height="128" viewBox="0 0 128 128">',
    `<rect width="128" height="128" rx="32" fill="${background}"/>`,
    `<text x="50%" y="54%" dominant-baseline="middle" text-anchor="middle" font-size="72">${escapeXml(text)}</text>`,
    '</svg>',
  ].join('');
  return `data:image/svg+xml;charset=UTF-8,${encodeURIComponent(svg)}`;
}

function escapeXml(value: string) {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll('\'', '&apos;');
}

function buildThumbnail(dataUri?: string, width = 128, height = 128): ApiThumbnail | undefined {
  if (!dataUri) return undefined;
  return { dataUri, width, height };
}

function buildPreviewSizes(width?: number, height?: number): ApiPhotoSize[] | undefined {
  if (!width || !height) return undefined;

  return [
    { width, height, type: 's' },
    { width, height, type: 'x' },
  ];
}

function buildStickerSetInfo(
  setID?: string,
  setShortName?: string,
): ApiStickerSetInfo {
  if (setID) {
    return { id: setID, accessHash: '0' };
  }

  if (setShortName) {
    return { shortName: setShortName };
  }

  return { isMissing: true };
}

function buildStickerTitleDataUri(sticker: Pick<SaturnSticker, 'emoji'>) {
  return makeSvgDataUri(sticker.emoji || '🙂', 'transparent');
}

type StickerAssetFormat = 'static' | 'animated' | 'video';

function normalizeStickerFormatHint(value?: string) {
  if (!value) {
    return undefined;
  }

  switch (value.toLowerCase()) {
    case 'tgs':
      return 'animated' as const;
    case 'webm':
      return 'video' as const;
    case 'webp':
    case 'svg':
    case 'png':
    case 'jpg':
    case 'jpeg':
      return 'static' as const;
    default:
      return undefined;
  }
}

function getStickerFormatHintFromUrl(url?: string) {
  if (!url) {
    return undefined;
  }

  const directMatch = url.match(/[?#&]orbit-format=(tgs|webm|webp|svg|png|jpe?g)\b/i);
  if (directMatch) {
    return normalizeStickerFormatHint(directMatch[1]);
  }

  try {
    const parsed = new URL(url, 'https://orbit.local');
    const queryHint = parsed.searchParams.get('orbit-format');
    const hashHint = parsed.hash.match(/orbit-format=(tgs|webm|webp|svg|png|jpe?g)\b/i)?.[1];
    const hint = normalizeStickerFormatHint(queryHint || hashHint || undefined);
    if (hint) {
      return hint;
    }

    return normalizeStickerFormatHint(parsed.pathname.match(/\.([a-z0-9]+)$/i)?.[1]);
  } catch {
    return normalizeStickerFormatHint(url.match(/\.([a-z0-9]+)(?:$|[?#])/i)?.[1]);
  }
}

function getStickerAssetFormatFromType(fileType?: SaturnSticker['file_type']) {
  switch (fileType) {
    case 'tgs':
      return 'animated';
    case 'webm':
      return 'video';
    case 'webp':
    case 'svg':
      return 'static';
    default:
      return undefined;
  }
}

function getMimeTypeForStickerFormat(format?: StickerAssetFormat, url?: string) {
  switch (format) {
    case 'animated':
      return 'application/x-tgsticker';
    case 'video':
      return 'video/webm';
    case 'static':
      if (url?.match(/\.png(?:$|[?#])/i)) {
        return 'image/png';
      }
      if (url?.match(/\.jpe?g(?:$|[?#])/i)) {
        return 'image/jpeg';
      }
      if (url?.match(/\.svg(?:$|[?#])/i)) {
        return 'image/svg+xml';
      }
      return 'image/webp';
    default:
      return undefined;
  }
}

function getStickerPreviewUrl(
  sticker: Pick<SaturnSticker, 'file_type' | 'preview_url' | 'thumbnail_url'>,
  fullUrl?: string,
) {
  const explicitPreviewUrl = toAbsoluteUrl(sticker.preview_url || sticker.thumbnail_url);
  if (explicitPreviewUrl) {
    return explicitPreviewUrl;
  }

  return sticker.file_type === 'webp' || sticker.file_type === 'svg'
    ? fullUrl
    : undefined;
}

function registerStickerAsset(
  id: string,
  fileType: SaturnSticker['file_type'],
  fullUrl?: string,
  previewUrl?: string,
  thumbnailDataUri?: string,
  kinds: readonly AssetKind[] = ['document', 'sticker'],
) {
  registerAsset(id, {
    fileName: `${id}.${fileType}`,
    fullUrl,
    mimeType: fileType === 'tgs'
      ? 'application/x-tgsticker'
      : fileType === 'webm'
        ? 'video/webm'
        : fileType === 'svg'
          ? 'image/svg+xml'
          : 'image/webp',
    previewUrl,
    thumbnailDataUri,
  }, kinds);
}

function getStickerSetCoverAsset(pack: SaturnStickerPack) {
  const thumbnailUrl = toAbsoluteUrl(pack.thumbnail_url);
  const thumbnailFormat = getStickerFormatHintFromUrl(pack.thumbnail_url);
  const format = thumbnailUrl
    ? (thumbnailFormat || 'static')
    : getStickerAssetFormatFromType(pack.stickers?.[0]?.file_type);

  if (thumbnailUrl) {
    return {
      url: thumbnailUrl,
      format,
      mimeType: getMimeTypeForStickerFormat(format, pack.thumbnail_url),
    };
  }

  if (!pack.stickers?.length) {
    return undefined;
  }

  const [firstSticker] = pack.stickers;
  const stickerAsset = getRegisteredAsset(firstSticker.id, 'sticker')
    || getRegisteredAsset(firstSticker.id, 'document');
  const stickerFormat = getStickerAssetFormatFromType(firstSticker.file_type);
  const stickerUrl = stickerAsset?.fullUrl || toAbsoluteUrl(firstSticker.file_url);
  if (!stickerUrl) {
    return undefined;
  }

  return {
    url: stickerUrl,
    format: stickerFormat,
    mimeType: stickerAsset?.mimeType || getMimeTypeForStickerFormat(stickerFormat, firstSticker.file_url),
  };
}

function groupStickerPacks(stickers: ApiSticker[]) {
  return stickers.reduce<Record<string, ApiSticker[]>>((acc, sticker) => {
    if (!sticker.emoji) return acc;
    if (!acc[sticker.emoji]) {
      acc[sticker.emoji] = [];
    }
    acc[sticker.emoji].push(sticker);
    return acc;
  }, {});
}

export function registerAsset(id: string, asset: RegisteredAsset, kinds: readonly AssetKind[] = ['document']) {
  kinds.forEach((kind) => {
    assetRegistry.set(toAssetKey(kind, id), asset);
  });
}

export function getRegisteredAsset(id: string, kind: AssetKind = 'document') {
  return assetRegistry.get(toAssetKey(kind, id));
}

export function buildStaticAssetDocument(id: string, emoji: string, title?: string): ApiDocument {
  const previewUrl = getEmojiImagePath(emoji, 'small');
  const fullUrl = getEmojiImagePath(emoji, 'big');

  if (previewUrl || fullUrl) {
    const resolvedPreviewUrl = previewUrl || fullUrl!;
    const resolvedFullUrl = fullUrl || resolvedPreviewUrl;

    registerAsset(id, {
      fileName: `${title || 'emoji'}.png`,
      fullUrl: resolvedFullUrl,
      mimeType: 'image/png',
      previewUrl: resolvedPreviewUrl,
      thumbnailDataUri: resolvedPreviewUrl,
    }, ['document']);

    return {
      mediaType: 'document',
      id,
      fileName: `${title || 'emoji'}.png`,
      mimeType: 'image/png',
      size: resolvedFullUrl.length,
      thumbnail: buildThumbnail(resolvedPreviewUrl),
    };
  }

  const dataUri = makeSvgDataUri(emoji, 'transparent');

  registerAsset(id, {
    fileName: `${title || 'emoji'}.svg`,
    fullUrl: dataUri,
    mimeType: 'image/svg+xml',
    previewUrl: dataUri,
    thumbnailDataUri: dataUri,
  }, ['document']);

  return {
    mediaType: 'document',
    id,
    fileName: `${title || 'emoji'}.svg`,
    mimeType: 'image/svg+xml',
    size: dataUri.length,
    thumbnail: buildThumbnail(dataUri),
  };
}

export function buildApiSticker(
  sticker: SaturnSticker,
  pack?: Pick<SaturnStickerPack, 'id' | 'short_name'>,
  options?: { isCustomEmoji?: boolean; isFree?: boolean; shouldUseTextColor?: boolean },
): ApiSticker {
  const fullUrl = toAbsoluteUrl(sticker.file_url);
  const previewUrl = getStickerPreviewUrl(sticker, fullUrl);
  const thumbnailDataUri = buildStickerTitleDataUri(sticker);
  const isCustomEmoji = options?.isCustomEmoji ?? sticker.is_custom_emoji;
  const isFree = options?.isFree ?? sticker.is_free ?? true;
  const shouldUseTextColor = options?.shouldUseTextColor ?? sticker.should_use_text_color;

  registerStickerAsset(sticker.id, sticker.file_type, fullUrl, previewUrl, thumbnailDataUri);

  return {
    mediaType: 'sticker',
    id: sticker.id,
    stickerSetInfo: buildStickerSetInfo(pack?.id || sticker.pack_id, pack?.short_name),
    emoji: sticker.emoji,
    isCustomEmoji: isCustomEmoji || undefined,
    isLottie: sticker.file_type === 'tgs',
    isVideo: sticker.file_type === 'webm',
    width: sticker.width || undefined,
    height: sticker.height || undefined,
    thumbnail: buildThumbnail(thumbnailDataUri, sticker.width || 128, sticker.height || 128),
    previewPhotoSizes: buildPreviewSizes(sticker.width || undefined, sticker.height || undefined),
    isFree,
    shouldUseTextColor: shouldUseTextColor || undefined,
  };
}

export function buildApiStickerSet(
  pack: SaturnStickerPack,
  options?: { isEmoji?: boolean; isCustomEmoji?: boolean },
): ApiStickerSet {
  const stickers = pack.stickers?.map((sticker) => buildApiSticker(sticker, pack, {
    isCustomEmoji: options?.isCustomEmoji || options?.isEmoji,
  }));
  const coverAsset = getStickerSetCoverAsset(pack);
  const hasStaticThumb = coverAsset?.format === 'static';
  const hasAnimatedThumb = coverAsset?.format === 'animated';
  const hasVideoThumb = coverAsset?.format === 'video';

  if (coverAsset?.url) {
    registerAsset(pack.id, {
      fileName: `${pack.short_name || pack.id}.${coverAsset.format === 'animated'
        ? 'tgs'
        : coverAsset.format === 'video'
          ? 'webm'
          : 'webp'}`,
      fullUrl: coverAsset.url,
      mimeType: coverAsset.mimeType,
      previewUrl: coverAsset.url,
    }, ['stickerSet']);
  }

  return {
    id: pack.id,
    accessHash: '0',
    title: pack.title,
    shortName: pack.short_name,
    count: pack.sticker_count,
    installedDate: pack.is_installed ? Math.floor(new Date(pack.updated_at).getTime() / 1000) : undefined,
    isArchived: pack.is_installed ? undefined : true,
    isEmoji: options?.isEmoji || undefined,
    hasThumbnail: Boolean(coverAsset?.url),
    hasStaticThumb: hasStaticThumb || undefined,
    hasAnimatedThumb: hasAnimatedThumb || undefined,
    hasVideoThumb: hasVideoThumb || undefined,
    stickers,
    packs: stickers ? groupStickerPacks(stickers) : undefined,
    covers: stickers?.length ? stickers.slice(0, 5) : undefined,
  };
}

export function buildApiGif(gif: SaturnSavedGIF | SaturnTenorGIF): ApiVideo {
  const isSavedGif = 'id' in gif;
  const id = isSavedGif ? gif.id : `tenor_${gif.tenor_id}`;
  const fullUrl = toAbsoluteUrl(gif.url);
  const previewUrl = toAbsoluteUrl(gif.preview_url);
  const thumb = previewUrl || makeSvgDataUri('GIF', '#dff4ff', '#0f4c81');

  registerAsset(id, {
    fileName: `${id}.mp4`,
    fullUrl,
    mimeType: 'video/mp4',
    previewUrl,
    thumbnailDataUri: thumb,
  }, ['document']);

  return {
    mediaType: 'video',
    id,
    mimeType: 'video/mp4',
    duration: 0,
    fileName: `${id}.mp4`,
    width: gif.width || undefined,
    height: gif.height || undefined,
    size: 0,
    isGif: true,
    thumbnail: buildThumbnail(thumb, gif.width || 320, gif.height || 320),
    previewPhotoSizes: buildPreviewSizes(gif.width || undefined, gif.height || undefined),
  };
}

export function serializeStickerForMessage(sticker: ApiSticker) {
  const asset = getRegisteredAsset(sticker.id, 'document');

  return JSON.stringify({
    orbit_rich: true,
    kind: 'sticker',
    sticker: {
      id: sticker.id,
      set_id: 'id' in sticker.stickerSetInfo ? sticker.stickerSetInfo.id : undefined,
      set_short_name: 'shortName' in sticker.stickerSetInfo ? sticker.stickerSetInfo.shortName : undefined,
      emoji: sticker.emoji,
      is_custom_emoji: sticker.isCustomEmoji,
      is_lottie: sticker.isLottie,
      is_video: sticker.isVideo,
      width: sticker.width,
      height: sticker.height,
      url: asset?.fullUrl,
      preview_url: asset?.previewUrl,
    },
  } satisfies SerializedStickerMessage);
}

export function serializeGifForMessage(gif: ApiVideo) {
  const asset = getRegisteredAsset(gif.id, 'document');

  return JSON.stringify({
    orbit_rich: true,
    kind: 'gif',
    gif: {
      id: gif.id,
      width: gif.width,
      height: gif.height,
      url: asset?.fullUrl,
      preview_url: asset?.previewUrl,
      mime_type: gif.mimeType,
      file_name: gif.fileName,
      size: gif.size,
      duration: gif.duration,
    },
  } satisfies SerializedGifMessage);
}

export function parseRichMessageContent(raw?: string): SerializedRichMessageContent | undefined {
  if (!raw) return undefined;

  try {
    const parsed = JSON.parse(raw) as Partial<SerializedRichMessageContent>;
    if (!parsed || parsed.orbit_rich !== true) {
      return undefined;
    }
    if (parsed.kind !== 'sticker' && parsed.kind !== 'gif') {
      return undefined;
    }
    return parsed as SerializedRichMessageContent;
  } catch {
    return undefined;
  }
}

export function buildStickerFromSerializedMessage(payload: SerializedStickerMessage['sticker']): ApiSticker {
  const previewUrl = toAbsoluteUrl(payload.preview_url || (
    !payload.is_lottie && !payload.is_video ? payload.url : undefined
  ));
  const thumbnailDataUri = makeSvgDataUri(payload.emoji || '🙂', 'transparent');
  const fileType = payload.is_lottie ? 'tgs' : payload.is_video ? 'webm' : 'webp';

  registerStickerAsset(
    payload.id,
    fileType,
    toAbsoluteUrl(payload.url),
    previewUrl,
    thumbnailDataUri,
  );

  return {
    mediaType: 'sticker',
    id: payload.id,
    stickerSetInfo: buildStickerSetInfo(payload.set_id, payload.set_short_name),
    emoji: payload.emoji,
    isCustomEmoji: payload.is_custom_emoji || undefined,
    isLottie: payload.is_lottie,
    isVideo: payload.is_video,
    width: payload.width || undefined,
    height: payload.height || undefined,
    thumbnail: buildThumbnail(thumbnailDataUri, payload.width || 128, payload.height || 128),
    previewPhotoSizes: buildPreviewSizes(payload.width, payload.height),
    isFree: true,
  };
}

export function buildGifFromSerializedMessage(payload: SerializedGifMessage['gif']): ApiVideo {
  const thumbnailDataUri = toAbsoluteUrl(payload.preview_url) || makeSvgDataUri('GIF', '#dff4ff', '#0f4c81');

  registerAsset(payload.id, {
    fileName: payload.file_name || `${payload.id}.mp4`,
    fullUrl: toAbsoluteUrl(payload.url),
    mimeType: payload.mime_type || 'video/mp4',
    previewUrl: toAbsoluteUrl(payload.preview_url || payload.url),
    thumbnailDataUri,
  }, ['document']);

  return {
    mediaType: 'video',
    id: payload.id,
    mimeType: payload.mime_type || 'video/mp4',
    duration: payload.duration || 0,
    fileName: payload.file_name || `${payload.id}.mp4`,
    width: payload.width || undefined,
    height: payload.height || undefined,
    size: payload.size || 0,
    isGif: true,
    thumbnail: buildThumbnail(thumbnailDataUri, payload.width || 320, payload.height || 320),
    previewPhotoSizes: buildPreviewSizes(payload.width, payload.height),
  };
}
