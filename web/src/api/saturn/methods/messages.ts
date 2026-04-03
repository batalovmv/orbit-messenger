import type {
  SaturnMessage,
  SaturnPaginatedResponse,
  SaturnPoll,
  SaturnPollVote,
  SaturnScheduledMessage,
  SaturnSharedMediaItem,
} from '../types';
import {
  type ApiAttachment,
  type ApiChat,
  type ApiMessage,
  type ApiMessageEntity,
  type ApiNewPoll,
  type ApiPeer,
  type ApiPoll,
  type ApiSendMessageAction,
  type ApiSticker,
  type ApiVideo,
  MESSAGE_DELETED,
} from '../../types';

import { buildMessageKey } from '../../../util/keys/messageKey';
import {
  buildApiMessage,
  buildApiPoll,
  buildApiScheduledMessage,
  buildApiScheduledPoll,
  buildSaturnEntities,
  getMessageUuid,
  getScheduledMessageUuid,
  setMessageBuilderCurrentUserId,
} from '../apiBuilders/messages';
import {
  serializeGifForMessage,
  serializeStickerForMessage,
} from '../apiBuilders/symbols';
import * as client from '../client';
import { sendApiUpdate, sendImmediateApiUpdate } from '../updates/apiUpdateEmitter';
import { trackPendingSend } from '../updates/wsHandler';
import { uploadMedia } from './media';

let currentUserId: string | undefined;
let localMessageCounter = 0;

const LOCAL_MESSAGES_LIMIT = 1e6;
const sharedMediaCursors = new Map<string, string>();
const SHARED_MEDIA_TYPE_MAP: Record<string, string> = {
  media: 'media',
  documents: 'file',
  voice: 'voice',
  gif: 'gif',
};

type PollSendResponse = {
  message: SaturnMessage;
  poll: SaturnPoll;
};
type SendProgressCallback = ((progress: number, key: string) => void) & {
  abort?: NoneToVoidFunction;
  isCanceled?: boolean;
};

function extractApiPolls(messages: SaturnMessage[]) {
  const pollsById: Record<string, ApiPoll> = {};

  messages.forEach((message) => {
    const poll = buildApiPoll(message.poll);
    if (poll) {
      pollsById[poll.id] = poll;
    }
  });

  return Object.values(pollsById);
}

function extractScheduledApiPolls(messages: SaturnScheduledMessage[]) {
  const pollsById: Record<string, ApiPoll> = {};

  messages.forEach((message) => {
    const poll = buildApiScheduledPoll(message);
    if (poll) {
      pollsById[poll.id] = poll;
    }
  });

  return Object.values(pollsById);
}

function getGlobalState() {
  return (window as any).getGlobal?.();
}

function getGlobalMessage(chatId: string, messageId: number) {
  const global = getGlobalState();
  return global?.messages?.byChatId?.[chatId]?.byId?.[messageId]
    || global?.scheduledMessages?.byChatId?.[chatId]?.[messageId];
}

function getLocalId(lastMessageId?: number) {
  return (lastMessageId || Math.floor(Date.now() / 1000)) + (++localMessageCounter / LOCAL_MESSAGES_LIMIT);
}

function buildLocalPoll(localId: number, poll: ApiNewPoll): ApiPoll {
  return {
    mediaType: 'poll',
    id: `local-poll-${localId}`,
    summary: {
      closed: undefined,
      isPublic: poll.summary.isPublic,
      multipleChoice: poll.summary.multipleChoice,
      quiz: poll.summary.quiz,
      question: {
        text: poll.summary.question.text,
        entities: poll.summary.question.entities || [],
      },
      answers: poll.summary.answers,
      closeDate: poll.summary.closeDate,
      closePeriod: poll.summary.closePeriod,
    },
    results: {
      results: poll.summary.answers.map((answer) => ({
        option: answer.option,
        votersCount: 0,
      })),
      totalVoters: 0,
      solution: poll.quiz?.solution,
      solutionEntities: poll.quiz?.solutionEntities,
    },
  };
}

function buildLocalMessageContent({
  attachment,
  gif,
  poll,
  sticker,
  text,
  entities,
}: {
  attachment?: ApiAttachment;
  gif?: ApiVideo;
  poll?: ApiNewPoll;
  sticker?: ApiSticker;
  text?: string;
  entities?: ApiMessageEntity[];
}) {
  const content: ApiMessage['content'] = {
    text: text ? { text, entities: entities || [] } : undefined,
  };

  if (attachment) {
    buildAttachmentContent(content, attachment);
  }

  if (sticker) {
    content.sticker = sticker;
    content.text = undefined;
  }

  if (gif) {
    content.video = gif;
    content.text = undefined;
  }

  if (poll) {
    content.pollId = '';
    content.text = undefined;
  }

  return content;
}

