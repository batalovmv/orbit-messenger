import { getActions, getGlobal, setGlobal } from '../global';

import type {
  ApiChat, ApiMessage, ApiPeer, ApiPeerReaction,
  ApiPhoneCall, ApiUser,
} from '../api/types';
import { ApiMediaFormat } from '../api/types';

import {
  APP_NAME,
  DEBUG,
  IS_TEST,
  VAPID_PUBLIC_KEY,
} from '../config';
import {
  getChatAvatarHash,
  getChatTitle,
  getMessageRecentReaction,
  getUserFullName,
} from '../global/helpers';
import { getIsChatMuted, getIsChatSilent, getShouldShowMessagePreview } from '../global/helpers/notifications';
import { getMessageSenderName } from '../global/helpers/peers';
import {
  selectCurrentMessageList,
  selectCustomEmoji,
  selectIsChatWithSelf,
  selectNotifyDefaults,
  selectNotifyException,
  selectPeer,
  selectSender,
  selectSettingsKeys,
  selectTopicFromMessage,
} from '../global/selectors';
import { callApi } from '../api/saturn';
import { IS_TAURI } from './browser/globalEnvironment';
import { IS_SERVICE_WORKER_SUPPORTED, IS_TOUCH_ENV } from './browser/windowEnvironment';
import jsxToHtml from './element/jsxToHtml';
import { buildCollectionByKey } from './iteratees';
import { getTranslationFn } from './localization';
import * as mediaLoader from './mediaLoader';
import { oldTranslate } from './oldLangProvider';
import { debounce } from './schedulers';
import { getServerTime } from './serverTime';

import MessageSummary from '../components/common/MessageSummary';

type PushSubscriptionData = {
  auth: string;
  endpoint: string;
  p256dh: string;
};

function serializePushSubscription(data: PushSubscriptionData) {
  return JSON.stringify(data);
}

function getPushSubscriptionData(subscription: PushSubscription): PushSubscriptionData {
  const data = subscription.toJSON();
  const endpoint = data.endpoint;
  const p256dh = data.keys?.p256dh;
  const auth = data.keys?.auth;

  if (!endpoint || !p256dh || !auth) {
    throw new Error('Incomplete push subscription payload');
  }

  return {
    endpoint,
    p256dh,
    auth,
  };
}

function getDeviceToken(subscription: PushSubscription) {
  return serializePushSubscription(getPushSubscriptionData(subscription));
}

function parseStoredPushSubscription(deviceToken: string): Partial<PushSubscriptionData> | undefined {
  try {
    const parsed = JSON.parse(deviceToken);
    const endpoint = parsed.endpoint;
    const p256dh = parsed.p256dh || parsed.keys?.p256dh;
    const auth = parsed.auth || parsed.keys?.auth;

    if (!endpoint) return undefined;

    return {
      endpoint,
      p256dh,
      auth,
    };
  } catch {
    return undefined;
  }
}

function urlBase64ToUint8Array(base64String: string) {
  const padding = '='.repeat((4 - base64String.length % 4) % 4);
  const normalized = `${base64String}${padding}`
    .replace(/-/g, '+')
    .replace(/_/g, '/');
  const rawData = atob(normalized);

  return Uint8Array.from(rawData, (char) => char.charCodeAt(0));
}

function checkIfPushSupported() {
  if (!IS_SERVICE_WORKER_SUPPORTED || IS_TAURI) return false;

  if (!('showNotification' in ServiceWorkerRegistration.prototype)) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.info('[PUSH] Push notifications aren\'t supported.');
    }
    return false;
  }

  // If permission is denied, it is blocked until the user manually changes their settings
  if (Notification.permission === 'denied') {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.info('[PUSH] The user has blocked push notifications.');
    }
    return false;
  }

  // Check if push messaging is supported
  if (!('PushManager' in window)) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.info('[PUSH] Push messaging isn\'t supported.');
    }
    return false;
  }

  return true;
}

export function checkIfNotificationsSupported() {
  // Let's check if the browser supports notifications
  if (!('Notification' in window)) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.info('[PUSH] This browser does not support desktop notification');
    }
    return false;
  }

  if (Notification.permission === 'denied') {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.info('[PUSH] The user has blocked push notifications.');
    }
    return false;
  }
  return true;
}

const expirationTime = 12 * 60 * 60 * 1000; // 12 hours
// Notification id is removed from soundPlayed cache after 3 seconds
const soundPlayedDelay = 3 * 1000;
const soundPlayedIds = new Set<string>();
const notificationSound = new Audio('./notification.mp3');
notificationSound.setAttribute('mozaudiochannel', 'notification');

