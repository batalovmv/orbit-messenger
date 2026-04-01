// Phase 4: Settings, Privacy, Blocked Users, Notifications, Push Subscriptions

import type {
  SaturnBlockedUser,
  SaturnNotificationSettings,
  SaturnPrivacySettings,
  SaturnPushSubscription,
  SaturnUserSettings,
} from '../types';
import { request } from '../client';

// ─── Privacy Settings ──────────────────────────────────────────────────────

export async function getPrivacySettings(): Promise<SaturnPrivacySettings | undefined> {
  try {
    return await request<SaturnPrivacySettings>('GET', '/users/me/settings/privacy');
  } catch {
    return undefined;
  }
}

export async function setPrivacySettings({
  lastSeen, avatar, phone, calls, groups, forwarded,
}: {
  lastSeen: string;
  avatar: string;
  phone: string;
  calls: string;
  groups: string;
  forwarded: string;
}): Promise<SaturnPrivacySettings | undefined> {
  try {
    return await request<SaturnPrivacySettings>('PUT', '/users/me/settings/privacy', {
      last_seen: lastSeen,
      avatar,
      phone,
      calls,
      groups,
      forwarded,
    });
  } catch {
    return undefined;
  }
}

// ─── User Settings (Appearance) ────────────────────────────────────────────

export async function getUserSettings(): Promise<SaturnUserSettings | undefined> {
  try {
    return await request<SaturnUserSettings>('GET', '/users/me/settings/appearance');
  } catch {
    return undefined;
  }
}

export async function updateUserSettings({
  theme, language, fontSize, sendByEnter, dndFrom, dndUntil,
}: {
  theme: string;
  language: string;
  fontSize: number;
  sendByEnter: boolean;
  dndFrom?: string;
  dndUntil?: string;
}): Promise<SaturnUserSettings | undefined> {
  try {
    return await request<SaturnUserSettings>('PUT', '/users/me/settings/appearance', {
      theme,
      language,
      font_size: fontSize,
      send_by_enter: sendByEnter,
      dnd_from: dndFrom,
      dnd_until: dndUntil,
    });
  } catch {
    return undefined;
  }
}

// ─── Blocked Users ─────────────────────────────────────────────────────────

export async function fetchBlockedUsersList({ limit }: {
  limit?: number;
} = {}): Promise<{ blocked_users: SaturnBlockedUser[] } | undefined> {
  try {
    return await request<{ blocked_users: SaturnBlockedUser[] }>(
      'GET', `/users/me/blocked?limit=${limit || 50}`,
    );
  } catch {
    return undefined;
  }
}

export async function blockUser({ userId }: { userId: string }): Promise<boolean> {
  try {
    await request('POST', `/users/me/blocked/${userId}`);
    return true;
  } catch {
    return false;
  }
}

export async function unblockUser({ userId }: { userId: string }): Promise<boolean> {
  try {
    await request('DELETE', `/users/me/blocked/${userId}`);
    return true;
  } catch {
    return false;
  }
}

// ─── Notification Settings ─────────────────────────────────────────────────

export async function getChatNotifySettings({ chatId }: {
  chatId: string;
}): Promise<SaturnNotificationSettings | undefined> {
  try {
    return await request<SaturnNotificationSettings>('GET', `/chats/${chatId}/notifications`);
  } catch {
    return undefined;
  }
}

export async function updateChatNotifySettings({
  chatId, mutedUntil, sound, showPreview,
}: {
  chatId: string;
  mutedUntil?: string;
  sound?: string;
  showPreview?: boolean;
}): Promise<SaturnNotificationSettings | undefined> {
  try {
    return await request<SaturnNotificationSettings>('PUT', `/chats/${chatId}/notifications`, {
      muted_until: mutedUntil,
      sound: sound || 'default',
      show_preview: showPreview !== false,
    });
  } catch {
    return undefined;
  }
}

export async function deleteChatNotifySettings({ chatId }: {
  chatId: string;
}): Promise<boolean> {
  try {
    await request('DELETE', `/chats/${chatId}/notifications`);
    return true;
  } catch {
    return false;
  }
}

// ─── Push Subscriptions ────────────────────────────────────────────────────

export async function subscribePush({
  endpoint, p256dh, auth,
}: {
  endpoint: string;
  p256dh: string;
  auth: string;
}): Promise<SaturnPushSubscription | undefined> {
  try {
    return await request<SaturnPushSubscription>('POST', '/push/subscribe', {
      endpoint,
      p256dh,
      auth,
    });
  } catch {
    return undefined;
  }
}

export async function unsubscribePush({ endpoint }: { endpoint: string }): Promise<boolean> {
  try {
    await request('DELETE', '/push/subscribe', { endpoint });
    return true;
  } catch {
    return false;
  }
}