function buildAttachmentContent(content: ApiMessage['content'], attachment: ApiAttachment) {
  const mediaType = detectMediaType(attachment);
  const localId = `local-${Date.now()}`;
  const { quick } = attachment;

  if (attachment.ttlSeconds !== undefined && attachment.ttlSeconds > 0 && mediaType !== 'file') {
    content.ttlSeconds = 1;
  }

  switch (mediaType) {
    case 'photo':
      content.photo = {
        mediaType: 'photo',
        id: localId,
        date: 0,
        thumbnail: attachment.previewBlobUrl ? {
          dataUri: attachment.previewBlobUrl,
          width: quick?.width || 320,
          height: quick?.height || 320,
        } : undefined,
        sizes: [{
          width: quick?.width || 320,
          height: quick?.height || 320,
          type: 's',
        }, {
          width: quick?.width || 800,
          height: quick?.height || 800,
          type: 'y',
        }],
        blobUrl: attachment.compressedBlobUrl || attachment.blobUrl,
        isSpoiler: attachment.shouldSendAsSpoiler || undefined,
      };
      break;
    case 'video':
      content.video = {
        mediaType: 'video',
        id: localId,
        mimeType: attachment.mimeType || 'video/mp4',
        duration: quick?.duration || 0,
        width: quick?.width || 0,
        height: quick?.height || 0,
        fileName: attachment.filename || 'video.mp4',
        size: attachment.size,
        thumbnail: attachment.previewBlobUrl ? {
          dataUri: attachment.previewBlobUrl,
          width: quick?.width || 320,
          height: quick?.height || 320,
        } : undefined,
        blobUrl: attachment.blobUrl,
        isSpoiler: attachment.shouldSendAsSpoiler || undefined,
      };
      break;
    case 'voice':
      content.voice = {
        mediaType: 'voice',
        id: localId,
        duration: attachment.voice?.duration || 0,
        waveform: attachment.voice?.waveform || [],
        size: attachment.size,
      };
      break;
    case 'gif':
      content.video = {
        mediaType: 'video',
        id: localId,
        mimeType: 'video/mp4',
        duration: quick?.duration || 0,
        width: quick?.width || 0,
        height: quick?.height || 0,
        fileName: 'animation.mp4',
        size: attachment.size,
        isGif: true,
      };
      break;
    default:
      content.document = {
        mediaType: 'document',
        id: localId,
        blobUrl: attachment.blobUrl,
        mimeType: attachment.mimeType || 'application/octet-stream',
        fileName: attachment.filename || 'file',
        pageCount: attachment.pageCount,
        previewBlobUrl: attachment.previewBlobUrl,
        size: attachment.size,
      };
      break;
  }
}

function isAlbumAttachment(attachment?: ApiAttachment) {
  if (!attachment) {
    return false;
  }

  const mediaType = detectMediaType(attachment);
  return mediaType === 'photo' || mediaType === 'video';
}

function buildSendBody({
  attachment,
  entities,
  gif,
  groupedId,
  isSpoiler,
  poll,
  replyInfo,
  sticker,
  text,
  chatId,
}: {
  attachment?: ApiAttachment;
  entities?: ApiMessageEntity[];
  gif?: ApiVideo;
  groupedId?: string;
  isSpoiler?: boolean;
  poll?: ApiNewPoll;
  replyInfo?: { replyToMsgId?: number; type?: string };
  sticker?: ApiSticker;
  text?: string;
  chatId: string;
}) {
  const body: Record<string, unknown> = {};

  if (poll) {
    body.type = 'poll';
    body.question = poll.summary.question.text;
    body.options = poll.summary.answers.map((answer) => answer.text.text);
    body.is_anonymous = poll.summary.isPublic ? false : true;
    body.is_multiple = Boolean(poll.summary.multipleChoice);
    body.is_quiz = Boolean(poll.summary.quiz);
    if (poll.quiz?.solution) {
      body.solution = poll.quiz.solution;
    }
    if (poll.quiz?.solutionEntities?.length) {
      body.solution_entities = buildSaturnEntities(poll.quiz.solutionEntities);
    }
    if (poll.quiz?.correctAnswers?.length) {
      body.correct_option = poll.summary.answers.findIndex((answer) => (
        poll.quiz?.correctAnswers.includes(answer.option)
      ));
    }
    return body;
  }

  body.content = text || '';

  if (entities?.length) {
    body.entities = buildSaturnEntities(entities);
  }

  if (attachment) {
    body.type = detectMediaType(attachment);
  }

  if (sticker) {
    body.type = 'sticker';
    body.content = serializeStickerForMessage(sticker);
  }

  if (gif) {
    body.type = 'gif';
    body.content = serializeGifForMessage(gif);
  }

  if ((isSpoiler || attachment?.shouldSendAsSpoiler) && !sticker && !gif) {
    body.is_spoiler = true;
  }

  if (attachment && attachment.ttlSeconds !== undefined && attachment.ttlSeconds > 0
    && detectMediaType(attachment) !== 'file' && !sticker && !gif) {
    body.is_one_time = true;
  }

  const replyToId = replyInfo?.type === 'message' ? replyInfo.replyToMsgId : undefined;
  if (replyToId) {
    const replyUuid = resolveMessageUuid(chatId, replyToId);
    if (replyUuid) {
      body.reply_to_id = replyUuid;
    }
  }

  if (groupedId) {
    body.grouped_id = groupedId;
  }

  return body;
}

