import { request } from '../client';

export type NotificationMode = 'smart' | 'all' | 'off';

export type NotificationPriority = 'urgent' | 'important' | 'normal' | 'low';

// eslint-disable-next-line no-null/no-null
export type NotificationPriorityOverride = NotificationPriority | null;

export type NotificationModeResponse = {
  mode: NotificationMode;
};

export type NotificationStats = {
  mode: NotificationMode;
  total_classified: number;
  by_priority: Record<NotificationPriority, number>;
  period_start: string;
};

export async function getNotificationMode(): Promise<NotificationModeResponse | undefined> {
  return request<NotificationModeResponse>('GET', '/users/me/notification-priority');
}

export async function updateNotificationMode(mode: NotificationMode) {
  return request('PUT', '/users/me/notification-priority', { mode });
}

export async function fetchNotificationStats(): Promise<NotificationStats | undefined> {
  return request<NotificationStats>('GET', '/ai/notification-priority/stats');
}

export async function updateChatNotificationPriority(
  chatId: string,
  priority: NotificationPriorityOverride,
) {
  return request('PUT', `/chats/${chatId}/notification-priority`, { priority_override: priority });
}

export async function sendNotificationFeedback(
  messageId: string,
  classifiedPriority: string,
  userOverride: string,
) {
  return request('POST', '/ai/notification-priority/feedback', {
    message_id: messageId,
    classified_priority: classifiedPriority,
    user_override_priority: userOverride,
  });
}
