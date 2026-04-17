import type { ApiBotInfo, ApiBotMenuButton } from '../../types';
import type { ApiUser, ApiUserFullInfo, ApiUserStatus } from '../../types';
import type { SaturnBot, SaturnBotCommand, SaturnUser } from '../types';

import { buildAvatarPhoto, getAvatarPhotoId } from './avatars';

export function buildApiUser(user: SaturnUser): ApiUser {
  const nameParts = (user.display_name || '').split(' ');
  const firstName = nameParts[0] || '';
  const lastName = nameParts.slice(1).join(' ') || undefined;

  const isBot = user.account_type === 'bot';
  const usernameValue = user.username || user.email;

  return {
    id: user.id,
    isMin: false,
    type: isBot ? 'userTypeBot' : 'userTypeRegular',
    firstName,
    lastName,
    phoneNumber: user.phone || '',
    avatarPhotoId: getAvatarPhotoId(user.id, user.avatar_url),
    hasUsername: Boolean(usernameValue),
    usernames: usernameValue ? [{ username: usernameValue, isActive: true, isEditable: !isBot }] : undefined,
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

// buildApiBotInfoFromSaturn maps our Saturn bot record into the ApiBotInfo
// shape the legacy Telegram UI expects. We intentionally leave photo/gif
// empty — Saturn bots reuse the user avatar rendered by MessageListAccountInfo
// instead of the TG "rich cover" media slot.
export function buildApiBotInfoFromSaturn(bot: SaturnBot, commands?: SaturnBotCommand[]): ApiBotInfo {
  const description = (bot.about_text && bot.about_text.trim())
    || (bot.description && bot.description.trim())
    || undefined;

  const apiCommands = commands?.map((c) => ({
    botId: bot.id,
    command: c.command,
    description: c.description,
  }));

  let menuButton: ApiBotMenuButton = { type: 'commands' };
  if (bot.menu_button?.type === 'web_app' && bot.menu_button.web_app_url) {
    menuButton = {
      type: 'webApp',
      text: bot.menu_button.text || 'Open',
      url: bot.menu_button.web_app_url,
    };
  }

  return {
    botId: bot.user_id,
    description,
    commands: apiCommands,
    menuButton,
  };
}
