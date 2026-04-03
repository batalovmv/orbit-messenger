import type { SaturnChatListItem, SaturnPaginatedResponse } from '../types';

import * as client from '../client';
import { fetchChats } from './chats';

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
        is_encrypted: false,
        max_members: 100,
        created_at: '2026-04-03T10:00:00.000Z',
        updated_at: '2026-04-03T10:00:00.000Z',
        default_permissions: 255,
        slow_mode_seconds: 0,
        is_signatures: false,
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
    expect(result.messages[0].chatId).toBe('chat-1');
    expect(result.messages[0].content.pollId).toBe('poll-1');
    expect(result.messages[0].content.text?.text).toBe('Ship the release?');
  });
});