async function uploadAttachmentIfNeeded(
  attachment?: ApiAttachment,
  progressCallback?: SendProgressCallback,
  chatId?: string,
  localId?: number,
) {
  if (!attachment) {
    return [];
  }

  const mediaType = detectMediaType(attachment);
  const uploadId = chatId && localId !== undefined ? buildMessageKey(chatId, localId) : undefined;
  const onProgress = progressCallback && chatId && localId
    ? (loaded: number, total: number) => {
      progressCallback(total > 0 ? loaded / total : 0, uploadId!);
    }
    : undefined;

  const upload = uploadMedia(
    attachment.blob,
    mediaType,
    onProgress,
    attachment.ttlSeconds !== undefined && attachment.ttlSeconds > 0,
    {
      fileName: attachment.filename,
      mimeType: attachment.mimeType,
      uploadId,
    },
  );

  if (progressCallback) {
    progressCallback.abort = upload.abort;
    if (uploadId) {
      progressCallback(0, uploadId);
    }
  }

  try {
    const result = await upload.response;
    return [result.id];
  } finally {
    if (progressCallback?.abort === upload.abort) {
      progressCallback.abort = undefined;
    }
  }
}

async function sendScheduledMessage({
  attachment,
  chat,
  entities,
  gif,
  lastMessageId,
  poll,
  progressCallback,
  replyInfo,
  scheduleRepeatPeriod,
  scheduledAt,
  sticker,
  text,
}: {
  attachment?: ApiAttachment;
  chat: { id: string };
  entities?: ApiMessageEntity[];
  gif?: ApiVideo;
  lastMessageId?: number;
  poll?: ApiNewPoll;
  progressCallback?: SendProgressCallback;
  replyInfo?: { replyToMsgId?: number; type?: string };
  scheduleRepeatPeriod?: number;
  scheduledAt: number;
  sticker?: ApiSticker;
  text?: string;
}) {
  if (scheduleRepeatPeriod) {
    return undefined;
  }

  const chatId = chat.id;
  const localId = getLocalId(lastMessageId);
  const localPoll = poll ? buildLocalPoll(localId, poll) : undefined;
  const localMessage: ApiMessage = {
    id: localId,
    chatId,
    date: scheduledAt,
    isOutgoing: true,
    senderId: currentUserId,
    content: buildLocalMessageContent({
      attachment,
      gif,
      poll,
      sticker,
      text,
      entities,
    }),
    sendingState: 'messageSendingStatePending',
    isScheduled: true,
  };
  if (localPoll) {
    localMessage.content.pollId = localPoll.id;
  }

  sendImmediateApiUpdate({
    '@type': 'newScheduledMessage',
    chatId,
    id: localId,
    message: localMessage,
    poll: localPoll,
  });

  let uploadedMediaIds: string[] = [];
  if (attachment) {
    try {
      uploadedMediaIds = await uploadAttachmentIfNeeded(attachment, progressCallback, chatId, localId);
    } catch (error) {
      if (isAbortError(error)) {
        return undefined;
      }

      sendApiUpdate({
        '@type': 'updateScheduledMessageSendFailed',
        chatId,
        localId,
        error: error instanceof Error ? error.message : 'Failed to upload media',
      });
      return undefined;
    }
  }

  try {
    const body = buildSendBody({
      attachment,
      entities,
      gif,
      poll,
      replyInfo,
      sticker,
      text,
      chatId,
    });
    if (uploadedMediaIds.length > 0) {
      body.media_ids = uploadedMediaIds;
    }
    const scheduledAtIso = new Date(scheduledAt * 1000).toISOString();
    const path = `/chats/${chatId}/messages?scheduled_at=${encodeURIComponent(scheduledAtIso)}`;
    const scheduledMessage = await client.request<SaturnScheduledMessage>('POST', path, body);
    const apiMessage = buildApiScheduledMessage(scheduledMessage);
    const apiPoll = buildApiScheduledPoll(scheduledMessage) || localPoll;

    sendApiUpdate({
      '@type': 'updateScheduledMessageSendSucceeded',
      chatId,
      localId,
      message: apiMessage,
      poll: apiPoll,
    });

    return apiMessage;
  } catch (error) {
    sendApiUpdate({
      '@type': 'updateScheduledMessageSendFailed',
      chatId,
      localId,
      error: error instanceof Error ? error.message : 'Failed to schedule message',
    });
    return undefined;
  }
}

