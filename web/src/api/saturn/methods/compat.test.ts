// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import { ARCHIVED_FOLDER_ID } from '../../../config';
import * as client from '../client';
import * as apiUpdateEmitter from '../updates/apiUpdateEmitter';
import * as chats from './chats';
import {
  editChatPhoto,
  fetchProfilePhotos,
  toggleChatArchived,
  uploadProfilePhoto,
} from './compat';
import * as media from './media';

describe('saturn compat methods', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('maps editChatPhoto file uploads to Saturn chat photo updates', async () => {
    const sendApiUpdate = jest.spyOn(apiUpdateEmitter, 'sendApiUpdate').mockImplementation(jest.fn());
    const file = new File(['avatar'], 'avatar.png', { type: 'image/png' });

    jest.spyOn(media, 'uploadMedia').mockReturnValue({
      abort: jest.fn(),
      response: Promise.resolve({
        id: 'media-1',
        type: 'image',
        mime_type: 'image/png',
        size_bytes: file.size,
        processing_status: 'completed',
        url: 'https://cdn.example.com/chat-avatar.png',
      }),
      uploadId: 'upload-1',
    });
    const updateChatPhoto = jest.spyOn(media, 'updateChatPhoto').mockResolvedValue(undefined);

    const result = await editChatPhoto({
      chatId: 'chat-1',
      photo: file,
    });

    expect(result).toBe(true);
    expect(updateChatPhoto).toHaveBeenCalledWith('chat-1', 'https://cdn.example.com/chat-avatar.png');
    expect(sendApiUpdate).toHaveBeenNthCalledWith(1, {
      '@type': 'updateChat',
      id: 'chat-1',
      chat: {
        avatarPhotoId: expect.stringContaining('avatar-chat-1-'),
      },
    });
    expect(sendApiUpdate).toHaveBeenNthCalledWith(2, {
      '@type': 'updateChatFullInfo',
      id: 'chat-1',
      fullInfo: {
        profilePhoto: expect.objectContaining({
          id: expect.stringContaining('avatar-chat-1-'),
        }),
      },
    });
  });

  it('maps empty editChatPhoto payloads to Saturn chat photo deletion', async () => {
    const sendApiUpdate = jest.spyOn(apiUpdateEmitter, 'sendApiUpdate').mockImplementation(jest.fn());
    const deleteChatPhoto = jest.spyOn(media, 'deleteChatPhoto').mockResolvedValue(undefined);

    const result = await editChatPhoto({ chatId: 'chat-1' });

    expect(result).toBe(true);
    expect(deleteChatPhoto).toHaveBeenCalledWith('chat-1');
    expect(sendApiUpdate).toHaveBeenCalledTimes(2);
  });

  it('maps uploadProfilePhoto to media upload plus /users/me avatar update', async () => {
    const sendApiUpdate = jest.spyOn(apiUpdateEmitter, 'sendApiUpdate').mockImplementation(jest.fn());
    const file = new File(['avatar'], 'profile.png', { type: 'image/png' });

    jest.spyOn(media, 'uploadMedia').mockReturnValue({
      abort: jest.fn(),
      response: Promise.resolve({
        id: 'media-1',
        type: 'image',
        mime_type: 'image/png',
        size_bytes: file.size,
        processing_status: 'completed',
        url: 'https://cdn.example.com/profile.png',
      }),
      uploadId: 'upload-1',
    });
    const request = jest.spyOn(client, 'request').mockResolvedValue({
      id: 'user-1',
      email: 'orbit@example.com',
      display_name: 'Orbit QA',
      avatar_url: 'https://cdn.example.com/profile.png',
      status: 'online',
      role: 'member',
      created_at: '2026-04-03T10:00:00.000Z',
      updated_at: '2026-04-03T10:00:00.000Z',
    });

    const result = await uploadProfilePhoto(file);

    expect(request).toHaveBeenCalledWith('PUT', '/users/me', {
      avatar_url: 'https://cdn.example.com/profile.png',
    });
    expect(sendApiUpdate).toHaveBeenCalledWith(expect.objectContaining({
      '@type': 'updateCurrentUser',
      currentUser: expect.objectContaining({
        id: 'user-1',
        avatarPhotoId: expect.stringContaining('avatar-user-1-'),
      }),
    }));
    expect(result).toEqual({
      photo: expect.objectContaining({
        id: expect.stringContaining('avatar-user-1-'),
      }),
    });
  });

  it('derives toggleChatArchived intent from folderId and boolean flags', async () => {
    const archiveChat = jest.spyOn(chats, 'archiveChat').mockResolvedValue(undefined);
    const unarchiveChat = jest.spyOn(chats, 'unarchiveChat').mockResolvedValue(undefined);

    await toggleChatArchived({
      chat: { id: 'chat-1' } as any,
      folderId: ARCHIVED_FOLDER_ID,
    });
    await toggleChatArchived({
      chatId: 'chat-1',
      isArchived: false,
    });

    expect(archiveChat).toHaveBeenCalledWith({ chatId: 'chat-1' });
    expect(unarchiveChat).toHaveBeenCalledWith({ chatId: 'chat-1' });
  });

  it('returns the current synthetic avatar as a single Saturn profile photo', async () => {
    const result = await fetchProfilePhotos({
      peer: {
        id: 'user-1',
        avatarPhotoId: 'avatar-user-1-abc123',
      } as any,
    });

    expect(result).toEqual({
      photos: [expect.objectContaining({
        id: 'avatar-user-1-abc123',
      })],
      count: 1,
      nextOffsetId: undefined,
    });
  });
});
