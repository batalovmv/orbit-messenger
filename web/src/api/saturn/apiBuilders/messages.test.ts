import type { SaturnMessage } from '../types';

import { buildApiMessage, buildApiPoll, buildApiScheduledPoll } from './messages';
import { getRegisteredAsset } from './symbols';

describe('buildApiMessage', () => {
  it('keeps poll question as preview text fallback', () => {
    const apiMessage = buildApiMessage({
      id: 'message-1',
      chat_id: 'chat-1',
      sender_id: 'user-1',
      type: 'poll',
      is_edited: false,
      is_deleted: false,
      is_pinned: false,
      is_forwarded: false,
      sequence_number: 10,
      created_at: '2026-04-03T10:00:00.000Z',
      sender_name: 'Orbit',
      poll: {
        id: 'poll-1',
        message_id: 'message-1',
        question: 'Release at 17:00?',
        is_anonymous: false,
        is_multiple: false,
        is_quiz: false,
        is_closed: false,
        options: [
          {
            id: 'option-1',
            poll_id: 'poll-1',
            text: 'Yes',
            position: 0,
            voters: 0,
          },
        ],
        total_voters: 0,
        created_at: '2026-04-03T10:00:00.000Z',
      },
    } satisfies SaturnMessage);

    expect(apiMessage.content.pollId).toBe('poll-1');
    expect(apiMessage.content.text?.text).toBe('Release at 17:00?');
  });

  it('registers inbound photo attachments for media loading', () => {
    buildApiMessage({
      id: 'message-2',
      chat_id: 'chat-1',
      sender_id: 'user-1',
      type: 'message',
      content: 'media-viewer-audit',
      is_edited: false,
      is_deleted: false,
      is_pinned: false,
      is_forwarded: false,
      sequence_number: 11,
      created_at: '2026-04-03T10:05:00.000Z',
      sender_name: 'Orbit',
      media_attachments: [{
        media_id: 'photo-1',
        type: 'photo',
        mime_type: 'image/jpeg',
        url: '/media/photo-1',
        thumbnail_url: '/media/photo-1/thumbnail',
        size_bytes: 1024,
        width: 800,
        height: 600,
        position: 0,
        is_spoiler: false,
        is_one_time: false,
        processing_status: 'ready',
      }],
    } satisfies SaturnMessage);

    expect(getRegisteredAsset('photo-1')).toEqual(expect.objectContaining({
      fullUrl: expect.stringContaining('/media/photo-1'),
      previewUrl: expect.stringContaining('/media/photo-1/thumbnail'),
      mimeType: 'image/jpeg',
    }));
  });

  it('preserves explicit poll booleans for anonymous and closed states', () => {
    const apiPoll = buildApiPoll({
      id: 'poll-2',
      message_id: 'message-2',
      question: 'Anonymous?',
      is_anonymous: true,
      is_multiple: false,
      is_quiz: false,
      is_closed: true,
      options: [{
        id: 'option-1',
        poll_id: 'poll-2',
        text: 'Yes',
        position: 0,
        voters: 1,
      }],
      total_voters: 1,
      created_at: '2026-04-03T10:00:00.000Z',
    });

    expect(apiPoll).toEqual(expect.objectContaining({
      summary: expect.objectContaining({
        closed: true,
        isPublic: false,
        multipleChoice: false,
        quiz: false,
      }),
    }));
  });

  it('keeps scheduled poll booleans explicit', () => {
    const apiPoll = buildApiScheduledPoll({
      id: 'scheduled-1',
      chat_id: 'chat-1',
      sender_id: 'user-1',
      type: 'poll',
      content: '',
      media_attachments: [],
      poll: {
        question: 'Schedule?',
        options: ['Yes'],
        is_anonymous: true,
        is_multiple: false,
        is_quiz: false,
      },
      scheduled_at: '2026-04-03T12:00:00.000Z',
      is_sent: false,
      created_at: '2026-04-03T10:00:00.000Z',
      updated_at: '2026-04-03T10:00:00.000Z',
    });

    expect(apiPoll).toEqual(expect.objectContaining({
      summary: expect.objectContaining({
        closed: false,
        isPublic: false,
        multipleChoice: false,
        quiz: false,
      }),
    }));
  });
});
