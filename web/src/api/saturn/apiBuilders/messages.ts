import type { ApiMessage } from '../../types';
import type { SaturnMessage } from '../types';

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
    date: Math.floor(new Date(msg.created_at).getTime() / 1000),
    isOutgoing: false, // Will be set by caller based on current user
    senderId: msg.sender_id || undefined,
    content: {
      text: msg.content ? {
        text: msg.content,
        entities: [],
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

  const replySeqNum = uuidToSeqMap.get(msg.reply_to_id);
  if (!replySeqNum) return undefined;

  return {
    type: 'message' as const,
    replyMessageId: replySeqNum,
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
