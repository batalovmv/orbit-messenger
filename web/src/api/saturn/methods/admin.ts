// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

export async function fetchAdminChatExport(chatId: string): Promise<Response> {
  return fetch(`/api/v1/admin/chats/${chatId}/export?format=json`, {
    headers: {
      Authorization: `Bearer ${localStorage.getItem('access_token') ?? ''}`,
    },
  });
}

export async function fetchAdminUserExport(userId: string): Promise<Response> {
  return fetch(`/api/v1/admin/users/${userId}/export?format=json`, {
    headers: {
      Authorization: `Bearer ${localStorage.getItem('access_token') ?? ''}`,
    },
  });
}
