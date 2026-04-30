// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import type { ApiChat, ApiPeer, ApiPhoto, ApiUser } from '../../types';
import type { SaturnUser } from '../types';

import { ARCHIVED_FOLDER_ID, DEBUG } from '../../../config';
import { buildAvatarPhoto, getAvatarPhotoId } from '../apiBuilders/avatars';
import { getRegisteredAsset } from '../apiBuilders/symbols';
import { buildApiUser, buildApiUserFullInfo } from '../apiBuilders/users';
import { getBaseUrl, request } from '../client';
import { sendApiUpdate } from '../updates/apiUpdateEmitter';
import { archiveChat, unarchiveChat } from './chats';
import { deleteChatPhoto, updateChatPhoto, uploadMedia } from './media';
import { updateProfile } from './users';

const DEFAULT_PREVIEW_SIZE = 160;
const DEFAULT_FULL_SIZE = 640;

type EditableChatPhoto = string | ApiPhoto | Blob;

type EditChatPhotoArgs = {
  accessHash?: string;
  chatId: string;
  isDeleted?: boolean;
  photo?: EditableChatPhoto;
};

type ToggleChatArchivedArgs = {
  chat?: Pick<ApiChat, 'id'>;
  chatId?: string;
  folderId?: number;
  isArchived?: boolean;
};

type PeerWithAvatar = Pick<ApiPeer, 'id' | 'avatarPhotoId'>;

type MediaUploadResponse = {
  id: string;
  url?: string;
};

export async function editChatPhoto({
  chatId,
  photo,
  isDeleted,
}: EditChatPhotoArgs) {
  if (isDeleted || !photo) {
    await deleteChatPhoto(chatId);
    emitChatPhotoUpdate(chatId);
    return true;
  }

  const avatarUrl = await resolveAvatarUrl(photo);
  if (!avatarUrl) {
    return false;
  }

  await updateChatPhoto(chatId, avatarUrl);
  emitChatPhotoUpdate(chatId, avatarUrl);

  return true;
}

export async function toggleChatArchived({
  chatId,
  isArchived,
  chat,
  folderId,
}: ToggleChatArchivedArgs) {
  const resolvedChatId = chatId || chat?.id;
  if (!resolvedChatId) {
    return false;
  }

  const shouldArchive = isArchived ?? folderId === ARCHIVED_FOLDER_ID;
  if (shouldArchive) {
    await archiveChat({ chatId: resolvedChatId });
  } else {
    await unarchiveChat({ chatId: resolvedChatId });
  }

  return true;
}

export async function uploadProfilePhoto(
  file: Blob,
  isFallback?: boolean,
  isVideo = false,
  videoTs = 0,
  bot?: ApiUser,
) {
  void isFallback;
  void isVideo;
  void videoTs;

  const avatarUrl = await uploadAvatar(file);
  if (!avatarUrl) {
    return undefined;
  }

  if (bot) {
    const user = await request<SaturnUser>('PUT', `/users/${bot.id}`, { avatar_url: avatarUrl });
    const apiUser = buildApiUser(user);

    sendApiUpdate({
      '@type': 'updateUser',
      id: user.id,
      user: apiUser,
      fullInfo: buildApiUserFullInfo(user),
    });

    return apiUser.avatarPhotoId
      ? { photo: buildPhotoFromId(apiUser.avatarPhotoId) }
      : undefined;
  }

  const result = await updateProfile({ avatarUrl });
  const avatarPhotoId = result?.user.avatarPhotoId;

  return avatarPhotoId
    ? { photo: buildPhotoFromId(avatarPhotoId) }
    : undefined;
}

export async function updateProfilePhoto(photo?: ApiPhoto, isFallback?: boolean) {
  void isFallback;

  if (!photo) {
    return undefined;
  }

  const avatarUrl = await resolveAvatarUrl(photo);
  if (!avatarUrl) {
    return undefined;
  }

  const result = await updateProfile({ avatarUrl });
  const avatarPhotoId = result?.user.avatarPhotoId;

  return avatarPhotoId
    ? { photo: buildPhotoFromId(avatarPhotoId) }
    : undefined;
}

export function fetchProfilePhotos({
  peer,
  offset,
  limit,
}: {
  peer: PeerWithAvatar;
  offset?: string | number;
  limit?: number;
}) {
  void offset;
  void limit;

  if (!peer.avatarPhotoId) {
    return Promise.resolve({
      photos: [] as ApiPhoto[],
      count: 0,
      nextOffsetId: undefined,
    });
  }

  return Promise.resolve({
    photos: [buildPhotoFromId(peer.avatarPhotoId)],
    count: 1,
    nextOffsetId: undefined,
  });
}

async function resolveAvatarUrl(photo: EditableChatPhoto) {
  if (typeof photo === 'string') {
    return toAbsoluteUrl(photo);
  }

  if (isBlob(photo)) {
    return uploadAvatar(photo);
  }

  return resolvePhotoUrl(photo);
}

async function uploadAvatar(file: Blob) {
  const mediaType = file.type.startsWith('video/')
    ? 'video'
    : file.type.startsWith('image/')
      ? 'image'
      : undefined;
  const { response } = uploadMedia(file, mediaType);
  const uploaded = await response as MediaUploadResponse;

  return toAbsoluteUrl(uploaded.url) || `${getBaseUrl()}/media/${uploaded.id}`;
}

function resolvePhotoUrl(photo: ApiPhoto) {
  if (photo.blobUrl) {
    return photo.blobUrl;
  }

  const asset = getRegisteredAsset(photo.id, 'photo') || getRegisteredAsset(photo.id, 'document');
  const registeredUrl = asset?.fullUrl || asset?.previewUrl || asset?.thumbnailDataUri;
  if (registeredUrl) {
    return registeredUrl;
  }

  if (/^[0-9a-f-]{36}$/i.test(photo.id)) {
    return `${getBaseUrl()}/media/${photo.id}`;
  }

  if (DEBUG) {
    // eslint-disable-next-line no-console
    console.warn('[Saturn] Unable to resolve profile photo URL for', photo.id);
  }

  return undefined;
}

function emitChatPhotoUpdate(chatId: string, avatarUrl?: string) {
  const avatarPhotoId = avatarUrl ? getAvatarPhotoId(chatId, avatarUrl) : undefined;

  sendApiUpdate({
    '@type': 'updateChat',
    id: chatId,
    chat: { avatarPhotoId },
  });

  sendApiUpdate({
    '@type': 'updateChatFullInfo',
    id: chatId,
    fullInfo: {
      profilePhoto: avatarUrl ? buildAvatarPhoto(chatId, avatarUrl) : undefined,
    },
  });
}

function buildPhotoFromId(photoId: string): ApiPhoto {
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

function isBlob(value: unknown): value is Blob {
  return typeof Blob !== 'undefined' && value instanceof Blob;
}

function toAbsoluteUrl(url?: string) {
  if (!url) {
    return undefined;
  }

  if (/^(?:https?:|data:|blob:)/.test(url)) {
    return url;
  }

  return `${getBaseUrl()}${url}`;
}
