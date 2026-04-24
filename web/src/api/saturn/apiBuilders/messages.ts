// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import type {
  ApiKeyboardButton,
  ApiMessage,
  ApiMessageEntity,
  ApiPhoto,
  ApiPoll,
  ApiReplyKeyboard,
  ApiVideo,
} from '../../types';
import type {
  SaturnInlineKeyboardButton,
  SaturnMediaAttachment,
  SaturnMessage,
  SaturnMessageEntity,
  SaturnPoll,
  SaturnReplyMarkup,
  SaturnScheduledMessage,
  SaturnSticker,
} from '../types';
import { ApiMessageEntityTypes } from '../../types';

import { getBaseUrl } from '../client';
import { buildApiReactions } from './reactions';
import {
  buildGifFromSerializedMessage,
  buildStickerFromSerializedMessage,
  parseRichMessageContent,
  registerAsset,
} from './symbols';

const MAX_UUID_MAP_SIZE = 50000;
const uuidToSeqMap = new Map<string, number>();
const seqToUuidMap = new Map<string, Map<number, string>>();
const scheduledUuidToIdMap = new Map<string, number>();
const scheduledIdToUuidMap = new Map<string, Map<number, string>>();
let nextScheduledId = -1;
let currentUserId: string | undefined;

function evictOldEntries() {
  if (uuidToSeqMap.size <= MAX_UUID_MAP_SIZE) return;

  const toDelete = uuidToSeqMap.size - MAX_UUID_MAP_SIZE;
  let count = 0;

  for (const [uuid, seqNum] of uuidToSeqMap.entries()) {
    if (count >= toDelete) break;

    uuidToSeqMap.delete(uuid);
    for (const chatMap of seqToUuidMap.values()) {
      if (chatMap.get(seqNum) === uuid) {
        chatMap.delete(seqNum);
        break;
      }
    }
    count++;
  }
}

function getScheduledMapKey(chatId: string, uuid: string) {
  return `${chatId}:${uuid}`;
}

function fullMediaUrl(relativePath?: string) {
  if (!relativePath) return undefined;
  if (/^(?:https?:|data:|blob:)/.test(relativePath)) {
    return relativePath;
  }
  return `${getBaseUrl()}${relativePath}`;
}

function isImageMimeType(mimeType?: string) {
  return Boolean(mimeType?.startsWith('image/'));
}

function isPhotoAttachment(att: Pick<SaturnMediaAttachment, 'type' | 'mime_type'>) {
  return att.type === 'photo' || (att.type === 'file' && isImageMimeType(att.mime_type));
}

function getEffectiveAttachmentType(att: SaturnMediaAttachment) {
  return isPhotoAttachment(att) ? 'photo' : att.type;
}

function getMediaAttachmentUrl(att: SaturnMediaAttachment, variant: 'full' | 'thumbnail' | 'medium') {
  switch (variant) {
    case 'thumbnail':
      return att.thumbnail_url ? fullMediaUrl(`/media/${att.media_id}/thumbnail`) : undefined;
    case 'medium':
      return att.medium_url ? fullMediaUrl(`/media/${att.media_id}/medium`) : undefined;
    case 'full':
    default:
      return fullMediaUrl(`/media/${att.media_id}`);
  }
}

function registerMediaAttachmentAsset(att: SaturnMediaAttachment) {
  const fullUrl = getMediaAttachmentUrl(att, 'full');
  const previewUrl = getMediaAttachmentUrl(att, 'thumbnail')
    || getMediaAttachmentUrl(att, 'medium')
    || (isPhotoAttachment(att) ? fullUrl : undefined);
  const assetKinds = isPhotoAttachment(att) ? ['photo', 'document'] as const : ['document'] as const;

  registerAsset(att.media_id, {
    fileName: att.original_filename || `${att.media_id}${guessFileExtension(att.mime_type)}`,
    fullUrl,
    previewUrl,
    mimeType: att.mime_type,
  }, assetKinds);
}

function guessFileExtension(mimeType?: string) {
  if (!mimeType) {
    return '';
  }

  const knownExtensions: Record<string, string> = {
    'image/jpeg': '.jpg',
    'image/png': '.png',
    'image/webp': '.webp',
    'video/mp4': '.mp4',
    'video/webm': '.webm',
  };

  if (knownExtensions[mimeType]) {
    return knownExtensions[mimeType];
  }

  const subtype = mimeType.split('/')[1];
  return subtype ? `.${subtype}` : '';
}

