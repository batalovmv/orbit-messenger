import type { ApiMessage, ApiMessageEntity } from '../../types';
import type { SaturnMessage, SaturnMessageEntity } from '../types';

// Saturn uses UUID for message IDs, but TG Web A expects numeric IDs.
// We use sequence_number as the numeric message ID.
// UUID is stored in a mapping for API calls that need it.

const uuidToSeqMap = new Map<string, number>();
const seqToUuidMap = new Map<string, Map<number, string>>(); // chatId -> seqNum -> uuid

export function registerMessageId(chatId: string, uuid: string, sequenceNumber: number) {
  uuidToSeqMap.set(uuid, sequenceNumber);
  let chatMap = seqToUuidMap.get(chatId);
  if (!chatMap) {
    chatMap = new Map();
    seqToUuidMap.set(chatId, chatMap);
  }
  chatMap.set(sequenceNumber, uuid);
}

export function getMessageUuid(chatId: string, seqNum: number): string | undefined {
  return seqToUuidMap.get(chatId)?.get(seqNum);
}

export function getMessageSeqNum(uuid: string): number | undefined {
  return uuidToSeqMap.get(uuid);
}

export function buildApiMessage(msg: SaturnMessage): ApiMessage {
  registerMessageId(msg.chat_id, msg.id, msg.sequence_number);

  return {
    id: msg.sequence_number,
    chatId: msg.chat_id,
    // Store Saturn UUID so delete/pin/edit work even after page reload (UUID map is in-memory only)
    saturnId: msg.id,
    date: Math.floor(new Date(msg.created_at).getTime() / 1000),
    isOutgoing: false, // Will be set by caller based on current user
    sendingState: undefined, // Server-confirmed message — clear pending state
    senderId: msg.sender_id || undefined,
    content: {
      text: msg.content ? {
        text: msg.content,
        entities: msg.entities?.map(buildApiEntity) || [],
      } : undefined,
    },
    isEdited: msg.is_edited,
    editDate: msg.edited_at ? Math.floor(new Date(msg.edited_at).getTime() / 1000) : undefined,
    isPinned: msg.is_pinned,
    isDeleting: msg.is_deleted,
    replyInfo: msg.reply_to_id ? buildReplyInfo(msg) : undefined,
    forwardInfo: msg.is_forwarded ? buildForwardInfo(msg) : undefined,
  };
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