export async function playNotifySound(id?: string, volume?: number) {
  if (id !== undefined && soundPlayedIds.has(id)) return;
  const { notificationSoundVolume } = selectSettingsKeys(getGlobal());
  const currentVolume = volume ? volume / 10 : notificationSoundVolume / 10;
  if (currentVolume === 0) return;
  notificationSound.volume = currentVolume;
  if (id !== undefined) {
    notificationSound.addEventListener('ended', () => {
      soundPlayedIds.add(id);
    }, { once: true });

    setTimeout(() => {
      soundPlayedIds.delete(id);
    }, soundPlayedDelay);
  }

  try {
    await notificationSound.play();
  } catch (error) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.warn('[PUSH] Unable to play notification sound');
    }
  }
}

export const playNotifySoundDebounced = debounce(playNotifySound, 1000, true, false);

function checkIfShouldResubscribe(subscription: PushSubscription | null) {
  const global = getGlobal();
  if (!global.push || !subscription) return true;
  if (getDeviceToken(subscription) !== global.push.deviceToken) return true;
  return Date.now() - global.push.subscribedAt > expirationTime;
}

export async function requestPermission() {
  if (IS_TAURI) {
    const tauriPlugin = await import('@tauri-apps/plugin-notification');
    const tauriPermissionGranted = await tauriPlugin.isPermissionGranted();

    if (!tauriPermissionGranted) {
      const permission = await tauriPlugin.requestPermission();

      return permission === 'granted';
    }

    return true;
  }

  if (!('Notification' in window)) {
    return false;
  }
  let permission = Notification.permission;
  if (!['granted', 'denied'].includes(permission)) {
    permission = await Notification.requestPermission();
  }
  return permission === 'granted';
}

async function unsubscribeFromPush(subscription: PushSubscription | null) {
  const { deleteDeviceToken } = getActions();
  if (subscription) {
    try {
      const { endpoint } = getPushSubscriptionData(subscription);
      await callApi('unregisterDevice', { endpoint });
      await subscription.unsubscribe();
      deleteDeviceToken();
      return;
    } catch (error) {
      if (DEBUG) {
        // eslint-disable-next-line no-console
        console.log('[PUSH] Unable to unsubscribe from push.', error);
      }
    }
  }
  const global = getGlobal();
  if (global.push) {
    const pushSubscription = parseStoredPushSubscription(global.push.deviceToken);
    if (pushSubscription?.endpoint) {
      await callApi('unregisterDevice', { endpoint: pushSubscription.endpoint });
    }
    deleteDeviceToken();
  }
}

export async function unsubscribe() {
  if (!checkIfPushSupported()) return;
  const serviceWorkerRegistration = await navigator.serviceWorker.ready;
  const subscription = await serviceWorkerRegistration.pushManager.getSubscription();
  await unsubscribeFromPush(subscription);
}

// Load custom emoji from the api if it's not cached already
async function loadCustomEmoji(id: string) {
  let global = getGlobal();
  if (selectCustomEmoji(global, id)) return;
  const customEmoji = await callApi('fetchCustomEmoji', {
    documentId: [id],
  });
  if (!customEmoji) return;
  global = getGlobal();
  global = {
    ...global,
    customEmojis: {
      ...global.customEmojis,
      byId: {
        ...global.customEmojis.byId,
        ...buildCollectionByKey(customEmoji, 'id'),
      },
    },
  };
  setGlobal(global);
}

let isSubscriptionFailed = false;
export function checkIfOfflinePushFailed() {
  return isSubscriptionFailed;
}

