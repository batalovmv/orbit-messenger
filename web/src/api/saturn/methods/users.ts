import type { SaturnUser } from '../types';

import { buildApiUser, buildApiUserFullInfo, buildApiUserStatus } from '../apiBuilders/users';
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

  const apiUsers = result.users.map(buildApiUser);

  result.users.forEach((user) => {
    sendApiUpdate({
      '@type': 'updateUser',
      id: user.id,
      user: buildApiUser(user),
    });
  });

  return {
    users: apiUsers,
    userStatusesById: Object.fromEntries(
      result.users.map((u) => [u.id, buildApiUserStatus(u)]),
    ),
  };
}

export async function fetchGlobalUsers({ limit = 50 }: { limit?: number } = {}) {
  // Saturn has no "list all users" endpoint; use search with empty query as fallback
  const result = await client.request<{ users: SaturnUser[] }>(
    'GET', `/users?q=&limit=${limit}`,
  );

  const apiUsers = result.users.map(buildApiUser);

  result.users.forEach((user) => {
    sendApiUpdate({
      '@type': 'updateUser',
      id: user.id,
      user: buildApiUser(user),
    });
  });

  return {
    users: apiUsers,
    userStatusesById: Object.fromEntries(
      result.users.map((u) => [u.id, buildApiUserStatus(u)]),
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
    globalResultIds: peerIds,
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
  });

  return { user: apiUser };
}
