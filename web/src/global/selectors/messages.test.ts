import type { GlobalState } from '../types';
import { MAIN_THREAD_ID } from '../../api/types';

import { INITIAL_GLOBAL_STATE } from '../initialState';
import { selectScheduledIds } from './messages';

describe('selectScheduledIds', () => {
  it('treats an empty loaded scheduled list as loaded, not loading', () => {
    const global = {
      ...INITIAL_GLOBAL_STATE,
      messages: {
        ...INITIAL_GLOBAL_STATE.messages,
        byChatId: {
          'chat-1': {
            byId: {},
            threadsById: {
              [MAIN_THREAD_ID]: {
                threadInfo: {
                  chatId: 'chat-1',
                  threadId: MAIN_THREAD_ID,
                  isCommentsInfo: false,
                },
                localState: {
                  scheduledIds: [],
                },
                readState: {},
              },
            },
            summaryById: {},
          },
        },
      },
    } as GlobalState;

    expect(selectScheduledIds(global, 'chat-1', MAIN_THREAD_ID)).toEqual([]);
  });

  it('derives an empty array from a loaded scheduled store without local thread state', () => {
    const global = {
      ...INITIAL_GLOBAL_STATE,
      scheduledMessages: {
        byChatId: {
          'chat-1': {
            byId: {},
            hash: 0,
          },
        },
      },
    } as GlobalState;

    expect(selectScheduledIds(global, 'chat-1', MAIN_THREAD_ID)).toEqual([]);
  });

  it('keeps returning undefined before scheduled history is loaded', () => {
    expect(selectScheduledIds(INITIAL_GLOBAL_STATE, 'chat-1', MAIN_THREAD_ID)).toBeUndefined();
  });
});