export function setCurrentUserId(userId: string) {
  currentUserId = userId;
  setMessageBuilderCurrentUserId(userId);
}

export function resolveMessageUuid(chatId: string, seqNum: number): string | undefined {
  const uuid = getMessageUuid(chatId, seqNum) || getScheduledMessageUuid(chatId, seqNum);
  if (uuid) return uuid;

  return getGlobalMessage(chatId, seqNum)?.saturnId;
}

export async function fetchMessages({
  chat, chatId: chatIdDirect, limit = 50, cursor, offsetId,
}: {
  chat?: { id: string };
  chatId?: string;
  limit?: number;
  cursor?: string;
  offsetId?: number;
}) {
  const chatId = chat?.id || chatIdDirect!;
  const params = new URLSearchParams();
  params.set('limit', String(limit));

  if (cursor) {
    params.set('cursor', cursor);
  } else if (offsetId !== undefined) {
    const forwardBuffer = Math.ceil(limit / 2);
    params.set('cursor', btoa(String(offsetId + forwardBuffer)));
  }

  const requestPath = `/chats/${chatId}/messages?${params.toString()}`;

  return client.deduplicateRequest(`messages:${chatId}:${params.toString()}`, async () => {
    const result = await client.request<SaturnPaginatedResponse<SaturnMessage>>(
      'GET',
      requestPath,
    );

    const messages = result.data.map((message) => {
      const apiMessage = buildApiMessage(message);
      if (currentUserId) {
        apiMessage.isOutgoing = message.sender_id === currentUserId;
      }
      return apiMessage;
    });

    return {
      messages,
      polls: extractApiPolls(result.data),
      count: messages.length,
      topics: [] as any[],
      hasMore: result.has_more,
      nextCursor: result.cursor,
    };
  });
}

export async function fetchMessagesByDate({
  chatId, date, limit = 50,
}: {
  chatId: string;
  date: string;
  limit?: number;
}) {
  const params = new URLSearchParams();
  params.set('date', date);
  params.set('limit', String(limit));

  const result = await client.request<SaturnPaginatedResponse<SaturnMessage>>(
    'GET',
    `/chats/${chatId}/history?${params.toString()}`,
  );

  return {
    messages: result.data.map((message) => {
      const apiMessage = buildApiMessage(message);
      if (currentUserId) {
        apiMessage.isOutgoing = message.sender_id === currentUserId;
      }
      return apiMessage;
    }),
    polls: extractApiPolls(result.data),
    hasMore: result.has_more,
  };
}

export async function fetchMessage({
  chat,
  messageId,
}: {
  chat: { id: string };
  messageId: number;
}) {
  const uuid = resolveMessageUuid(chat.id, messageId);
  if (!uuid) {
    return undefined;
  }

  try {
    const message = await client.request<SaturnMessage>('GET', `/messages/${uuid}`);
    const apiMessage = buildApiMessage(message);
    if (currentUserId) {
      apiMessage.isOutgoing = message.sender_id === currentUserId;
    }

    return {
      message: apiMessage,
      poll: buildApiPoll(message.poll),
    };
  } catch (error) {
    if ((error as { status?: number }).status === 404) {
      return MESSAGE_DELETED;
    }
    throw error;
  }
}