export async function subscribe() {
  const { setDeviceToken, updateWebNotificationSettings } = getActions();
  let hasWebNotifications = false;
  let hasPushNotifications = false;
  if (!checkIfPushSupported()) {
    // Ask for notification permissions only if service worker notifications are not supported
    // As pushManager.subscribe automatically triggers permission popup
    hasWebNotifications = await requestPermission();
    updateWebNotificationSettings({
      hasWebNotifications,
      hasPushNotifications,
    });
    return;
  }
  const serviceWorkerRegistration = await navigator.serviceWorker.ready;
  let subscription = await serviceWorkerRegistration.pushManager.getSubscription();
  if (!checkIfShouldResubscribe(subscription) && subscription) {
    setDeviceToken({ token: getDeviceToken(subscription) });
    updateWebNotificationSettings({
      hasWebNotifications: true,
      hasPushNotifications: true,
    });
    return;
  }
  await unsubscribeFromPush(subscription);

  if (!VAPID_PUBLIC_KEY) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.warn('[PUSH] VAPID public key is not configured, falling back to local notifications.');
    }
    isSubscriptionFailed = true;
    hasWebNotifications = await requestPermission();
    updateWebNotificationSettings({
      hasWebNotifications,
      hasPushNotifications,
    });
    return;
  }

  try {
    subscription = await serviceWorkerRegistration.pushManager.subscribe({
      applicationServerKey: urlBase64ToUint8Array(VAPID_PUBLIC_KEY),
      userVisibleOnly: true,
    });
    const pushSubscription = getPushSubscriptionData(subscription);
    const deviceToken = serializePushSubscription(pushSubscription);
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.log('[PUSH] Subscription endpoint registered');
    }
    await callApi('registerDevice', pushSubscription);
    setDeviceToken({ token: deviceToken });
    isSubscriptionFailed = false;
    hasPushNotifications = true;
    hasWebNotifications = true;
  } catch (error: any) {
    if (Notification.permission === 'denied') {
      // The user denied the notification permission which
      // means we failed to subscribe and the user will need
      // to manually change the notification permission to
      // subscribe to push messages
      if (DEBUG) {
        // eslint-disable-next-line no-console
        console.info('[PUSH] The user has blocked push notifications.');
      }
    } else {
      // A problem occurred with the subscription, this can
      // often be down to an issue or lack of the gcm_sender_id
      // and / or gcm_user_visible_only
      if (DEBUG) {
        // eslint-disable-next-line no-console
        console.log('[PUSH] Unable to subscribe to push.', error);
      }
      isSubscriptionFailed = true;
      hasWebNotifications = Notification.permission === 'granted' || await requestPermission();
    }
  }
  updateWebNotificationSettings({
    hasWebNotifications,
    hasPushNotifications,
  });
}

function checkIfShouldNotify(chat: ApiChat, message: Partial<ApiMessage>) {
  const global = getGlobal();
  const isChatMuted = getIsChatMuted(chat, selectNotifyDefaults(global), selectNotifyException(global, chat.id));
  const topic = selectTopicFromMessage(global, message as ApiMessage);
  const topicMutedUntil = topic?.notifySettings.mutedUntil;
  const isMuted = topicMutedUntil === undefined ? isChatMuted : topicMutedUntil > getServerTime();
  const shouldIgnoreMute = message.isMentioned;

  const shouldNotifyAboutMessage = message.content?.action?.type !== 'phoneCall';
  if ((isMuted && !shouldIgnoreMute) || !shouldNotifyAboutMessage
    || chat.isNotJoined || !chat.isListed || selectIsChatWithSelf(global, chat.id)) {
    return false;
  }
  // On touch devices show notifications when chat is not active
  if (IS_TOUCH_ENV) {
    const {
      chatId,
      type,
    } = selectCurrentMessageList(global) || {};
    return !(chatId === chat.id && type === 'thread');
  }
  // On desktop show notifications when window is not focused
  return !document.hasFocus();
}

function getNotificationContent(chat: ApiChat, message: ApiMessage, reaction?: ApiPeerReaction) {
  const global = getGlobal();
  let sender = selectSender(global, message);
  const hasReaction = Boolean(reaction);
  if (hasReaction) {
    sender = selectPeer(global, reaction.peerId);
  }

  const { isScreenLocked } = global.passcode;
  const isSelf = chat.id === global.currentUserId;

  let body: string;
  if (
    !isScreenLocked
    && getShouldShowMessagePreview(chat, selectNotifyDefaults(global), selectNotifyException(global, chat.id))
  ) {
    const senderName = sender ? getMessageSenderName(getTranslationFn(), chat.id, sender) : undefined;
    let summary = jsxToHtml(<span><MessageSummary message={message} /></span>)[0].textContent || '';

    if (hasReaction) {
      const emoji = getReactionEmoji(reaction);
      summary = oldTranslate('PushReactText', [emoji, summary]);
    }

    body = senderName ? `${senderName}: ${summary}` : summary;
  } else {
    body = getTranslationFn()('NotificationMessageTextHidden');
  }

  let title = isScreenLocked ? APP_NAME : getChatTitle(oldTranslate, chat, isSelf);

  if (message.isSilent) {
    title += ' 🔕';
  }

  return { title, body };
}

async function getAvatar(chat: ApiPeer) {
  const imageHash = getChatAvatarHash(chat);
  if (!imageHash) return undefined;
  let mediaData = mediaLoader.getFromMemory(imageHash);
  if (!mediaData) {
    await mediaLoader.fetch(imageHash, ApiMediaFormat.BlobUrl);
    mediaData = mediaLoader.getFromMemory(imageHash);
  }
  return mediaData;
}

