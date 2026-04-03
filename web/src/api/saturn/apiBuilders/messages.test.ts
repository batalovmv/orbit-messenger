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
    expect(getRegisteredAsset('photo-1', 'photo')).toEqual(expect.objectContaining({
      fullUrl: expect.stringContaining('/media/photo-1'),
      previewUrl: expect.stringContaining('/media/photo-1/thumbnail'),
      mimeType: 'image/jpeg',
    }));
  });

  it('marks grouped Saturn media messages as album items', () => {
    const apiMessage = buildApiMessage({
      id: 'message-3',
      chat_id: 'chat-1',
      sender_id: 'user-1',
      type: 'message',
      content: '',
      is_edited: false,
      is_deleted: false,
      is_pinned: false,
      is_forwarded: false,
      grouped_id: 'group-1',
      sequence_number: 12,
      created_at: '2026-04-03T10:06:00.000Z',
      sender_name: 'Orbit',
      media_attachments: [{
        media_id: 'photo-2',
        type: 'photo',
        mime_type: 'image/jpeg',
        url: '/media/photo-2',
        thumbnail_url: '/media/photo-2/thumbnail',
        size_bytes: 1024,
        width: 800,
        height: 600,
        position: 0,
        is_spoiler: false,
        is_one_time: false,
        processing_status: 'ready',
      }],
    } satisfies SaturnMessage);

    expect(apiMessage.groupedId).toBe('group-1');
    expect(apiMessage.isInAlbum).toBe(true);
  });

  it('treats image attachments with file type as photos and registers them for photo loading', () => {
    const apiMessage = buildApiMessage({
      id: 'message-3',
      chat_id: 'chat-1',
      sender_id: 'user-1',
      type: 'message',
      content: 'caption',
      is_edited: false,
      is_deleted: false,
      is_pinned: false,
      is_forwarded: false,
      sequence_number: 12,
      created_at: '2026-04-03T10:06:00.000Z',
      sender_name: 'Orbit',
      media_attachments: [{
        media_id: 'photo-caption-1',
        type: 'file',
        mime_type: 'image/jpeg',
        url: 'https://r2.example.com/expired.jpg',
        thumbnail_url: 'https://r2.example.com/expired-thumb.jpg',
        size_bytes: 2048,
        width: 1024,
        height: 768,
        position: 0,
        is_spoiler: false,
        is_one_time: false,
        processing_status: 'ready',
      }],
    } satisfies SaturnMessage);

    expect(apiMessage.content.photo).toEqual(expect.objectContaining({
      id: 'photo-caption-1',
      mediaType: 'photo',
    }));
    expect(apiMessage.content.document).toBeUndefined();
    expect(getRegisteredAsset('photo-caption-1', 'photo')).toEqual(expect.objectContaining({
      fullUrl: expect.stringContaining('/media/photo-caption-1'),
      previewUrl: expect.stringContaining('/media/photo-caption-1/thumbnail'),
      mimeType: 'image/jpeg',
    }));
  });

  it('keeps webm sticker attachments as video stickers', () => {
    const apiMessage = buildApiMessage({
      id: 'message-sticker-video',
      chat_id: 'chat-1',
      sender_id: 'user-1',
      type: 'sticker',
      content: '',
      is_edited: false,
      is_deleted: false,
      is_pinned: false,
      is_forwarded: false,
      sequence_number: 13,
      created_at: '2026-04-03T10:07:00.000Z',
      sender_name: 'Orbit',
      media_attachments: [{
        media_id: 'sticker-video-1',
        type: 'sticker',
        mime_type: 'video/webm',
        url: '/media/sticker-video-1',
        thumbnail_url: '/media/sticker-video-1/thumbnail',
        size_bytes: 4096,
        width: 512,
        height: 512,
        position: 0,
        is_spoiler: false,
        is_one_time: false,
        processing_status: 'ready',
      }],
    } satisfies SaturnMessage);

    expect(apiMessage.content.sticker).toEqual(expect.objectContaining({
      id: 'sticker-video-1',
      isVideo: true,
      isLottie: false,
    }));
    expect(getRegisteredAsset('sticker-video-1', 'sticker')).toEqual(expect.objectContaining({
      mimeType: 'video/webm',
      previewUrl: expect.stringContaining('/media/sticker-video-1/thumbnail'),
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

  it('maps quiz solution and entities from Saturn polls', () => {
    const apiPoll = buildApiPoll({
      id: 'poll-3',
      message_id: 'message-3',
      question: '2+2?',
      is_anonymous: true,
      is_multiple: false,
      is_quiz: true,
      correct_option: 1,
      solution: 'Because 2 plus 2 equals 4.',
      solution_entities: [{
        type: 'MessageEntityItalic',
        offset: 0,
        length: 7,
      }],
      is_closed: false,
      options: [
        {
          id: 'option-1',
          poll_id: 'poll-3',
          text: '3',
          position: 0,
          voters: 0,
        },
        {
          id: 'option-2',
          poll_id: 'poll-3',
          text: '4',
          position: 1,
          voters: 1,
          is_correct: true,
        },
      ],
      total_voters: 1,
      created_at: '2026-04-03T10:00:00.000Z',
    });

    expect(apiPoll?.results.solution).toBe('Because 2 plus 2 equals 4.');
    expect(apiPoll?.results.solutionEntities).toEqual([{
      type: 'MessageEntityItalic',
      offset: 0,
      length: 7,
    }]);
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