function inferStickerAttachmentFileType(att: Pick<SaturnMediaAttachment, 'mime_type' | 'url' | 'original_filename'>) {
  const mimeType = att.mime_type.toLowerCase();
  const fileSource = `${att.original_filename || ''} ${att.url || ''}`.toLowerCase();

  if (mimeType === 'video/webm' || fileSource.includes('.webm')) {
    return 'webm' as const;
  }

  if (
    mimeType === 'application/x-tgsticker'
    || mimeType === 'application/x-gzip'
    || mimeType === 'application/gzip'
    || fileSource.includes('.tgs')
  ) {
    return 'tgs' as const;
  }

  return 'webp' as const;
}

function getOrCreateScheduledMessageId(chatId: string, uuid: string) {
  const key = getScheduledMapKey(chatId, uuid);
  const existing = scheduledUuidToIdMap.get(key);
  if (existing !== undefined) {
    return existing;
  }

  const id = nextScheduledId--;
  scheduledUuidToIdMap.set(key, id);

  let chatMap = scheduledIdToUuidMap.get(chatId);
  if (!chatMap) {
    chatMap = new Map();
    scheduledIdToUuidMap.set(chatId, chatMap);
  }
  chatMap.set(id, uuid);

  return id;
}

function buildSaturnPhoto(att: SaturnMediaAttachment): ApiPhoto {
  registerMediaAttachmentAsset(att);

  return {
    mediaType: 'photo',
    id: att.media_id,
    date: 0,
    thumbnail: undefined,
    sizes: [
      { width: att.width || 320, height: att.height || 320, type: 's' },
      { width: att.width || 800, height: att.height || 800, type: 'y' },
    ],
    isSpoiler: att.is_spoiler || undefined,
  };
}

function buildSaturnVideo(att: SaturnMediaAttachment): ApiVideo {
  registerMediaAttachmentAsset(att);

  return {
    mediaType: 'video',
    id: att.media_id,
    mimeType: att.mime_type || 'video/mp4',
    duration: att.duration_seconds || 0,
    width: att.width || 0,
    height: att.height || 0,
    fileName: att.original_filename || 'video.mp4',
    size: att.size_bytes,
    isRound: att.type === 'videonote' || undefined,
    thumbnail: undefined,
    isSpoiler: att.is_spoiler || undefined,
  };
}

function buildApiPollOptionResult(poll: SaturnPoll, option: SaturnPoll['options'][number]) {
  const derivedCorrect = poll.correct_option !== undefined
    && poll.correct_option === option.position
    && (poll.is_closed || option.is_chosen);

  return {
    option: option.id,
    votersCount: option.voters,
    isChosen: option.is_chosen || undefined,
    isCorrect: option.is_correct || derivedCorrect || undefined,
  };
}

function buildApiEntity(entity: SaturnMessageEntity): ApiMessageEntity {
  const base = { type: entity.type as ApiMessageEntity['type'], offset: entity.offset, length: entity.length };
  if (entity.url) return { ...base, type: ApiMessageEntityTypes.TextUrl, url: entity.url };
  if (entity.language) return { ...base, type: ApiMessageEntityTypes.Pre, language: entity.language };
  if (entity.user_id) return { ...base, type: ApiMessageEntityTypes.MentionName, userId: entity.user_id };
  return base as ApiMessageEntity;
}

