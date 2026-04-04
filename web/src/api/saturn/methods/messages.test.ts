import type { ApiChat, ApiMessage } from '../../types';
import type { SaturnMessage, SaturnPoll } from '../types';

import { registerMessageId } from '../apiBuilders/messages';
import * as client from '../client';
import * as apiUpdateEmitter from '../updates/apiUpdateEmitter';
import {
  editMessage,
  sendPollVote,
  setCurrentUserId,
} from './messages';

describe('editMessage', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('uses chat/message payload and saturnId when patching a message', async () => {
    const sendApiUpdate = jest.spyOn(apiUpdateEmitter, 'sendApiUpdate').mockImplementation(jest.fn());
    jest.spyOn(client, 'request').mockResolvedValue({
      id: 'message-uuid',
      chat_id: 'chat-1',
      sender_id: 'user-1',
      type: 'text',
      content: 'updated',
      entities: [],
      is_edited: true,
      is_deleted: false,
      is_pinned: false,
      is_forwarded: false,
      sequence_number: 11,
      created_at: '2026-04-03T10:00:00.000Z',
      sender_name: 'Orbit',
    } satisfies SaturnMessage);

    setCurrentUserId('user-1');

    const chat = { id: 'chat-1' } as ApiChat;
    const message = {
      id: 11,
      chatId: 'chat-1',
      saturnId: 'message-uuid',
      date: 1,
      isOutgoing: false,
      content: {
        text: {
          text: 'original',
          entities: [],
        },
      },
    } as ApiMessage;

    const result = await editMessage({
      chat,
      message,
      text: 'updated',
    });

    expect(client.request).toHaveBeenCalledWith('PATCH', '/messages/message-uuid', {
      content: 'updated',
    });
    expect(sendApiUpdate).toHaveBeenNthCalledWith(1, expect.objectContaining({
      '@type': 'updateMessage',
      chatId: 'chat-1',
      id: 11,
      isFull: true,
      message: expect.objectContaining({
        content: expect.objectContaining({
          text: {
            text: 'updated',
            entities: [],
          },
        }),
      }),
    }));
    expect(sendApiUpdate).toHaveBeenNthCalledWith(2, expect.objectContaining({
      '@type': 'updateMessage',
      chatId: 'chat-1',
      id: 11,
      isFull: true,
      message: expect.objectContaining({
        id: 11,
        saturnId: 'message-uuid',
      }),
    }));
    expect(result?.content.text?.text).toBe('updated');
    expect(result?.saturnId).toBe('message-uuid');
  });

  it('rolls back the optimistic edit when patching fails', async () => {
    const sendApiUpdate = jest.spyOn(apiUpdateEmitter, 'sendApiUpdate').mockImplementation(jest.fn());
    jest.spyOn(client, 'request').mockRejectedValue(new Error('Forbidden'));

    const chat = { id: 'chat-1' } as ApiChat;
    const message = {
      id: 11,
      chatId: 'chat-1',
      saturnId: 'message-uuid',
      date: 1,
      isOutgoing: false,
      content: {
        text: {
          text: 'original',
          entities: [],
        },
      },
    } as ApiMessage;

    const result = await editMessage({
      chat,
      message,
      text: 'updated',
    });

    expect(result).toBeUndefined();
    expect(sendApiUpdate).toHaveBeenNthCalledWith(1, expect.objectContaining({
      '@type': 'updateMessage',
      id: 11,
      chatId: 'chat-1',
    }));
    expect(sendApiUpdate).toHaveBeenNthCalledWith(2, {
      '@type': 'error',
      error: {
        message: 'Forbidden',
        hasErrorKey: true,
      },
    });
    expect(sendApiUpdate).toHaveBeenNthCalledWith(3, {
      '@type': 'updateMessage',
      chatId: 'chat-1',
      id: 11,
      isFull: true,
      message,
    });
  });
});

describe('sendPollVote', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('applies the server poll state without an extra optimistic increment', async () => {
    const sendApiUpdate = jest.spyOn(apiUpdateEmitter, 'sendApiUpdate').mockImplementation(jest.fn());
    jest.spyOn(client, 'request').mockResolvedValue({
      id: 'poll-1',
      message_id: 'message-uuid',
      question: 'Everything synced?',
      is_anonymous: true,
      is_multiple: false,
      is_quiz: false,
      is_closed: false,
      total_voters: 1,
      created_at: '2026-04-04T07:41:48.194699Z',
      options: [
        {
          id: 'option-1',
          poll_id: 'poll-1',
          text: 'Yes',
          position: 0,
          voters: 1,
          is_chosen: true,
        },
        {
          id: 'option-2',
          poll_id: 'poll-1',
          text: 'No',
          position: 1,
          voters: 0,
        },
      ],
    } satisfies SaturnPoll);

    setCurrentUserId('user-1');
    registerMessageId('chat-1', 'message-uuid', 15);

    const result = await sendPollVote({
      chat: { id: 'chat-1' } as ApiChat,
      messageId: 15,
      options: ['option-1'],
    });

    expect(result).toBe(true);
    expect(client.request).toHaveBeenCalledWith('POST', '/messages/message-uuid/poll/vote', {
      option_ids: ['option-1'],
    });
    expect(sendApiUpdate).toHaveBeenCalledTimes(1);
    expect(sendApiUpdate).toHaveBeenCalledWith(expect.objectContaining({
      '@type': 'updateMessagePoll',
      pollId: 'poll-1',
      pollUpdate: expect.objectContaining({
        results: expect.objectContaining({
          totalVoters: 1,
          results: expect.arrayContaining([
            expect.objectContaining({
              option: 'option-1',
              votersCount: 1,
              isChosen: true,
            }),
          ]),
        }),
      }),
    }));
  });
});
