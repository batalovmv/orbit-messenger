// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import type { ApiChat } from '../../types';
import type { SaturnChatListItem, SaturnPaginatedResponse } from '../types';

import * as client from '../client';
import * as apiUpdateEmitter from '../updates/apiUpdateEmitter';
import {
  archiveChat,
  fetchChats,
  setChatMuted,
  toggleChatPinned,
} from './chats';

describe('fetchChats', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('normalizes last message chat id to the parent chat item', async () => {
    jest.spyOn(client, 'request').mockResolvedValue({
      data: [{
        id: 'chat-1',
        type: 'group',
        name: 'Release',
        max_members: 100,
        created_at: '2026-04-03T10:00:00.000Z',
        updated_at: '2026-04-03T10:00:00.000Z',
        default_permissions: 255,
        slow_mode_seconds: 0,
        is_signatures: false,
        is_pinned: true,
        is_muted: true,
        is_archived: true,
        member_count: 3,
        unread_count: 0,
        last_message: {
          id: 'message-1',
          chat_id: 'other-chat',
          sender_id: 'user-1',
          type: 'poll',
          is_edited: false,
          is_deleted: false,
          is_pinned: false,
          is_forwarded: false,
          sequence_number: 5,
          created_at: '2026-04-03T10:00:00.000Z',
          sender_name: 'Orbit',
          poll: {
            id: 'poll-1',
            message_id: 'message-1',
            question: 'Ship the release?',
            is_anonymous: false,
            is_multiple: false,
            is_quiz: false,
            is_closed: false,
            options: [{
              id: 'option-1',
              poll_id: 'poll-1',
              text: 'Yes',
              position: 0,
              voters: 0,
            }],
            total_voters: 0,
            created_at: '2026-04-03T10:00:00.000Z',
          },
        },
      }],
      has_more: false,
    } satisfies SaturnPaginatedResponse<SaturnChatListItem>);

    const result = await fetchChats({ limit: 1 });

    expect(result.messages).toHaveLength(1);
    expect(result.chats[0].lastMessage?.chatId).toBe('chat-1');
    expect(result.chats[0].lastMessage?.id).toBe(5);
    expect(result.chats[0].isPinned).toBe(true);
    expect(result.chats[0].isMuted).toBe(true);
    expect(result.chats[0].folderId).toBe(1);
    expect(result.messages[0].chatId).toBe('chat-1');
    expect(result.messages[0].content.pollId).toBe('poll-1');
    expect(result.messages[0].content.text?.text).toBe('Ship the release?');
  });

  it('persists pinned state before emitting the local update', async () => {
    const sendApiUpdate = jest.spyOn(apiUpdateEmitter, 'sendApiUpdate').mockImplementation(jest.fn());
    jest.spyOn(client, 'request').mockResolvedValue({
      chat_id: 'chat-1',
      user_id: 'user-1',
      role: 'member',
      permissions: 0,
      joined_at: '2026-04-03T10:00:00.000Z',
      notification_level: 'all',
      display_name: 'Orbit',
      is_pinned: true,
      is_muted: false,
      is_archived: false,
    });

    await toggleChatPinned({
      chat: { id: 'chat-1' } as ApiChat,
      shouldBePinned: true,
    });

    expect(client.request).toHaveBeenCalledWith('PATCH', '/chats/chat-1/members/me', {
      is_pinned: true,
    });
    expect(sendApiUpdate).toHaveBeenCalledWith({
      '@type': 'updateChat',
      id: 'chat-1',
      chat: { isPinned: true },
    });
  });

  it('persists archived state before moving the chat to the archive folder', async () => {
    const sendApiUpdate = jest.spyOn(apiUpdateEmitter, 'sendApiUpdate').mockImplementation(jest.fn());
    jest.spyOn(client, 'request').mockResolvedValue({
      chat_id: 'chat-1',
      user_id: 'user-1',
      role: 'member',
      permissions: 0,
      joined_at: '2026-04-03T10:00:00.000Z',
      notification_level: 'all',
      display_name: 'Orbit',
      is_pinned: false,
      is_muted: false,
      is_archived: true,
    });

    await archiveChat({ chatId: 'chat-1' });

    expect(client.request).toHaveBeenCalledWith('PATCH', '/chats/chat-1/members/me', {
      is_archived: true,
    });
    expect(sendApiUpdate).toHaveBeenCalledWith({
      '@type': 'updateChatListType',
      id: 'chat-1',
      folderId: 1,
    });
  });

  it('persists muted state before emitting the local mute flag', async () => {
    const sendApiUpdate = jest.spyOn(apiUpdateEmitter, 'sendApiUpdate').mockImplementation(jest.fn());
    jest.spyOn(client, 'request').mockResolvedValue({
      chat_id: 'chat-1',
      user_id: 'user-1',
      role: 'member',
      permissions: 0,
      joined_at: '2026-04-03T10:00:00.000Z',
      notification_level: 'all',
      display_name: 'Orbit',
      is_pinned: false,
      is_muted: true,
      is_archived: false,
    });

    await setChatMuted({ chatId: 'chat-1', isMuted: true });

    expect(client.request).toHaveBeenCalledWith('PATCH', '/chats/chat-1/members/me', {
      is_muted: true,
    });
    expect(sendApiUpdate).toHaveBeenCalledWith({
      '@type': 'updateChat',
      id: 'chat-1',
      chat: { isMuted: true },
    });
  });
});
