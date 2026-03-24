import type { ApiChat, ApiChatFullInfo, ApiChatMember } from '../../types';
import type { SaturnChat, SaturnChatListItem, SaturnChatMember } from '../types';

export function buildApiChat(chat: SaturnChat | SaturnChatListItem): ApiChat {
  let title = chat.name || '';
  if (chat.type === 'direct' && !title && 'other_user' in chat && chat.other_user) {
    title = chat.other_user.display_name;
  }
  if (!title) {
    title = chat.type === 'direct' ? 'Saved Messages' : 'Chat';
  }

  const apiChat: ApiChat = {
    id: chat.id,
    type: chat.type === 'direct' ? 'chatTypePrivate'
      : chat.type === 'channel' ? 'chatTypeChannel'
        : 'chatTypeBasicGroup',
    title,
    creationDate: Math.floor(new Date(chat.created_at).getTime() / 1000),
    isMin: false,
  };

  if ('member_count' in chat) {
    apiChat.membersCount = chat.member_count;
  }

  return apiChat;
}

export function buildApiChatFullInfo(
  chat: SaturnChat,
  members?: SaturnChatMember[],
): ApiChatFullInfo {
  return {
    about: chat.description || undefined,
    members: members?.map(buildApiChatMember),
    canViewMembers: true,
  };
}

export function buildApiChatMember(member: SaturnChatMember): ApiChatMember {
  return {
    userId: member.user_id,
    inviterId: undefined,
    joinedDate: Math.floor(new Date(member.joined_at).getTime() / 1000),
    kickedByUserId: undefined,
    promotedByUserId: undefined,
    bannedRights: undefined,
    adminRights: member.role === 'owner' || member.role === 'admin' ? {
      changeInfo: true,
      deleteMessages: true,
      banUsers: true,
      inviteUsers: true,
      pinMessages: true,
      manageCall: true,
      addAdmins: member.role === 'owner' ? true as true : undefined,
    } : undefined,
    customTitle: undefined,
    isOwner: member.role === 'owner' ? true as true : undefined,
    isAdmin: (member.role === 'owner' || member.role === 'admin') ? true as true : undefined,
  };
}
