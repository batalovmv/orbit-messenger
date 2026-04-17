import type { SaturnBot, SaturnBotCommand, SaturnUser } from '../types';

import {
  buildApiBotInfoFromSaturn, buildApiUser, buildApiUserFullInfo, buildApiUserStatus,
} from '../apiBuilders/users';
import * as client from '../client';
import { sendApiUpdate } from '../updates/apiUpdateEmitter';

export async function fetchCurrentUser() {
  const user = await client.request<SaturnUser>('GET', '/users/me');

  const apiUser = buildApiUser(user);
  apiUser.isSelf = true;

  sendApiUpdate({
    '@type': 'updateCurrentUser',
    currentUser: apiUser,
    currentUserFullInfo: buildApiUserFullInfo(user),
    saturnRole: user.role,
  });

  sendApiUpdate({
    '@type': 'updateUserStatus',
    userId: user.id,
    status: buildApiUserStatus(user),
  });

  return { user: apiUser };
}

export async function fetchUser({ userId }: { userId: string }) {
  const user = await client.request<SaturnUser>('GET', `/users/${userId}`);

  const apiUser = buildApiUser(user);

  sendApiUpdate({
    '@type': 'updateUser',
    id: user.id,
    user: {
      ...apiUser,
    },
  });

  sendApiUpdate({
    '@type': 'updateUserStatus',
    userId: user.id,
    status: buildApiUserStatus(user),
  });

  return { user: apiUser };
}

export async function fetchFullUser({ id }: { id: string; accessHash?: string }) {
  const saturnUser = await client.request<SaturnUser>('GET', `/users/${id}`);

  const user = buildApiUser(saturnUser);
  const fullInfo = buildApiUserFullInfo(saturnUser);
  const status = buildApiUserStatus(saturnUser);

  if (saturnUser.account_type === 'bot') {
    try {
      const bot = await client.request<SaturnBot>('GET', `/bots/by-user/${saturnUser.id}`);
      let commands: SaturnBotCommand[] | undefined;
      try {
        commands = await client.request<SaturnBotCommand[]>('GET', `/bots/${bot.id}/commands`);
      } catch {
        commands = undefined;
      }
      fullInfo.botInfo = buildApiBotInfoFromSaturn(bot, commands);
    } catch {
      // Not every bot-typed user is in the bots registry (e.g. legacy system accounts);
      // leaving botInfo undefined keeps the UI in "no bot metadata" state.
    }
  }

  sendApiUpdate({
    '@type': 'updateUser',
    id: saturnUser.id,
    user,
  });

  sendApiUpdate({
    '@type': 'updateUserStatus',
    userId: saturnUser.id,
    status,
  });

  return {
    user,
    fullInfo,
    users: [user],
    chats: [],
    userStatusesById: { [saturnUser.id]: status },
  };
}

export async function searchUsers({ query, limit = 20 }: { query: string; limit?: number }) {
  const result = await client.request<{ users: SaturnUser[] }>(
    'GET', `/users?q=${encodeURIComponent(query)}&limit=${limit}`,
  );

  const users = result.users || [];
  const apiUsers = users.map(buildApiUser);

  users.forEach((user) => {
    sendApiUpdate({
      '@type': 'updateUser',
      id: user.id,
      user: buildApiUser(user),
    });
  });

  return {
    users: apiUsers,
    userStatusesById: Object.fromEntries(
      users.map((u) => [u.id, buildApiUserStatus(u)]),
    ),
  };
}

export async function fetchGlobalUsers({ limit = 50 }: { limit?: number } = {}) {
  // Saturn has no "list all users" endpoint; use search with empty query as fallback
  const result = await client.request<{ users: SaturnUser[] }>(
    'GET', `/users?q=&limit=${limit}`,
  );

  const users = result.users || [];
  const apiUsers = users.map(buildApiUser);

  users.forEach((user) => {
    sendApiUpdate({
      '@type': 'updateUser',
      id: user.id,
      user: buildApiUser(user),
    });
  });

  return {
    users: apiUsers,
    userStatusesById: Object.fromEntries(
      users.map((u) => [u.id, buildApiUserStatus(u)]),
    ),
  };
}

// Returns all users as "contacts" — Saturn has no separate contact list
export async function fetchContactList() {
  const result = await client.request<{ users: SaturnUser[] }>(
    'GET', '/users?q=&limit=50',
  );

  const users = (result.users || []).map(buildApiUser);

  users.forEach((user) => {
    sendApiUpdate({
      '@type': 'updateUser',
      id: user.id,
      user,
    });
  });

  return {
    users,
    userStatusesById: Object.fromEntries(
      (result.users || []).map((u) => [u.id, buildApiUserStatus(u)]),
    ),
  };
}

// Used by globalSearch action — returns user IDs matching the query
export async function searchChats({ query }: { query: string }) {
  const result = await client.request<{ users: SaturnUser[] }>(
    'GET', `/users?q=${encodeURIComponent(query)}&limit=20`,
  );

  const users = (result.users || []).map(buildApiUser);

  users.forEach((user) => {
    sendApiUpdate({
      '@type': 'updateUser',
      id: user.id,
      user,
    });
  });

  const peerIds = users.map((u) => u.id);

  return {
    accountResultIds: peerIds,
    globalResultIds: [],
  };
}

export async function updateProfile({
  displayName, bio, phone, avatarUrl,
}: {
  displayName?: string;
  bio?: string;
  phone?: string;
  avatarUrl?: string;
}) {
  const body: Record<string, unknown> = {};
  if (displayName !== undefined) body.display_name = displayName;
  if (bio !== undefined) body.bio = bio;
  if (phone !== undefined) body.phone = phone;
  if (avatarUrl !== undefined) body.avatar_url = avatarUrl;

  const user = await client.request<SaturnUser>('PUT', '/users/me', body);

  const apiUser = buildApiUser(user);
  apiUser.isSelf = true;

  sendApiUpdate({
    '@type': 'updateCurrentUser',
    currentUser: apiUser,
    currentUserFullInfo: buildApiUserFullInfo(user),
    saturnRole: user.role,
  });

  return { user: apiUser };
}

export async function checkUsername(username: string): Promise<{ result?: boolean; error?: string }> {
  try {
    const usernameRegex = /^[a-zA-Z][a-zA-Z0-9_]{4,31}$/;
    if (!usernameRegex.test(username)) {
      return {
        result: false,
        error: 'Username must be 5-32 characters, start with a letter, and contain only letters, numbers, and underscores',
      };
    }

    const response = await client.request<{ users: any[] }>(
      'GET',
      `/users?q=${encodeURIComponent(username)}&limit=1`,
    );
    const taken = response?.users?.some((u: any) => u.username?.toLowerCase() === username.toLowerCase());

    return { result: !taken };
  } catch {
    return { result: undefined, error: 'Failed to check username' };
  }
}

export async function updateUsername(username: string): Promise<boolean | undefined> {
  try {
    await client.request('PUT', '/users/me', { username });
    return true;
  } catch {
    return undefined;
  }
}

export async function updateEmojiStatus(emojiStatus: any): Promise<boolean> {
  if (!emojiStatus) {
    await client.request('PUT', '/users/me', { custom_status: null, custom_status_emoji: null });
  } else {
    await client.request('PUT', '/users/me', {
      custom_status: emojiStatus.title || '',
      custom_status_emoji: emojiStatus.documentId || emojiStatus.emoji || '',
    });
  }

  return true;
}