function getReactionEmoji(reaction: ApiPeerReaction) {
  let emoji;
  if (reaction.reaction.type === 'emoji') {
    emoji = reaction.reaction.emoticon;
  }

  if (reaction.reaction.type === 'custom') {
    emoji = selectCustomEmoji(getGlobal(), reaction.reaction.documentId)?.emoji;
  }
  return emoji || '❤️';
}

export async function notifyAboutCall({
  call, user,
}: {
  call: ApiPhoneCall; user: ApiUser;
}) {
  const { hasWebNotifications } = selectSettingsKeys(getGlobal());
  if (document.hasFocus() || !hasWebNotifications) return;
  const areNotificationsSupported = checkIfNotificationsSupported();
  if (!areNotificationsSupported) return;

  const icon = await getAvatar(user);

  const options: NotificationOptions = {
    body: getUserFullName(user),
    icon,
    badge: icon,
    tag: `call_${call.id}`,
  };

  if ('vibrate' in navigator) {
    // @ts-ignore
    options.vibrate = [200, 100, 200];
  }

  const notification = new Notification(oldTranslate('VoipIncoming'), options);

  notification.onclick = () => {
    notification.close();
    if (window.focus) {
      window.focus();
    }
  };
}

export async function notifyAboutMessage({
  chat,
  message,
  isReaction = false,
}: { chat: ApiChat; message: Partial<ApiMessage>; isReaction?: boolean }) {
  const global = getGlobal();
  const { hasWebNotifications } = selectSettingsKeys(global);
  if (!checkIfShouldNotify(chat, message)) return;
  const isChatSilent = getIsChatSilent(
    chat, selectNotifyDefaults(getGlobal()), selectNotifyException(getGlobal(), chat.id),
  );
  const topic = selectTopicFromMessage(global, message as ApiMessage);
  const isSilent = topic?.notifySettings.hasSound === undefined ? isChatSilent : !topic.notifySettings.hasSound;

  const areNotificationsSupported = checkIfNotificationsSupported();
  if (!hasWebNotifications || !areNotificationsSupported) {
    if (!isSilent && !message.isSilent && !isReaction && !IS_TAURI) {
      // Only play sound if web notifications are disabled
      playNotifySoundDebounced(String(message.id) || chat.id);
    }

    return;
  }
  if (!areNotificationsSupported) return;

  if (!message.id) return;

  const activeReaction = getMessageRecentReaction(message);
  // Do not notify about reactions on messages that are not outgoing
  if (isReaction && !activeReaction) return;

  // If this is a custom emoji reaction we need to make sure it is loaded
  if (isReaction && activeReaction && activeReaction.reaction.type === 'custom') {
    await loadCustomEmoji(activeReaction.reaction.documentId);
  }

  const icon = await getAvatar(chat);

  const {
    title,
    body,
  } = getNotificationContent(chat, message as ApiMessage, activeReaction);

  if (checkIfPushSupported()) {
    if (navigator.serviceWorker?.controller) {
      // notify service worker about new message notification
      navigator.serviceWorker.controller.postMessage({
        type: 'showMessageNotification',
        payload: {
          title,
          body,
          icon,
          chatId: chat.id,
          messageId: message.id,
          shouldReplaceHistory: true,
          isSilent: isSilent || message.isSilent,
          reaction: activeReaction?.reaction,
        },
      });
    }
  } else {
    const dispatch = getActions();
    const options: NotificationOptions = {
      body,
      icon,
      badge: icon,
      tag: String(message.id),
    };

    if ('vibrate' in navigator) {
      // @ts-ignore
      options.vibrate = [200, 100, 200];
    }

    const notification = new Notification(title, options);

    notification.onclick = () => {
      notification.close();
      dispatch.focusMessage({
        chatId: chat.id,
        messageId: message.id!,
        shouldReplaceHistory: true,
      });
      if (window.focus) {
        window.focus();
      }
    };

    // Play sound when notification is displayed
    notification.onshow = () => {
      // TODO Update when reaction badges are implemented
      if (isReaction || message.isSilent || IS_TAURI) return;
      playNotifySoundDebounced(String(message.id) || chat.id);
    };
  }
}

export function closeMessageNotifications(payload: { chatId: string; lastReadInboxMessageId?: number }) {
  if (IS_TEST || !navigator.serviceWorker?.controller) return;
  navigator.serviceWorker.controller.postMessage({
    type: 'closeMessageNotifications',
    payload,
  });
}

// Notify service worker that client is fully loaded
export function notifyClientReady() {
  if (!navigator.serviceWorker?.controller) return;
  navigator.serviceWorker.controller.postMessage({
    type: 'clientReady',
  });
}