export async function sendMessage({
  chat,
  text,
  entities,
  replyInfo,
  lastMessageId,
  mediaIds,
  isSpoiler,
  attachment,
  sticker,
  gif,
  poll,
  groupedId,
  scheduledAt,
  scheduleRepeatPeriod,
}: {
  chat: { id: string };
  text?: string;
  entities?: ApiMessageEntity[];
  replyInfo?: { replyToMsgId?: number; type?: string };
  lastMessageId?: number;
  mediaIds?: string[];
  isSpoiler?: boolean;
  attachment?: ApiAttachment;
  sticker?: ApiSticker;
  gif?: ApiVideo;
  poll?: ApiNewPoll;
  groupedId?: string;
  scheduledAt?: number;
  scheduleRepeatPeriod?: number;
}, progressCallback?: SendProgressCallback) {
  const chatId = chat.id;
  if (!text && (!mediaIds || mediaIds.length === 0) && !attachment && !sticker && !gif && !poll) {
    return undefined;
  }

  if (scheduledAt) {
    return sendScheduledMessage({
      attachment,
      chat,
      entities,
      gif,
      lastMessageId,
      poll,
      progressCallback,
      replyInfo,
      scheduleRepeatPeriod,
      scheduledAt,
      sticker,
      text,
    });
  }

  const localId = getLocalId(lastMessageId);
  const now = Math.floor(Date.now() / 1000);
  const localPoll = poll ? buildLocalPoll(localId, poll) : undefined;
  const localMessage: ApiMessage = {
    id: localId,
    chatId,
    date: now,
    isOutgoing: true,
    senderId: currentUserId,
    content: buildLocalMessageContent({
      attachment,
      gif,
      poll,
      sticker,
      text,
      entities,
    }),
    sendingState: 'messageSendingStatePending',
    groupedId,
    isInAlbum: groupedId && isAlbumAttachment(attachment) ? true : undefined,
  };

  if (localPoll) {
    localMessage.content.pollId = localPoll.id;
  }

  sendImmediateApiUpdate({
    '@type': 'newMessage',
    chatId,
    id: localId,
    message: localMessage,
    poll: localPoll,
  });

  await new Promise((resolve) => {
    setTimeout(resolve, 0);
  });

  if (!getGlobalMessage(chatId, localId)) {
    return undefined;
  }

  let uploadedMediaIds = mediaIds || [];
  if (attachment) {
    try {
      uploadedMediaIds = await uploadAttachmentIfNeeded(attachment, progressCallback, chatId, localId);
    } catch (error) {
      if (isAbortError(error)) {
        return undefined;
      }

      sendApiUpdate({
        '@type': 'updateMessageSendFailed',
        chatId,
        localId,
        error: error instanceof Error ? error.message : 'Failed to upload media',
      });
      return undefined;
    }
  }

  try {
    const body = buildSendBody({
      attachment,
      entities,
      gif,
      groupedId,
      isSpoiler,
      poll,
      replyInfo,
      sticker,
      text,
      chatId,
    });
    if (uploadedMediaIds.length > 0) {
      body.media_ids = uploadedMediaIds;
    }

    if (poll) {
      const result = await client.request<PollSendResponse>('POST', `/chats/${chatId}/messages`, body);
      trackPendingSend(result.message.id);

      const apiMessage = buildApiMessage(result.message);
      apiMessage.isOutgoing = true;
      const apiPoll = buildApiPoll(result.poll);
      if (apiPoll) {
        apiMessage.content.pollId = apiPoll.id;
      }

      sendApiUpdate({
        '@type': 'updateMessageSendSucceeded',
        chatId,
        localId,
        message: apiMessage,
        poll: apiPoll,
      });

      return apiMessage;
    }

    const message = await client.request<SaturnMessage>('POST', `/chats/${chatId}/messages`, body);
    trackPendingSend(message.id);

    const apiMessage = buildApiMessage(message);
    apiMessage.isOutgoing = true;

    sendApiUpdate({
      '@type': 'updateMessageSendSucceeded',
      chatId,
      localId,
      message: apiMessage,
      poll: buildApiPoll(message.poll),
    });

    return apiMessage;
  } catch (error) {
    sendApiUpdate({
      '@type': 'updateMessageSendFailed',
      chatId,
      localId,
      error: error instanceof Error ? error.message : 'Failed to send message',
    });
    return undefined;
  }
}

function isAbortError(error: unknown) {
  return error instanceof Error && error.name === 'AbortError';
}

export async function editMessage({
  chat,
  message,
  text,
  entities,
  attachment,
  noWebPage,
}: {
  chat: ApiChat;
  message: ApiMessage;
  text: string;
  entities?: ApiMessageEntity[];
  attachment?: ApiAttachment;
  noWebPage?: boolean;
}) {
  void attachment;
  void noWebPage;

  const chatId = chat.id;
  const messageId = message.id;
  const uuid = message.saturnId || resolveMessageUuid(chatId, messageId);
  if (!uuid) return undefined;

  sendApiUpdate({
    '@type': 'updateMessage',
    chatId,
    id: messageId,
    isFull: true,
    message: {
      ...message,
      content: {
        ...message.content,
        text: {
          text,
          entities: entities || [],
        },
      },
    },
  });

  const body: Record<string, unknown> = { content: text };
  if (entities?.length) {
    body.entities = buildSaturnEntities(entities);
  }

  try {
    const updatedMessage = await client.request<SaturnMessage>('PATCH', `/messages/${uuid}`, body);
    const apiMessage = buildApiMessage(updatedMessage);
    if (currentUserId) {
      apiMessage.isOutgoing = updatedMessage.sender_id === currentUserId;
    }

    sendApiUpdate({
      '@type': 'updateMessage',
      chatId,
      id: messageId,
      isFull: true,
      message: apiMessage,
      poll: buildApiPoll(updatedMessage.poll),
    });

    return apiMessage;
  } catch (error) {
    sendApiUpdate({
      '@type': 'error',
      error: {
        message: error instanceof Error ? error.message : 'Failed to edit message',
        hasErrorKey: true,
      },
    });

    sendApiUpdate({
      '@type': 'updateMessage',
      chatId,
      id: messageId,
      isFull: true,
      message,
    });

    return undefined;
  }
}

