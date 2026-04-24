// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import type { ApiChat } from '../../types';
import type { SaturnPaginatedResponse, SaturnReaction, SaturnReactionSummary } from '../types';

import * as client from '../client';
import * as messagesMethods from './messages';
import { fetchMessageReactionsList } from './reactions';

describe('fetchMessageReactionsList', () => {
  const originalGetGlobal = (window as any).getGlobal;

  beforeEach(() => {
    (window as any).getGlobal = () => ({ currentUserId: 'user-1' });
  });

  afterEach(() => {
    if (originalGetGlobal) {
      (window as any).getGlobal = originalGetGlobal;
    } else {
      delete (window as any).getGlobal;
    }
    jest.restoreAllMocks();
  });

  it('loads the full reactor list without forcing an emoji filter and hydrates users', async () => {
    jest.spyOn(messagesMethods, 'resolveMessageUuid').mockReturnValue('message-uuid');
    const request = jest.spyOn(client, 'request').mockImplementation((_method, path) => {
      if (path === '/messages/message-uuid/reactions/users?limit=50') {
        return Promise.resolve({
          data: [
            {
              message_id: 'message-uuid',
              user_id: 'user-2',
              emoji: '👍',
              created_at: '2026-04-03T10:00:00.000Z',
              display_name: 'Orbit QA',
              avatar_url: '/media/avatar-2.jpg',
            },
            {
              message_id: 'message-uuid',
              user_id: 'user-3',
              emoji: '❤️',
              created_at: '2026-04-03T09:00:00.000Z',
              display_name: 'Jane Doe',
            },
          ],
          cursor: 'cursor-2',
          has_more: true,
        } satisfies SaturnPaginatedResponse<SaturnReaction> as never);
      }

      if (path === '/messages/message-uuid/reactions') {
        return Promise.resolve([
          { emoji: '👍', count: 2, user_ids: ['user-1', 'user-2'] },
          { emoji: '❤️', count: 1, user_ids: ['user-3'] },
        ] satisfies SaturnReactionSummary[] as never);
      }

      return Promise.reject(new Error(`Unexpected request path: ${path}`));
    });

    const result = await fetchMessageReactionsList({
      chat: { id: 'chat-1' } as ApiChat,
      messageId: 42,
    });

    expect(request).toHaveBeenCalledTimes(2);
    expect(result).toEqual(expect.objectContaining({
      count: 3,
      nextOffset: 'cursor-2',
      reactions: [
        expect.objectContaining({
          peerId: 'user-2',
          reaction: { type: 'emoji', emoticon: '👍' },
          addedDate: 1775210400,
        }),
        expect.objectContaining({
          peerId: 'user-3',
          reaction: { type: 'emoji', emoticon: '❤️' },
          addedDate: 1775206800,
        }),
      ],
      users: [
        expect.objectContaining({
          id: 'user-2',
          isMin: true,
          type: 'userTypeRegular',
          firstName: 'Orbit',
          lastName: 'QA',
          phoneNumber: '',
          avatarPhotoId: expect.stringContaining('avatar-user-2-'),
        }),
        expect.objectContaining({
          id: 'user-3',
          isMin: true,
          type: 'userTypeRegular',
          firstName: 'Jane',
          lastName: 'Doe',
          phoneNumber: '',
        }),
      ],
    }));
  });

  it('passes the emoji filter through when a specific reaction tab is requested', async () => {
    jest.spyOn(messagesMethods, 'resolveMessageUuid').mockReturnValue('message-uuid');
    const request = jest.spyOn(client, 'request').mockImplementation((_method, path) => {
      if (path === '/messages/message-uuid/reactions/users?limit=50&emoji=%F0%9F%91%8D') {
        return Promise.resolve({
          data: [],
          has_more: false,
        } satisfies SaturnPaginatedResponse<SaturnReaction> as never);
      }

      if (path === '/messages/message-uuid/reactions') {
        return Promise.resolve([
          { emoji: '👍', count: 4, user_ids: ['user-1'] },
        ] satisfies SaturnReactionSummary[] as never);
      }

      return Promise.reject(new Error(`Unexpected request path: ${path}`));
    });

    const result = await fetchMessageReactionsList({
      chat: { id: 'chat-1' } as ApiChat,
      messageId: 42,
      reaction: { type: 'emoji', emoticon: '👍' },
    });

    expect(request).toHaveBeenCalledTimes(2);
    expect(result).toEqual(expect.objectContaining({
      count: 4,
      reactions: [],
      users: [],
    }));
  });
});
