import { APP_NAME, DEBUG, DEBUG_MORE } from '../config';

declare const self: ServiceWorkerGlobalScope;

enum LegacyBoolean {
  True = '1',
  False = '0',
}

type PushData = {
  title?: string;
  body?: string;
  icon?: string;
  is_silent?: boolean;
  data?: {
    chat_id?: string;
    message_id?: number | string;
    should_replace_history?: boolean;
    is_silent?: boolean;
  };
};

type LegacyPushData = {
  custom: {
    msg_id?: string;
    silent?: string;
    channel_id?: string;
    chat_id?: string;
    from_id?: string;
  };
  mute: LegacyBoolean;
  badge: LegacyBoolean;
  loc_key: string;
  loc_args: string[];
  random_id: number;
  title: string;
  description: string;
};

type NotificationData = {
  messageId?: number;
  chatId?: string;
  title: string;
  body: string;
  isSilent?: boolean;
  icon?: string;
  reaction?: string;
  shouldReplaceHistory?: boolean;
};

type FocusMessageData = {
  chatId?: string;
  messageId?: number;
  reaction?: string;
  shouldReplaceHistory?: boolean;
};

type CloseNotificationData = {
  lastReadInboxMessageId?: number;
  chatId: string;
};

let lastSyncAt = new Date().valueOf();
const shownNotifications = new Set<string>();
const clickBuffer: Record<string, FocusMessageData> = {};

function getPushData(e: PushEvent): PushData | LegacyPushData | undefined {
  if (!e.data) {
    return undefined;
  }

  try {
    return e.data.json();
  } catch (error) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.log('[SW] Unable to parse push notification data', e.data);
    }
    return undefined;
  }
}

function isLegacyPushData(data: PushData | LegacyPushData): data is LegacyPushData {
  return 'custom' in data;
}

function getChatId(data: LegacyPushData) {
  if (data.custom.from_id) {
    return data.custom.from_id;
  }

  // Chats and channels have “negative” IDs
  if (data.custom.chat_id || data.custom.channel_id) {
    return `-${data.custom.chat_id || data.custom.channel_id}`;
  }

  return undefined;
}

function getMessageId(data: LegacyPushData) {
  if (!data.custom.msg_id) return undefined;
  return parseInt(data.custom.msg_id, 10);
}

function normalizeMessageId(messageId: unknown) {
  if (typeof messageId === 'number' && Number.isFinite(messageId)) {
    return messageId;
  }

  if (typeof messageId === 'string' && /^\d+$/.test(messageId)) {
    return Number(messageId);
  }

  return undefined;
}

function getNotificationKey({ chatId, messageId }: Pick<NotificationData, 'chatId' | 'messageId'>) {
  if (!chatId || messageId === undefined) {
    return undefined;
  }

  return `${chatId}:${messageId}`;
}

function getNotificationData(data: PushData | LegacyPushData): NotificationData {
  if (!isLegacyPushData(data)) {
    return {
      chatId: data.data?.chat_id,
      messageId: normalizeMessageId(data.data?.message_id),
      body: data.body || '',
      icon: data.icon,
      isSilent: data.data?.is_silent === true || data.is_silent === true,
      shouldReplaceHistory: data.data?.should_replace_history !== false,
      title: data.title || APP_NAME,
    };
  }

  let title = data.title || APP_NAME;
  const isSilent = data.custom.silent === LegacyBoolean.True;
  if (isSilent) {
    title += ' 🔕';
  }

  return {
    chatId: getChatId(data),
    messageId: getMessageId(data),
    body: data.description,
    isSilent,
    title,
  };
}

async function getClients() {
  const appUrl = new URL(self.registration.scope).origin;
  const clients = await self.clients.matchAll({ type: 'window' }) as WindowClient[];
  return clients.filter((client) => {
    return new URL(client.url).origin === appUrl;
  });
}

async function playNotificationSound(id: string) {
  const clients = await getClients();
  const client = clients[0];
  if (!client) return;
  client.postMessage({
    type: 'playNotificationSound',
    payload: { id },
  });
}

function showNotification({
  chatId,
  messageId,
  body,
  title,
  icon,
  reaction,
  isSilent,
  shouldReplaceHistory,
}: NotificationData) {
  const isFirstBatch = new Date().valueOf() - lastSyncAt < 1000;
  const tag = String(isFirstBatch ? 0 : chatId || 0);
  const options: NotificationOptions = {
    body,
    data: {
      chatId,
      messageId,
      reaction,
      count: 1,
      shouldReplaceHistory,
    },
    icon: icon || 'icon-192x192.png',
    badge: 'icon-192x192.png',
    tag,
    // @ts-ignore
    vibrate: [200, 100, 200],
  };

  return Promise.all([
    // TODO Update condition when reaction badges are implemented
    (!reaction && !isSilent) ? playNotificationSound(String(messageId) || chatId || '') : undefined,
    self.registration.showNotification(title, options),
  ]);
}

