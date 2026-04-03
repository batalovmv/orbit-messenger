import type { ApiChat, ApiMessage } from '../../types';
import type { SaturnMessage } from '../types';

import * as client from '../client';
import * as apiUpdateEmitter from '../updates/apiUpdateEmitter';
import { editMessage, setCurrentUserId } from './messages';

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