export async function deleteMessages({
  chat,
  messageIds,
}: {
  chat: { id: string };
  messageIds: number[];
}) {
  const deletedIds: number[] = [];

  await Promise.all(messageIds.map(async (messageId) => {
    const uuid = resolveMessageUuid(chat.id, messageId);
    if (!uuid) return;

    try {
      await client.request('DELETE', `/messages/${uuid}`);
      deletedIds.push(messageId);
    } catch {
      // Ignore per-message deletion errors to match existing behavior.
    }
  }));

  if (deletedIds.length) {
    sendApiUpdate({
      '@type': 'deleteMessages',
      ids: deletedIds,
      chatId: chat.id,
    });
  }
}

export async function forwardMessages({
  fromChatId,
  messageIds,
  toChatId,
}: {
  fromChatId: string;
  messageIds: number[];
  toChatId: string;
}) {
  const uuids = messageIds
    .map((messageId) => resolveMessageUuid(fromChatId, messageId))
    .filter(Boolean);

  if (!uuids.length) {
    return undefined;
  }

  const result = await client.request<{ messages: SaturnMessage[] }>(
    'POST',
    '/messages/forward',
    { message_ids: uuids, to_chat_id: toChatId },
  );

  const messages = result.messages.map((message) => {
    const apiMessage = buildApiMessage(message);
    apiMessage.isOutgoing = true;
    return apiMessage;
  });

  messages.forEach((message) => {
    sendApiUpdate({
      '@type': 'newMessage',
      chatId: toChatId,
      id: message.id,
      message,
      poll: undefined,
    });
  });

  return { messages };
}

export async function fetchPinnedMessages({
  chat,
  chatId: chatIdDirect,
}: {
  chat?: { id: string };
  chatId?: string;
}) {
  const chatId = chat?.id || chatIdDirect!;

  return client.deduplicateRequest(`pinned:${chatId}`, async () => {
    const result = await client.request<{ messages: SaturnMessage[] }>('GET', `/chats/${chatId}/pinned`);
    const pinnedMessages = result.messages || [];

    const messages = pinnedMessages.map((message) => {
      const apiMessage = buildApiMessage(message);
      if (currentUserId) {
        apiMessage.isOutgoing = message.sender_id === currentUserId;
      }
      return apiMessage;
    });
    const pinnedIds = messages.map((message) => message.id);

    sendApiUpdate({
      '@type': 'updatePinnedIds',
      chatId,
      messageIds: pinnedIds,
    });

    return {
      messages,
      pinnedIds,
      polls: extractApiPolls(pinnedMessages),
    };
  });
}

export async function pinMessage({
  chat,
  messageId,
  isUnpin,
}: {
  chat: { id: string };
  messageId: number;
  isUnpin?: boolean;
}) {
  const uuid = resolveMessageUuid(chat.id, messageId);
  if (!uuid) return undefined;

  if (isUnpin) {
    await client.request('DELETE', `/chats/${chat.id}/pin/${uuid}`);
  } else {
    await client.request('POST', `/chats/${chat.id}/pin/${uuid}`);
  }

  sendApiUpdate({
    '@type': 'updateMessage',
    chatId: chat.id,
    id: messageId,
    isFull: false,
    message: { isPinned: !isUnpin },
  });
  sendApiUpdate({
    '@type': 'updatePinnedIds',
    chatId: chat.id,
    isPinned: !isUnpin,
    messageIds: [messageId],
  });

  return true;
}

export async function unpinMessage({
  chat,
  messageId,
}: {
  chat: { id: string };
  messageId: number;
}) {
  return pinMessage({ chat, messageId, isUnpin: true });
}

export async function unpinAllMessages({ chat }: { chat: { id: string } }) {
  await client.request('DELETE', `/chats/${chat.id}/pin`);
  sendApiUpdate({
    '@type': 'updatePinnedIds',
    chatId: chat.id,
    messageIds: [],
  });
  return true;
}

export async function markMessageListRead({
  chat,
  maxId,
}: {
  chat: { id: string };
  threadId?: number;
  maxId: number;
}) {
  const uuid = resolveMessageUuid(chat.id, maxId);
  if (!uuid) return undefined;

  await client.request('PATCH', `/chats/${chat.id}/read`, { last_read_message_id: uuid });

  sendApiUpdate({
    '@type': 'updateChat',
    id: chat.id,
    chat: {},
    readState: {
      lastReadInboxMessageId: maxId,
      unreadCount: 0,
    },
    noTopChatsRequest: true,
  });

  return true;
}

export async function markMessagesRead({
  chat,
  messageIds,
}: {
  chat: { id: string };
  messageIds: number[];
}) {
  if (!messageIds.length) {
    return true;
  }

  const maxId = Math.max(...messageIds);
  const uuid = resolveMessageUuid(chat.id, maxId);
  if (!uuid) {
    return undefined;
  }

  await client.request('PATCH', `/chats/${chat.id}/read`, { last_read_message_id: uuid });

  return true;
}