async function closeNotifications({
  chatId,
  lastReadInboxMessageId,
}: CloseNotificationData) {
  const notifications = await self.registration.getNotifications();
  const lastMessageId = lastReadInboxMessageId || Number.MAX_VALUE;
  notifications.forEach((notification) => {
    if (
      notification.tag === '0'
      || (notification.data.chatId === chatId && notification.data.messageId <= lastMessageId)
    ) {
      notification.close();
    }
  });
}

async function hasMatchingNotification({
  chatId,
  messageId,
}: Pick<NotificationData, 'chatId' | 'messageId'>) {
  if (!chatId || messageId === undefined) {
    return false;
  }

  const notifications = await self.registration.getNotifications();
  return notifications.some((notification) => {
    return notification.data?.chatId === chatId && notification.data?.messageId === messageId;
  });
}

export function handlePush(e: PushEvent) {
  if (DEBUG) {
    // eslint-disable-next-line no-console
    console.log('[SW] Push received event', e);
    if (e.data) {
      // eslint-disable-next-line no-console
      console.log('[SW] Push received with data', e.data.json());
    }
  }

  const data = getPushData(e);
  if (!data) return;
  if (isLegacyPushData(data) && data.mute === LegacyBoolean.True) return;

  const notification = getNotificationData(data);
  const notificationKey = getNotificationKey(notification);

  // Don't show already triggered notification
  if (notificationKey && shownNotifications.has(notificationKey)) {
    shownNotifications.delete(notificationKey);
    return;
  }

  e.waitUntil(showNotification(notification));
}

async function focusChatMessage(client: WindowClient, data: FocusMessageData) {
  if (!data.chatId) return;
  client.postMessage({
    type: 'focusMessage',
    payload: data,
  });
  if (!client.focused) {
    // Catch "focus not allowed" DOM Exceptions
    try {
      await client.focus();
    } catch (error) {
      if (DEBUG) {
        // eslint-disable-next-line no-console
        console.warn('[SW] ', error);
      }
    }
  }
}

export function handleNotificationClick(e: NotificationEvent) {
  const appUrl = self.registration.scope;
  e.notification.close(); // Android needs explicit close.
  const { data } = e.notification;
  const notifyClients = async () => {
    const clients = await getClients();
    await Promise.all(clients.map((client) => {
      clickBuffer[client.id] = data;
      return focusChatMessage(client, data);
    }));
    if (!self.clients.openWindow || clients.length > 0) return undefined;
    // Store notification data for default client (fix for android)
    clickBuffer[0] = data;
    // If there is no opened client we need to open one and wait until it is fully loaded
    try {
      const newClient = await self.clients.openWindow(appUrl);
      if (newClient) {
        // Store notification data until client is fully loaded
        clickBuffer[newClient.id] = data;
      }
    } catch (error) {
      if (DEBUG) {
        // eslint-disable-next-line no-console
        console.warn('[SW] ', error);
      }
    }
    return undefined;
  };
  e.waitUntil(notifyClients());
}

export function handleClientMessage(e: ExtendableMessageEvent) {
  if (DEBUG_MORE) {
    // eslint-disable-next-line no-console
    console.log('[SW] New message from client', e);
  }
  if (!e.data) return;
  const source = e.source as WindowClient;
  if (e.data.type === 'clientReady') {
    // focus on chat message when client is fully ready
    const data = clickBuffer[source.id] || clickBuffer[0];
    if (data) {
      delete clickBuffer[source.id];
      delete clickBuffer[0];
      e.waitUntil(focusChatMessage(source, data));
    }
  }
  if (e.data.type === 'showMessageNotification') {
    // store messageId for already shown notification
    const notification: NotificationData = e.data.payload;
    e.waitUntil((async () => {
      if (await hasMatchingNotification(notification)) {
        return undefined;
      }

      // Close existing notification if it is already shown
      if (notification.chatId) {
        const notifications = await self.registration.getNotifications({ tag: notification.chatId });
        notifications.forEach((n) => n.close());
      }
      // Mark this notification as shown if it was handled locally
      const notificationKey = getNotificationKey(notification);
      if (notificationKey) {
        shownNotifications.add(notificationKey);
      }
      return showNotification(notification);
    })());
  }

  if (e.data.type === 'closeMessageNotifications') {
    e.waitUntil(closeNotifications(e.data.payload));
  }
}

self.addEventListener('sync', () => {
  lastSyncAt = Date.now();
});