function buildMediaContent(content: ApiMessage['content'], attachments: SaturnMediaAttachment[]) {
  const first = attachments[0];
  if (!first) return;
  const effectiveFirstType = getEffectiveAttachmentType(first);

  if (first.is_one_time) {
    content.ttlSeconds = 1;
  }

  const albumAttachments = attachments.filter(
    (attachment) => {
      const type = getEffectiveAttachmentType(attachment);
      return type === 'photo' || type === 'video';
    },
  );
  if (albumAttachments.length > 1) {
    content.albumMedia = albumAttachments.map((attachment) => {
      return getEffectiveAttachmentType(attachment) === 'photo'
        ? buildSaturnPhoto(attachment)
        : buildSaturnVideo(attachment);
    });

    if (effectiveFirstType === 'photo') {
      content.photo = content.albumMedia[0] as ApiPhoto;
    } else {
      content.video = content.albumMedia[0] as ApiVideo;
    }
    return;
  }

  switch (effectiveFirstType) {
    case 'photo':
      content.photo = buildSaturnPhoto(first);
      break;
    case 'video':
    case 'videonote':
      content.video = buildSaturnVideo(first);
      break;
    case 'voice':
      content.voice = {
        mediaType: 'voice',
        id: first.media_id,
        duration: first.duration_seconds || 0,
        waveform: first.waveform_data || [],
        size: first.size_bytes,
      };
      break;
    case 'gif':
      content.video = {
        mediaType: 'video',
        id: first.media_id,
        mimeType: 'video/mp4',
        duration: first.duration_seconds || 0,
        width: first.width || 0,
        height: first.height || 0,
        fileName: 'animation.mp4',
        size: first.size_bytes,
        isGif: true,
      };
      break;
    case 'sticker': {
      const serializedSticker: SaturnSticker = {
        id: first.media_id,
        pack_id: '',
        emoji: undefined,
        file_url: getMediaAttachmentUrl(first, 'full') || fullMediaUrl(first.url) || '',
        preview_url: getMediaAttachmentUrl(first, 'thumbnail') || getMediaAttachmentUrl(first, 'medium'),
        file_type: inferStickerAttachmentFileType(first),
        width: first.width || undefined,
        height: first.height || undefined,
        position: first.position,
      };
      content.sticker = buildStickerFromSerializedMessage({
        id: serializedSticker.id,
        is_custom_emoji: false,
        is_lottie: serializedSticker.file_type === 'tgs',
        is_video: serializedSticker.file_type === 'webm',
        set_id: serializedSticker.pack_id,
        url: serializedSticker.file_url,
        preview_url: serializedSticker.preview_url,
        width: serializedSticker.width,
        height: serializedSticker.height,
      });
      break;
    }
    case 'file':
    default:
      content.document = {
        mediaType: 'document',
        id: first.media_id,
        mimeType: first.mime_type || 'application/octet-stream',
        fileName: first.original_filename || 'file',
        pageCount: first.page_count || undefined,
        size: first.size_bytes,
      };
      break;
  }
}

function isGroupedAlbumMessage(content: ApiMessage['content'], groupedId?: string) {
  return Boolean(
    groupedId
    && (content.photo || (content.video && !content.video.isGif && !content.video.isRound)),
  );
}

type SaturnReplyMessage = Pick<SaturnMessage, 'chat_id' | 'reply_to_id' | 'reply_to_sequence_number'>
  | Pick<SaturnScheduledMessage, 'chat_id' | 'reply_to_id' | 'reply_to_sequence_number'>;

function buildReplyInfo(msg: SaturnReplyMessage) {
  if (!msg.reply_to_id) return undefined;

  const replySeqNum = uuidToSeqMap.get(msg.reply_to_id) || msg.reply_to_sequence_number;
  if (!replySeqNum) return undefined;

  if (msg.reply_to_sequence_number && !uuidToSeqMap.has(msg.reply_to_id)) {
    registerMessageId(msg.chat_id, msg.reply_to_id, msg.reply_to_sequence_number);
  }

  return {
    type: 'message' as const,
    replyToMsgId: replySeqNum,
  };
}

function buildForwardInfo(msg: SaturnMessage) {
  if (!msg.is_forwarded) return undefined;

  return {
    isChannelPost: false,
    date: Math.floor(new Date(msg.created_at).getTime() / 1000),
    senderUserId: msg.forwarded_from || undefined,
  };
}

export function setMessageBuilderCurrentUserId(userId: string) {
  currentUserId = userId;
}

export function registerMessageId(chatId: string, uuid: string, sequenceNumber: number) {
  uuidToSeqMap.set(uuid, sequenceNumber);

  let chatMap = seqToUuidMap.get(chatId);
  if (!chatMap) {
    chatMap = new Map();
    seqToUuidMap.set(chatId, chatMap);
  }
  chatMap.set(sequenceNumber, uuid);

  evictOldEntries();
}

export function getMessageUuid(chatId: string, seqNum: number) {
  return seqToUuidMap.get(chatId)?.get(seqNum);
}

export function getMessageSeqNum(uuid: string) {
  return uuidToSeqMap.get(uuid);
}

