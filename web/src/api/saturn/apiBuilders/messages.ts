import type { ApiMessage, ApiMessageEntity } from '../../types';
import type { SaturnMediaAttachment, SaturnMessage, SaturnMessageEntity } from '../types';
import { getBaseUrl } from '../client';

// Saturn uses UUID for message IDs, but TG Web A expects numeric IDs.
// We use sequence_number as the numeric message ID.
// UUID is stored in a mapping for API calls that need it.

const MAX_UUID_MAP_SIZE = 50000;
const uuidToSeqMap = new Map<string, number>();
const seqToUuidMap = new Map<string, Map<number, string>>(); // chatId -> seqNum -> uuid

function evictOldEntries() {
  if (uuidToSeqMap.size <= MAX_UUID_MAP_SIZE) return;
  // Map iterates in insertion order — delete oldest entries
  const toDelete = uuidToSeqMap.size - MAX_UUID_MAP_SIZE;
  let count = 0;
  for (const key of uuidToSeqMap.keys()) {
    if (count >= toDelete) break;
    uuidToSeqMap.delete(key);
    count++;
  }
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

export function getMessageUuid(chatId: string, seqNum: number): string | undefined {
  return seqToUuidMap.get(chatId)?.get(seqNum);
}

export function getMessageSeqNum(uuid: string): number | undefined {
  return uuidToSeqMap.get(uuid);
}

export function buildApiMessage(msg: SaturnMessage): ApiMessage {
  registerMessageId(msg.chat_id, msg.id, msg.sequence_number);

  const content: ApiMessage['content'] = {
    text: msg.content ? {
      text: msg.content,
      entities: msg.entities?.map(buildApiEntity) || [],
    } : { text: '', entities: [] },
  };

  // Map media_attachments to TG Web A content fields
  if (msg.media_attachments?.length) {
    buildMediaContent(content, msg.media_attachments);
  }

  return {
    id: msg.sequence_number,
    chatId: msg.chat_id,
    // Store Saturn UUID so delete/pin/edit work even after page reload (UUID map is in-memory only)
    saturnId: msg.id,
    date: Math.floor(new Date(msg.created_at).getTime() / 1000),
    isOutgoing: false, // Will be set by caller based on current user
    sendingState: undefined, // Server-confirmed message — clear pending state
    senderId: msg.sender_id || undefined,
    content,
    isEdited: msg.is_edited,
    editDate: msg.edited_at ? Math.floor(new Date(msg.edited_at).getTime() / 1000) : undefined,
    isPinned: msg.is_pinned,
    isDeleting: msg.is_deleted,
    replyInfo: msg.reply_to_id ? buildReplyInfo(msg) : undefined,
    forwardInfo: msg.is_forwarded ? buildForwardInfo(msg) : undefined,
  };
}

// Build full media URL from backend-relative path (/media/:id → gateway presigned redirect)
function fullMediaUrl(relativePath: string): string {
  if (!relativePath) return '';
  return `${getBaseUrl()}${relativePath}`;
}

function buildMediaContent(content: ApiMessage['content'], attachments: SaturnMediaAttachment[]) {
  const first = attachments[0];
  if (!first) return;

  switch (first.type) {
    case 'photo': {
      content.photo = {
        mediaType: 'photo',
        id: first.media_id,
        date: 0,
        thumbnail: first.thumbnail_url ? {
          width: first.width || 320,
          height: first.height || 320,
          dataUri: fullMediaUrl(first.thumbnail_url),
        } : undefined,
        sizes: [
          {
            width: first.width || 320,
            height: first.height || 320,
            type: 's' as const,
          },
          {
            width: first.width || 800,
            height: first.height || 800,
            type: 'y' as const,
          },
        ],
        blobUrl: first.url ? fullMediaUrl(first.url) : undefined,
        isSpoiler: first.is_spoiler || undefined,
      };
      break;
    }
    case 'video':
    case 'videonote': {
      content.video = {
        mediaType: 'video',
        id: first.media_id,
        mimeType: first.mime_type || 'video/mp4',
        duration: first.duration_seconds || 0,
        width: first.width || 0,
        height: first.height || 0,
        fileName: first.original_filename || 'video.mp4',
        size: first.size_bytes,
        isRound: first.type === 'videonote' || undefined,
        thumbnail: first.thumbnail_url ? {
          width: first.width || 320,
          height: first.height || 320,
          dataUri: fullMediaUrl(first.thumbnail_url),
        } : undefined,
        blobUrl: first.url ? fullMediaUrl(first.url) : undefined,
        isSpoiler: first.is_spoiler || undefined,
      };
      break;
    }
    case 'voice': {
      content.voice = {
        mediaType: 'voice',
        id: first.media_id,
        duration: first.duration_seconds || 0,
        waveform: first.waveform_data || [],
        size: first.size_bytes,
      };
      break;
    }
    case 'gif': {
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
        thumbnail: first.thumbnail_url ? {
          width: first.width || 320,
          height: first.height || 320,
          dataUri: fullMediaUrl(first.thumbnail_url),
        } : undefined,
        blobUrl: first.url ? fullMediaUrl(first.url) : undefined,
      };
      break;
    }
    case 'file':
    default: {
      content.document = {
        mediaType: 'document',
        id: first.media_id,
        mimeType: first.mime_type || 'application/octet-stream',
        fileName: first.original_filename || 'file',
        size: first.size_bytes,
      };
      break;
    }
  }
}

function buildReplyInfo(msg: SaturnMessage) {
  if (!msg.reply_to_id) return undefined;

  // Try local map first, then use server-provided sequence_number as fallback
  const replySeqNum = uuidToSeqMap.get(msg.reply_to_id) || msg.reply_to_sequence_number;
  if (!replySeqNum) return undefined;

  // Register the mapping if we got it from the server
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

function buildApiEntity(e: SaturnMessageEntity): ApiMessageEntity {
  const base = { type: e.type as ApiMessageEntity['type'], offset: e.offset, length: e.length };
  if (e.url) return { ...base, type: 'MessageEntityTextUrl' as const, url: e.url };
  if (e.language) return { ...base, type: 'MessageEntityPre' as const, language: e.language };
  if (e.user_id) return { ...base, type: 'MessageEntityMentionName' as const, userId: e.user_id };
  return base as ApiMessageEntity;
}

export function buildSaturnEntities(entities: ApiMessageEntity[]): SaturnMessageEntity[] {
  return entities.map((e) => {
    const base: SaturnMessageEntity = { type: e.type, offset: e.offset, length: e.length };
    if ('url' in e) base.url = e.url;
    if ('language' in e) base.language = e.language;
    if ('userId' in e) base.user_id = e.userId;
    return base;
  });
}
