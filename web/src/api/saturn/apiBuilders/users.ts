import type { ApiUser, ApiUserFullInfo, ApiUserStatus } from '../../types';
import type { SaturnUser } from '../types';

import { buildAvatarPhoto, getAvatarPhotoId } from './avatars';

export function buildApiUser(user: SaturnUser): ApiUser {
  const nameParts = (user.display_name || '').split(' ');
  const firstName = nameParts[0] || '';
  const lastName = nameParts.slice(1).join(' ') || undefined;

  return {
    id: user.id,
    isMin: false,
    type: 'userTypeRegular',
    firstName,
    lastName,
    phoneNumber: user.phone || '',
    avatarPhotoId: getAvatarPhotoId(user.id, user.avatar_url),
    hasUsername: Boolean(user.email),
    usernames: user.email ? [{ username: user.email, isActive: true, isEditable: true }] : undefined,
    customStatus: user.custom_status || undefined,
    customStatusEmoji: user.custom_status_emoji || undefined,
  };
}

export function buildApiUserStatus(user: SaturnUser): ApiUserStatus {
  if (user.status === 'online') {
    return {
      type: 'userStatusOnline',
      expires: Math.floor(Date.now() / 1000) + 300,
    };
  }

  if (user.last_seen_at) {
    return {
      type: 'userStatusOffline',
      wasOnline: Math.floor(new Date(user.last_seen_at).getTime() / 1000),
    };
  }

  if (user.status === 'recently') {
    return { type: 'userStatusRecently' };
  }

  if (user.status === 'offline') {
    return { type: 'userStatusRecently' };
  }

  return { type: 'userStatusEmpty' };
}

export function buildApiUserFullInfo(user: SaturnUser): ApiUserFullInfo {
  return {
    bio: user.bio || undefined,
    profilePhoto: buildAvatarPhoto(user.id, user.avatar_url),
    commonChatsCount: 1, // Hint to show tab; real count loaded by loadCommonChats
  };
}
