// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import { request } from '../client';
import type {
  SaturnBot, SaturnBotCommand, SaturnBotCreateResponse, SaturnBotInstallation, SaturnBotMenuButton,
} from '../types';

export type UpdateBotPayload = Partial<{
  username: string;
  display_name: string;
  description: string;
  short_description: string;
  about_text: string;
  is_inline: boolean;
  inline_placeholder: string;
  is_privacy_enabled: boolean;
  can_join_groups: boolean;
  can_read_all_group_messages: boolean;
  menu_button: SaturnBotMenuButton;
  clear_menu_button: boolean;
  webhook_url: string;
  is_active: boolean;
}>;

export async function fetchBots(limit = 50, offset = 0) {
  return request<{ data: SaturnBot[]; total: number }>('GET', `/bots?limit=${limit}&offset=${offset}`);
}

export async function fetchBot(botId: string) {
  return request<SaturnBot>('GET', `/bots/${botId}`);
}

export async function createBot(data: { username: string; display_name: string; description?: string }) {
  return request<SaturnBotCreateResponse>('POST', '/bots', data);
}

export async function updateBot(botId: string, data: UpdateBotPayload) {
  return request<SaturnBot>('PATCH', `/bots/${botId}`, data);
}

export async function deleteBot(botId: string) {
  return request<void>('DELETE', `/bots/${botId}`);
}

export async function rotateToken(botId: string) {
  return request<{ token: string }>('POST', `/bots/${botId}/token/rotate`);
}

export async function setBotCommands(botId: string, commands: Array<{ command: string; description: string }>) {
  return request<SaturnBotCommand[]>('PUT', `/bots/${botId}/commands`, { commands });
}

export async function fetchBotCommands(botId: string) {
  return request<SaturnBotCommand[]>('GET', `/bots/${botId}/commands`);
}

export async function installBot(botId: string, chatId: string, scopes: number) {
  return request<SaturnBotInstallation>('POST', `/bots/${botId}/install`, { chat_id: chatId, scopes });
}

export async function uninstallBot(botId: string, chatId: string) {
  return request<void>('DELETE', `/bots/${botId}/install`, { chat_id: chatId });
}

export async function fetchChatBots(chatId: string) {
  return request<SaturnBotInstallation[]>('GET', `/chats/${chatId}/bots`);
}

export async function fetchBotByUserId(userId: string) {
  return request<SaturnBot>('GET', `/bots/by-user/${userId}`);
}

export async function sendBotCallback(
  messageId: string,
  chatId: string,
  viaBotId: string,
  data: string,
) {
  return request<{ text?: string; show_alert?: boolean }>('POST', '/bots/callback', {
    message_id: messageId,
    chat_id: chatId,
    via_bot_id: viaBotId,
    data,
  });
}