export function getScheduledMessageUuid(chatId: string, messageId: number) {
  return scheduledIdToUuidMap.get(chatId)?.get(messageId);
}

export function buildApiPoll(poll?: SaturnPoll): ApiPoll | undefined {
  if (!poll) return undefined;

  const closeDate = poll.close_at
    ? Math.floor(new Date(poll.close_at).getTime() / 1000)
    : undefined;
  const createdAt = Math.floor(new Date(poll.created_at).getTime() / 1000);
  const closePeriod = closeDate && closeDate > createdAt ? closeDate - createdAt : undefined;

  return {
    mediaType: 'poll',
    id: poll.id,
    summary: {
      closed: poll.is_closed,
      isPublic: !poll.is_anonymous,
      multipleChoice: poll.is_multiple,
      quiz: poll.is_quiz,
      question: {
        text: poll.question,
        entities: [],
      },
      answers: [...poll.options]
        .sort((left, right) => left.position - right.position)
        .map((option) => ({
          option: option.id,
          text: {
            text: option.text,
            entities: [],
          },
        })),
      closeDate,
      closePeriod,
    },
    results: {
      results: [...poll.options]
        .sort((left, right) => left.position - right.position)
        .map((option) => buildApiPollOptionResult(poll, option)),
      totalVoters: poll.total_voters,
      solution: poll.solution,
      solutionEntities: poll.solution_entities?.map(buildApiEntity),
    },
  };
}

export function buildApiScheduledPoll(msg: SaturnScheduledMessage): ApiPoll | undefined {
  if (!msg.poll) return undefined;

  const pollId = `scheduled-poll-${msg.id}`;

  return {
    mediaType: 'poll',
    id: pollId,
    summary: {
      closed: false,
      isPublic: !msg.poll.is_anonymous,
      multipleChoice: msg.poll.is_multiple,
      quiz: msg.poll.is_quiz,
      question: {
        text: msg.poll.question,
        entities: [],
      },
      answers: msg.poll.options.map((option, position) => ({
        option: getScheduledPollOptionId(msg.id, position),
        text: {
          text: option,
          entities: [],
        },
      })),
      closeDate: undefined,
      closePeriod: undefined,
    },
    results: {
      results: msg.poll.options.map((_, position) => ({
        option: getScheduledPollOptionId(msg.id, position),
        votersCount: 0,
      })),
      totalVoters: 0,
      solution: msg.poll.solution,
      solutionEntities: msg.poll.solution_entities?.map(buildApiEntity),
    },
  };
}

function getScheduledPollOptionId(messageId: string, position: number) {
  return `scheduled-poll-option-${messageId}-${position}`;
}

function buildInlineButton(btn: SaturnInlineKeyboardButton): ApiKeyboardButton {
  if (btn.url) {
    return { type: 'url', text: btn.text, url: btn.url };
  }
  if (btn.callback_data !== undefined) {
    return { type: 'callback', text: btn.text, data: btn.callback_data };
  }
  return { type: 'command', text: btn.text };
}

function buildReplyKeyboard(markup: SaturnReplyMarkup | string | undefined): ApiReplyKeyboard | undefined {
  if (!markup) return undefined;

  // Backend may return reply_markup as a JSON string — parse it
  let parsed: SaturnReplyMarkup;
  if (typeof markup === 'string') {
    try {
      parsed = JSON.parse(markup);
    } catch {
      return undefined;
    }
  } else {
    parsed = markup;
  }

  if (!parsed?.inline_keyboard?.length) return undefined;

  return {
    inlineButtons: parsed.inline_keyboard.map((row) => row.map(buildInlineButton)),
  };
}

