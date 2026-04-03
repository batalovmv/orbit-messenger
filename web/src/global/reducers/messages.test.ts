import type { ApiMessage, ApiPoll } from '../../api/types';

jest.mock('../../util/mediaLoader', () => ({
  unload: jest.fn(),
}));

import { MAIN_THREAD_ID } from '../../api/types';

import { buildMessageKey } from '../../util/keys/messageKey';
import { INITIAL_GLOBAL_STATE } from '../initialState';
import {
  clearUploadByMessage,
  replaceScheduledMessages,
  updateChatMessage,
  updatePoll,
} from './messages';

function buildPoll(): ApiPoll {
  return {
    mediaType: 'poll',
    id: 'poll-1',
    summary: {
      question: {
        text: 'Question',
        entities: [],
      },
      answers: [
        {
          option: 'option-1',
          text: {
            text: 'Option 1',
            entities: [],
          },
        },
      ],
      isPublic: true,
    },
    results: {
      totalVoters: 3,
      results: [
        {
          option: 'option-1',
          votersCount: 3,
        },
      ],
    },
  };
}

describe('updatePoll', () => {
  it('preserves full summary on partial updates', () => {
    const poll = buildPoll();
    const global = {
      ...INITIAL_GLOBAL_STATE,
      messages: {
        ...INITIAL_GLOBAL_STATE.messages,
        pollById: {
          [poll.id]: poll,
        },
      },
    };

    const updated = updatePoll(global, poll.id, {
      id: poll.id,
      summary: {
        closed: true,
      },
    });

    expect(updated.messages.pollById[poll.id].summary.closed).toBe(true);
    expect(updated.messages.pollById[poll.id].summary.question.text).toBe('Question');
    expect(updated.messages.pollById[poll.id].summary.answers).toHaveLength(1);
    expect(updated.messages.pollById[poll.id].summary.answers[0].text.text).toBe('Option 1');
  });

  it('ignores incomplete poll creation from partial updates', () => {
    const updated = updatePoll(INITIAL_GLOBAL_STATE, 'missing-poll', {
      id: 'missing-poll',
      summary: {
        closed: true,
      },
    });

    expect(updated).toBe(INITIAL_GLOBAL_STATE);
    expect(updated.messages.pollById['missing-poll']).toBeUndefined();
  });
});

describe('updateChatMessage', () => {
  it('preserves existing reactions when incoming update does not include them', () => {
    const message: ApiMessage = {
      id: 1,
      chatId: 'chat-1',
      date: 1,
      isOutgoing: true,
      content: {
        text: {
          text: 'Hello',
          entities: [],
        },
      },
      reactions: {
        canSeeList: true,
        results: [
          {
            count: 1,
            chosenOrder: 0,
            reaction: {
              type: 'emoji',
              emoticon: '❤️',
            },
          },
        ],
      },
    };

    const global = {
      ...INITIAL_GLOBAL_STATE,
      messages: {
        ...INITIAL_GLOBAL_STATE.messages,
        byChatId: {
          'chat-1': {
            byId: {
              1: message,
            },
            threadsById: {},
            summaryById: {},
          },
        },
      },
    };

    const updated = updateChatMessage(global, 'chat-1', 1, {
      isEdited: true,
      content: {
        text: {
          text: 'Edited',
          entities: [],
        },
      },
      reactions: undefined,
    });

    expect(updated.messages.byChatId['chat-1'].byId[1].reactions).toEqual(message.reactions);
    expect(updated.messages.byChatId['chat-1'].byId[1].content.text?.text).toBe('Edited');
  });
});

describe('clearUploadByMessage', () => {
  it('removes stale upload progress for both local and server message keys', () => {
    const chatId = 'chat-1';
    const localId = 1001.5;
    const serverId = 7;
    const localKey = buildMessageKey(chatId, localId);
    const serverKey = buildMessageKey(chatId, serverId);
    const unrelatedKey = buildMessageKey('chat-2', 1);

    const global = {
      ...INITIAL_GLOBAL_STATE,
      fileUploads: {
        byMessageKey: {
          [localKey]: { progress: 0 },
          [serverKey]: { progress: 100 },
          [unrelatedKey]: { progress: 55 },
        },
      },
    };

    const updated = clearUploadByMessage(global, {
      chatId,
      id: serverId,
      previousLocalId: localId,
    }, localId);

    expect(updated.fileUploads.byMessageKey[localKey]).toBeUndefined();
    expect(updated.fileUploads.byMessageKey[serverKey]).toBeUndefined();
    expect(updated.fileUploads.byMessageKey[unrelatedKey]).toEqual({ progress: 55 });
  });
});

describe('replaceScheduledMessages', () => {
  it('replaces stale scheduled messages for a chat instead of merging them', () => {
    const chatId = 'chat-1';
    const staleMessage: ApiMessage = {
      id: -1,
      chatId,
      date: 1,
      isOutgoing: true,
      isScheduled: true,
      content: {
        text: {
          text: 'stale scheduled text',
          entities: [],
        },
      },
    };
    const freshMessage: ApiMessage = {
      id: -2,
      chatId,
      date: 2,
      isOutgoing: true,
      isScheduled: true,
      content: {
        text: {
          text: 'fresh scheduled poll',
          entities: [],
        },
        pollId: 'scheduled-poll-1',
      },
    };

    const global = {
      ...INITIAL_GLOBAL_STATE,
      messages: {
        ...INITIAL_GLOBAL_STATE.messages,
        byChatId: {
          [chatId]: {
            byId: {},
            threadsById: {
              [MAIN_THREAD_ID]: {
                localState: {
                  scheduledIds: [staleMessage.id],
                },
                readState: {},
                threadInfo: {
                  chatId,
                  isCommentsInfo: false,
                  threadId: MAIN_THREAD_ID,
                },
              },
            },
            summaryById: {},
          },
        },
      },
      scheduledMessages: {
        byChatId: {
          [chatId]: {
            byId: {
              [staleMessage.id]: staleMessage,
            },
          },
        },
      },
    } as typeof INITIAL_GLOBAL_STATE;

    const updated = replaceScheduledMessages(global, chatId, {
      [freshMessage.id]: freshMessage,
    });

    expect(updated.scheduledMessages.byChatId[chatId].byId).toEqual({
      [freshMessage.id]: freshMessage,
    });
  });
});
