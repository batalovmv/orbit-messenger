import type { ApiChat, ApiChatFullInfo, ApiChatMember } from '../../types';
import type {
  SaturnChat,
  SaturnChatAvailableReactions,
  SaturnChatListItem,
  SaturnChatMember,
} from '../types';

import { ARCHIVED_FOLDER_ID } from '../../../config';
import { registerChatId } from '../../../util/entities/ids';
import { buildAvatarPhoto, getAvatarPhotoId } from './avatars';
import { buildApiChatReactions } from './reactions';

const PERM = {
  sendMessages: 1 << 0,
  sendMedia: 1 << 1,
  addMembers: 1 << 2,
  pinMessages: 1 << 3,
  changeInfo: 1 << 4,
  deleteMessages: 1 << 5,
  banUsers: 1 << 6,
  inviteViaLink: 1 << 7,
};

function decodeAdminRights(mask: number, isOwner: boolean): any {
  return {
    changeInfo: Boolean(mask & PERM.changeInfo) || undefined,
    postMessages: Boolean(mask & PERM.sendMessages) || undefined,
    deleteMessages: Boolean(mask & PERM.deleteMessages) || undefined,
    banUsers: Boolean(mask & PERM.banUsers) || undefined,
    inviteUsers: Boolean(mask & PERM.addMembers) || undefined,
    pinMessages: Boolean(mask & PERM.pinMessages) || undefined,
    addAdmins: isOwner ? true as const : undefined,
    manageCall: true,
  };
}

function decodeBannedRights(defaultPerms: number): any {
  return {
    sendMessages: !(defaultPerms & PERM.sendMessages) || undefined,
    sendMedia: !(defaultPerms & PERM.sendMedia) || undefined,
    inviteUsers: !(defaultPerms & PERM.addMembers) || undefined,
    pinMessages: !(defaultPerms & PERM.pinMessages) || undefined,
    changeInfo: !(defaultPerms & PERM.changeInfo) || undefined,
  };
}

export function buildApiChat(chat: SaturnChat | SaturnChatListItem): ApiChat {
  if (chat.type !== 'direct') registerChatId(chat.id);

  let title = chat.name || '';
  if (chat.type === 'direct' && !title && 'other_user' in chat && chat.other_user) {
    title = chat.other_user.display_name;
  }
  if (!title) {
    title = chat.type === 'direct' ? 'Saved Messages' : 'Chat';
  }

  const apiChat: ApiChat = {
    id: chat.id,
    folderId: chat.is_archived ? ARCHIVED_FOLDER_ID : undefined,
    type: chat.type === 'direct' ? 'chatTypePrivate' : 'chatTypeSuperGroup',
    title,
    creationDate: Math.floor(new Date(chat.created_at).getTime() / 1000),
    isMin: false,
    avatarPhotoId: getAvatarPhotoId(chat.id, chat.avatar_url),
    isPinned: typeof chat.is_pinned === 'boolean' ? chat.is_pinned : undefined,
    isMuted: typeof chat.is_muted === 'boolean' ? chat.is_muted : undefined,
    defaultBannedRights: chat.type !== 'direct'
      ? decodeBannedRights((chat as SaturnChat).default_permissions ?? 255)
      : undefined,
  };

  if ('member_count' in chat) {
    apiChat.membersCount = chat.member_count;
  }

  if ('unread_count' in chat) {
    (apiChat as any).unreadCount = chat.unread_count;
  }

  if (chat.type === 'direct' && 'other_user' in chat && chat.other_user) {
    apiChat.peerUserId = chat.other_user.id;
  }

  if ((chat as { is_protected?: boolean }).is_protected) {
    apiChat.isProtected = true;
  }

  return apiChat;
}

export function buildApiChatFullInfo(
  chat: SaturnChat,
  members?: SaturnChatMember[],
  availableReactions?: SaturnChatAvailableReactions,
): ApiChatFullInfo {
  return {
    about: chat.description || undefined,
    profilePhoto: buildAvatarPhoto(chat.id, chat.avatar_url),
    members: members?.map(buildApiChatMember),
    canViewMembers: true,
    slowMode: chat.slow_mode_seconds ? { seconds: chat.slow_mode_seconds } : undefined,
    enabledReactions: buildApiChatReactions(availableReactions),
  };
}

export function buildApiChatMember(member: SaturnChatMember): ApiChatMember {
  const isOwner = member.role === 'owner';
  const isAdmin = isOwner || member.role === 'admin';
  const permMask = member.permissions || 255; // default all permissions

  return {
    userId: member.user_id,
    inviterId: undefined,
    joinedDate: Math.floor(new Date(member.joined_at).getTime() / 1000),
    kickedByUserId: undefined,
    promotedByUserId: undefined,
    bannedRights: !isAdmin && member.permissions ? decodeBannedRights(member.permissions) : undefined,
    adminRights: isAdmin ? decodeAdminRights(permMask, isOwner) : undefined,
    customTitle: member.custom_title || undefined,
    isOwner: isOwner ? true as const : undefined,
    isAdmin: isAdmin ? true as const : undefined,
  };
}
