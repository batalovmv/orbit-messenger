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
    priority?: 'urgent' | 'important' | 'normal' | 'low';
  };
};

// Stage 4: high-priority incoming call push payload from gateway nats_subscriber.
type CallPushData = {
  type: 'call_incoming';
  call_id: string;
  caller_id: string;
  caller_name?: string;
  call_type?: 'voice' | 'video';
  call_mode?: 'p2p' | 'group';
  chat_id?: string;
  timestamp?: number;
};

function isCallPushData(data: unknown): data is CallPushData {
  return Boolean(data) && typeof data === 'object'
    && (data as { type?: string }).type === 'call_incoming'
    && typeof (data as { call_id?: unknown }).call_id === 'string'
    && typeof (data as { caller_id?: unknown }).caller_id === 'string';
}

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
  priority?: 'urgent' | 'important' | 'normal' | 'low';
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

const MAX_SHOWN_NOTIFICATIONS = 500;
let lastSyncAt = new Date().valueOf();
const shownNotifications = new Set<string>();

function trackShownNotification(key: string) {
  shownNotifications.add(key);
  if (shownNotifications.size > MAX_SHOWN_NOTIFICATIONS) {
    const first = shownNotifications.values().next().value;
    if (first) shownNotifications.delete(first);
  }
}
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
      priority: data.data?.priority,
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
  priority,
}: NotificationData) {
  const isFirstBatch = new Date().valueOf() - lastSyncAt < 1000;
  const tag = String(isFirstBatch ? 0 : chatId || 0);

  // Low-priority notifications are silent; urgent ones are persistent
  const isLowPriority = priority === 'low';
  const isUrgent = priority === 'urgent';
  const effectiveSilent = isSilent || isLowPriority;

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
    requireInteraction: isUrgent,
    // @ts-ignore
    vibrate: [200, 100, 200],
  };

  return Promise.all([
    // TODO Update condition when reaction badges are implemented
    (!reaction && !effectiveSilent) ? playNotificationSound(String(messageId) || chatId || '') : undefined,
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

function showCallNotification(data: CallPushData) {
  const callTypeLabel = data.call_type === 'video' ? 'видеозвонок' : 'голосовой звонок';
  const title = data.caller_name?.trim() || 'Входящий звонок';
  const body = `Входящий ${callTypeLabel}`;
  const options: NotificationOptions = {
    body,
    // tag is per-call so a re-ring collapses the prior notification rather
    // than stacking it. requireInteraction prevents auto-dismissal — the
    // user must accept/decline. renotify forces re-alert if tag is replaced.
    tag: `call-${data.call_id}`,
    icon: 'icon-192x192.png',
    badge: 'icon-192x192.png',
    requireInteraction: true,
    // @ts-ignore
    renotify: true,
    // @ts-ignore
    vibrate: [300, 200, 300, 200, 300],
    data: {
      isCall: true,
      callId: data.call_id,
      callerId: data.caller_id,
      callMode: data.call_mode || 'p2p',
      callType: data.call_type || 'voice',
      chatId: data.chat_id,
    },
    // @ts-ignore actions are widely supported in service worker notifications
    actions: [
      { action: 'accept', title: 'Принять' },
      { action: 'decline', title: 'Отклонить' },
    ],
  };
  return self.registration.showNotification(title, options);
}

export function handlePush(e: PushEvent) {
  const data = getPushData(e);
  if (DEBUG) {
    // eslint-disable-next-line no-console
    console.log('[SW] Push received with data', data);
  }
  if (!data) return;

  // Stage 4: incoming call push has its own shape (no message fields).
  if (isCallPushData(data)) {
    e.waitUntil(showCallNotification(data));
    return;
  }

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

async function handleCallNotificationClick(
  appUrl: string,
  action: string,
  data: { callId: string; callMode: 'p2p' | 'group'; callType: 'voice' | 'video'; chatId?: string },
) {
  const callAction = action === 'decline' ? 'decline' : 'accept';
  const clients = await getClients();
  if (clients.length > 0) {
    await Promise.all(clients.map(async (client) => {
      client.postMessage({
        type: 'callAction',
        payload: {
          action: callAction,
          callId: data.callId,
          callMode: data.callMode,
          callType: data.callType,
          chatId: data.chatId,
        },
      });
      if (!client.focused) {
        try {
          await client.focus();
        } catch (error) {
          if (DEBUG) {
            // eslint-disable-next-line no-console
            console.warn('[SW] call focus failed', error);
          }
        }
      }
    }));
    return;
  }

  // No window open — boot a fresh tab. Main thread reads URL params on mount
  // and dispatches the same action handlers once auth is restored.
  if (!self.clients.openWindow) return;
  const url = `${appUrl}?call_action=${callAction}&call_id=${encodeURIComponent(data.callId)}`
    + `&call_mode=${encodeURIComponent(data.callMode)}`;
  try {
    await self.clients.openWindow(url);
  } catch (error) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.warn('[SW] openWindow for call failed', error);
    }
  }
}

export function handleNotificationClick(e: NotificationEvent) {
  const appUrl = self.registration.scope;
  e.notification.close(); // Android needs explicit close.
  const { data } = e.notification;

  // Stage 4: incoming call notification — accept/decline routed back to main thread.
  if (data?.isCall && data.callId) {
    e.waitUntil(handleCallNotificationClick(appUrl, e.action, {
      callId: data.callId,
      callMode: data.callMode || 'p2p',
      callType: data.callType || 'voice',
      chatId: data.chatId,
    }));
    return;
  }

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
        trackShownNotification(notificationKey);
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