export function fetchMessageLink({
  chatId,
  messageId,
}: {
  chatId: string;
  messageId: number;
}) {
  const uuid = resolveMessageUuid(chatId, messageId);
  if (!uuid) return undefined;

  return { link: `#chat/${chatId}/${uuid}` };
}

export function sendMessageAction({
  peer,
  action,
}: {
  peer: { id: string };
  threadId?: number;
  action: ApiSendMessageAction;
}) {
  if (action.type === 'cancel') {
    client.sendWsMessage('stop_typing', { chat_id: peer.id });
  } else {
    client.sendWsMessage('typing', { chat_id: peer.id });
  }
}

export async function searchMessagesInChat({
  peer,
  type,
  limit,
  offsetId,
}: {
  peer: ApiPeer;
  type?: string;
  limit: number;
  threadId?: number;
  offsetId?: number;
  isSavedDialog?: boolean;
}) {
  const chatId = peer.id;
  const saturnType = type ? SHARED_MEDIA_TYPE_MAP[type] : undefined;

  if (type && !saturnType) {
    return {
      messages: [] as ApiMessage[],
      totalCount: 0,
      nextOffsetId: undefined,
      userStatusesById: {},
    };
  }

  const params = new URLSearchParams();
  params.set('limit', String(limit));
  if (saturnType) params.set('type', saturnType);

  if (offsetId) {
    const cursorKey = `${chatId}:${type || ''}:${offsetId}`;
    const cursor = sharedMediaCursors.get(cursorKey);
    if (cursor) {
      params.set('cursor', cursor);
    }
  }

  const result = await client.request<SaturnPaginatedResponse<SaturnSharedMediaItem>>(
    'GET',
    `/chats/${chatId}/media?${params.toString()}`,
  );

  const messages: ApiMessage[] = result.data.map((item) => {
    const syntheticMessage: SaturnMessage = {
      id: item.message_id,
      chat_id: item.chat_id,
      sender_id: item.sender_id,
      type: 'text',
      content: item.content,
      is_edited: false,
      is_deleted: false,
      is_pinned: false,
      is_forwarded: false,
      sequence_number: item.sequence_number,
      created_at: item.created_at,
      sender_name: '',
      media_attachments: [item.attachment],
    };

    const apiMessage = buildApiMessage(syntheticMessage);
    if (currentUserId) {
      apiMessage.isOutgoing = item.sender_id === currentUserId;
    }
    return apiMessage;
  });

  let nextOffsetId: number | undefined;
  if (result.has_more && result.data.length > 0 && result.cursor) {
    const lastItem = result.data[result.data.length - 1];
    nextOffsetId = lastItem.sequence_number;
    sharedMediaCursors.set(`${chatId}:${type || ''}:${nextOffsetId}`, result.cursor);
  }

  return {
    messages,
    totalCount: messages.length + (result.has_more ? limit : 0),
    nextOffsetId,
    userStatusesById: {} as Record<string, any>,
  };
}

export async function sendPollVote({
  chat,
  messageId,
  options,
}: {
  chat: { id: string };
  messageId: number;
  options: string[];
}) {
  const uuid = resolveMessageUuid(chat.id, messageId);
  if (!uuid) return undefined;

  const method = options.length ? 'POST' : 'DELETE';
  const path = `/messages/${uuid}/poll/vote`;
  const body = options.length ? { option_ids: options } : undefined;

  const poll = await client.request<SaturnPoll>(method, path, body);
  const apiPoll = buildApiPoll(poll);

  if (apiPoll) {
    sendApiUpdate({
      '@type': 'updateMessagePoll',
      pollId: apiPoll.id,
      pollUpdate: apiPoll,
    });

    if (currentUserId) {
      sendApiUpdate({
        '@type': 'updateMessagePollVote',
        pollId: apiPoll.id,
        peerId: currentUserId,
        options,
      });
    }
  }

  return true;
}

export async function closePoll({
  chat,
  messageId,
}: {
  chat: { id: string };
  messageId: number;
  poll?: ApiPoll;
}) {
  const uuid = resolveMessageUuid(chat.id, messageId);
  if (!uuid) return undefined;

  const poll = await client.request<SaturnPoll>('POST', `/messages/${uuid}/poll/close`);
  const apiPoll = buildApiPoll(poll);
  if (apiPoll) {
    sendApiUpdate({
      '@type': 'updateMessagePoll',
      pollId: apiPoll.id,
      pollUpdate: apiPoll,
    });
  }
  return true;
}