export function buildApiMessage(msg: SaturnMessage): ApiMessage {
  registerMessageId(msg.chat_id, msg.id, msg.sequence_number);

  const richContent = parseRichMessageContent(msg.content);
  const content: ApiMessage['content'] = {
    text: msg.content && !richContent && msg.type !== 'poll' ? {
      text: msg.content,
      entities: msg.entities?.map(buildApiEntity) || [],
    } : undefined,
  };

  if (msg.media_attachments?.length) {
    buildMediaContent(content, msg.media_attachments);
  }

  // One-time media: set ttlSeconds so TG Web A renders blur/one-time overlay
  if (msg.is_one_time && !msg.viewed_at) {
    content.ttlSeconds = 2147483647;
  }

  if (!msg.media_attachments?.length && richContent?.kind === 'sticker') {
    content.sticker = buildStickerFromSerializedMessage(richContent.sticker);
  } else if (content.sticker && richContent?.kind === 'sticker' && richContent.sticker.emoji) {
    // Media attachment path doesn't carry the emoji — backfill from the serialized JSON content.
    content.sticker = { ...content.sticker, emoji: richContent.sticker.emoji };
  }

  if (richContent?.kind === 'gif') {
    content.video = buildGifFromSerializedMessage(richContent.gif);
  }

  const poll = buildApiPoll(msg.poll);
  if (poll) {
    content.pollId = poll.id;
    if (!content.text) {
      content.text = {
        text: poll.summary.question.text,
        entities: [...(poll.summary.question.entities || [])],
      };
    }
  }

  const isInAlbum = isGroupedAlbumMessage(content, msg.grouped_id);

  const replyMarkup = buildReplyKeyboard(msg.reply_markup);

  return {
    id: msg.sequence_number,
    chatId: msg.chat_id,
    saturnId: msg.id,
    date: Math.floor(new Date(msg.created_at).getTime() / 1000),
    isOutgoing: false,
    sendingState: undefined,
    senderId: msg.sender_id || undefined,
    content,
    isEdited: msg.is_edited,
    editDate: msg.edited_at ? Math.floor(new Date(msg.edited_at).getTime() / 1000) : undefined,
    isPinned: msg.is_pinned,
    isDeleting: msg.is_deleted,
    replyInfo: msg.reply_to_id ? buildReplyInfo(msg) : undefined,
    forwardInfo: msg.is_forwarded ? buildForwardInfo(msg) : undefined,
    reactions: buildApiReactions(msg.reactions, currentUserId),
    areReactionsPossible: msg.is_deleted ? undefined : true,
    groupedId: msg.grouped_id,
    isInAlbum: isInAlbum || undefined,
    inlineButtons: replyMarkup?.inlineButtons,
    viaBotId: msg.via_bot_id,
  };
}

export function buildApiScheduledMessage(msg: SaturnScheduledMessage): ApiMessage {
  const richContent = parseRichMessageContent(msg.content);
  const id = getOrCreateScheduledMessageId(msg.chat_id, msg.id);
  const content: ApiMessage['content'] = {
    text: msg.content && !richContent && msg.type !== 'poll' ? {
      text: msg.content,
      entities: msg.entities?.map(buildApiEntity) || [],
    } : undefined,
  };

  if (msg.media_attachments?.length) {
    buildMediaContent(content, msg.media_attachments);
    if (content.sticker && richContent?.kind === 'sticker' && richContent.sticker.emoji) {
      content.sticker = { ...content.sticker, emoji: richContent.sticker.emoji };
    }
  } else if (richContent?.kind === 'sticker') {
    content.sticker = buildStickerFromSerializedMessage(richContent.sticker);
    content.text = undefined;
  } else if (richContent?.kind === 'gif') {
    content.video = buildGifFromSerializedMessage(richContent.gif);
    content.text = undefined;
  }

  const poll = buildApiScheduledPoll(msg);
  if (poll) {
    content.pollId = poll.id;
    if (!content.text) {
      content.text = {
        text: poll.summary.question.text,
        entities: [...(poll.summary.question.entities || [])],
      };
    }
  }

  return {
    id,
    chatId: msg.chat_id,
    saturnId: msg.id,
    date: Math.floor(new Date(msg.scheduled_at).getTime() / 1000),
    isOutgoing: true,
    senderId: msg.sender_id,
    content,
    replyInfo: msg.reply_to_id ? buildReplyInfo(msg) : undefined,
    isScheduled: true,
    isDeleting: msg.is_sent,
  };
}

export function buildSaturnEntities(entities: ApiMessageEntity[]): SaturnMessageEntity[] {
  return entities.map((entity) => {
    const base: SaturnMessageEntity = { type: entity.type, offset: entity.offset, length: entity.length };
    if ('url' in entity) base.url = entity.url;
    if ('language' in entity) base.language = entity.language;
    if ('userId' in entity) base.user_id = entity.userId;
    return base;
  });
}
