import type { ApiPhoto } from '../../types';

import { getBaseUrl } from '../client';
import { registerAsset } from './symbols';

const DEFAULT_PREVIEW_SIZE = 160;
const DEFAULT_FULL_SIZE = 640;

export function getAvatarPhotoId(peerId: string, avatarUrl?: string) {
  const absoluteUrl = toAbsoluteUrl(avatarUrl);
  if (!absoluteUrl) {
    return undefined;
  }

  const photoId = `avatar-${peerId}-${hashString(absoluteUrl)}`;
  registerAvatarAssets(peerId, photoId, absoluteUrl);

  return photoId;
}

export function buildAvatarPhoto(peerId: string, avatarUrl?: string): ApiPhoto | undefined {
  const photoId = getAvatarPhotoId(peerId, avatarUrl);
  if (!photoId) {
    return undefined;
  }

  return {
    mediaType: 'photo',
    id: photoId,
    date: 0,
    sizes: [
      { width: DEFAULT_PREVIEW_SIZE, height: DEFAULT_PREVIEW_SIZE, type: 's' },
      { width: DEFAULT_PREVIEW_SIZE * 2, height: DEFAULT_PREVIEW_SIZE * 2, type: 'x' },
      { width: DEFAULT_FULL_SIZE, height: DEFAULT_FULL_SIZE, type: 'y' },
    ],
  };
}

function registerAvatarAssets(peerId: string, photoId: string, absoluteUrl: string) {
  const asset = {
    fileName: `${photoId}${guessFileExtension(absoluteUrl)}`,
    fullUrl: absoluteUrl,
    previewUrl: absoluteUrl,
    mimeType: guessMimeType(absoluteUrl),
  };

  registerAsset(peerId, asset, ['avatar', 'profile']);
  registerAsset(photoId, asset, ['document', 'photo']);
}

function toAbsoluteUrl(url?: string) {
  if (!url) return undefined;
  if (/^(?:https?:|data:|blob:)/.test(url)) {
    return url;
  }

  return `${getBaseUrl()}${url}`;
}

function guessMimeType(url: string) {
  if (url.startsWith('data:')) {
    return url.match(/^data:([^;,]+)/)?.[1] || 'image/jpeg';
  }

  const normalized = url.toLowerCase();

  if (normalized.endsWith('.png')) return 'image/png';
  if (normalized.endsWith('.webp')) return 'image/webp';
  if (normalized.endsWith('.gif')) return 'image/gif';
  if (normalized.endsWith('.svg')) return 'image/svg+xml';
  if (normalized.endsWith('.mp4')) return 'video/mp4';

  return 'image/jpeg';
}

function guessFileExtension(url: string) {
  switch (guessMimeType(url)) {
    case 'image/png':
      return '.png';
    case 'image/webp':
      return '.webp';
    case 'image/gif':
      return '.gif';
    case 'image/svg+xml':
      return '.svg';
    case 'video/mp4':
      return '.mp4';
    default:
      return '.jpg';
  }
}

function hashString(value: string) {
  let hash = 2166136261;

  for (let i = 0; i < value.length; i++) {
    hash ^= value.charCodeAt(i);
    hash = Math.imul(hash, 16777619);
  }

  return (hash >>> 0).toString(16);
}