export async function loadPollOptionResults({
  chat,
  messageId,
  option,
  offset,
  limit = 50,
}: {
  chat: { id: string };
  messageId: number;
  option: string;
  offset?: string;
  limit?: number;
}) {
  const uuid = resolveMessageUuid(chat.id, messageId);
  if (!uuid) return undefined;

  const params = new URLSearchParams({
    option_id: option,
    limit: String(limit),
  });
  if (offset) {
    params.set('cursor', offset);
  }

  const page = await client.request<SaturnPaginatedResponse<SaturnPollVote>>(
    'GET',
    `/messages/${uuid}/poll/voters?${params.toString()}`,
  );

  return {
    count: page.data.length + (page.has_more ? 1 : 0),
    nextOffset: page.cursor,
    shouldResetVoters: !offset,
    votes: page.data.map((vote) => ({
      peerId: vote.user_id,
      date: Math.floor(new Date(vote.voted_at).getTime() / 1000),
    })),
  };
}

export async function fetchScheduledHistory({ chat }: { chat: { id: string } }) {
  const scheduled = await client.request<SaturnScheduledMessage[] | null>(
    'GET',
    `/chats/${chat.id}/messages/scheduled`,
  );
  const scheduledMessages = scheduled || [];

  return {
    messages: scheduledMessages.map((message) => buildApiScheduledMessage(message)),
    polls: extractScheduledApiPolls(scheduledMessages),
  };
}

export async function sendScheduledMessages({
  chat,
  ids,
}: {
  chat: { id: string };
  ids: number[];
}) {
  const deliveredMessages: ApiMessage[] = [];
  const deliveredPolls: Array<ApiPoll | undefined> = [];

  for (const id of ids) {
    const uuid = resolveMessageUuid(chat.id, id);
    if (!uuid) continue;

    const message = await client.request<SaturnMessage>('POST', `/messages/${uuid}/scheduled/send-now`);
    const apiMessage = buildApiMessage(message);
    if (currentUserId) {
      apiMessage.isOutgoing = message.sender_id === currentUserId;
    }
    deliveredMessages.push(apiMessage);

    const poll = buildApiPoll(message.poll);
    deliveredPolls.push(poll);
  }

  if (ids.length) {
    sendApiUpdate({
      '@type': 'deleteScheduledMessages',
      chatId: chat.id,
      ids,
      newIds: deliveredMessages.map((message) => message.id),
    });
  }

  deliveredMessages.forEach((message, index) => {
    sendApiUpdate({
      '@type': 'newMessage',
      chatId: chat.id,
      id: message.id,
      message,
      poll: deliveredPolls[index],
    });
  });

  return true;
}

export async function editScheduledMessage({
  chat,
  message,
  text,
  entities,
  scheduledAt,
}: {
  chat: { id: string };
  message: ApiMessage;
  text?: string;
  entities?: ApiMessageEntity[];
  scheduledAt?: number;
}) {
  const uuid = message.saturnId || resolveMessageUuid(chat.id, message.id);
  if (!uuid) return undefined;

  const body: Record<string, unknown> = {};
  if (text !== undefined) {
    body.content = text;
  }
  if (entities?.length) {
    body.entities = buildSaturnEntities(entities);
  }
  if (scheduledAt) {
    body.scheduled_at = new Date(scheduledAt * 1000).toISOString();
  }

  const updated = await client.request<SaturnScheduledMessage>('PATCH', `/messages/${uuid}/scheduled`, body);
  const apiMessage = buildApiScheduledMessage(updated);
  const apiPoll = buildApiScheduledPoll(updated);

  sendApiUpdate({
    '@type': 'updateScheduledMessage',
    chatId: chat.id,
    id: message.id,
    isFull: true,
    message: apiMessage,
    poll: apiPoll,
  });

  return apiMessage;
}

export async function deleteScheduledMessages({
  chat,
  messageIds,
}: {
  chat: { id: string };
  messageIds: number[];
}) {
  for (const id of messageIds) {
    const uuid = resolveMessageUuid(chat.id, id);
    if (!uuid) continue;
    await client.request('DELETE', `/messages/${uuid}/scheduled`);
  }

  sendApiUpdate({
    '@type': 'deleteScheduledMessages',
    chatId: chat.id,
    ids: messageIds,
  });

  return true;
}

export async function rescheduleMessage({
  chat,
  message,
  scheduledAt,
}: {
  chat: { id: string };
  message: ApiMessage;
  scheduledAt: number;
  scheduleRepeatPeriod?: number;
}) {
  return editScheduledMessage({
    chat,
    message,
    scheduledAt,
  });
}

function detectMediaType(attachment: ApiAttachment) {
  if (attachment.voice) return 'voice';
  if (attachment.shouldSendAsFile) return 'file';

  const mime = attachment.mimeType || '';
  if (mime === 'image/gif') return 'gif';
  if (mime.startsWith('image/')) return 'photo';
  if (mime.startsWith('video/')) return 'video';
  if (mime.startsWith('audio/')) return 'voice';
  return 'file';
}
