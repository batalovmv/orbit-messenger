import type { Page } from '@playwright/test';
import { randomUUID } from 'crypto';

// ---- Saturn API shapes (mirrors src/api/saturn/types.ts) ----

export interface SaturnUser {
  id: string;
  email: string;
  display_name: string;
  avatar_url?: string;
  status: 'online' | 'offline' | 'recently';
  role: 'admin' | 'member';
  created_at: string;
  updated_at: string;
}

export interface SaturnMessage {
  id: string;
  chat_id: string;
  sender_id: string;
  type: string;
  content: string;
  entities: unknown[];
  is_edited: boolean;
  is_deleted: boolean;
  is_pinned: boolean;
  is_forwarded: boolean;
  sequence_number: number;
  created_at: string;
  edited_at?: string;
  sender_name: string;
  reactions?: Array<{
    emoji: string;
    count: number;
    user_ids: string[];
  }>;
  poll?: {
    id: string;
    message_id: string;
    question: string;
    is_anonymous: boolean;
    is_multiple: boolean;
    is_quiz: boolean;
    correct_option?: number;
    is_closed: boolean;
    close_at?: string;
    total_voters: number;
    created_at: string;
    options: Array<{
      id: string;
      poll_id: string;
      text: string;
      position: number;
      voters: number;
      is_chosen?: boolean;
      is_correct?: boolean;
    }>;
  };
}

export interface SaturnChatListItem {
  id: string;
  type: 'direct' | 'group' | 'channel';
  name?: string;
  is_encrypted: boolean;
  max_members: number;
  created_at: string;
  updated_at: string;
  last_message?: SaturnMessage;
  member_count: number;
  unread_count: number;
  other_user?: SaturnUser;
}

export interface SaturnChatMember {
  chat_id: string;
  user_id: string;
  role: string;
  last_read_message_id?: string;
  joined_at: string;
  notification_level: string;
  display_name: string;
}

// ---- WS event builders ----

export function buildWsNewMessage(msg: SaturnMessage) {
  return { type: 'new_message', data: msg };
}

export function buildWsMessageDeleted(chatId: string, sequenceNumber: number) {
  return { type: 'message_deleted', data: { chat_id: chatId, sequence_number: sequenceNumber } };
}

export function buildWsMessageUpdated(msg: SaturnMessage) {
  return { type: 'message_updated', data: msg };
}

export function buildWsMessagesRead(chatId: string, userId: string, lastReadMessageId: string) {
  return { type: 'messages_read', data: { chat_id: chatId, user_id: userId, last_read_message_id: lastReadMessageId } };
}

// ---- MockServerState ----

export class MockServerState {
  alice: SaturnUser;
  bob: SaturnUser;
  chatId: string;
  messages: SaturnMessage[] = [];
  private seqCounter = 0;
  private tsOffset = 0;
  lastReadByUserId: Record<string, string> = {};
  allPages: Page[] = [];

  constructor() {
    const now = new Date().toISOString();
    this.chatId = randomUUID();

    this.alice = {
      id: randomUUID(),
      email: 'alice@test.local',
      display_name: 'Alice Test',
      status: 'online',
      role: 'admin',
      created_at: now,
      updated_at: now,
    };

    this.bob = {
      id: randomUUID(),
      email: 'bob@test.local',
      display_name: 'Bob Test',
      status: 'online',
      role: 'member',
      created_at: now,
      updated_at: now,
    };
  }

  reset() {
    this.messages = [];
    this.seqCounter = 0;
    this.tsOffset = 0;
    this.lastReadByUserId = {};
    this.allPages = [];
  }

  buildSaturnMessage(senderId: string, text: string): SaturnMessage {
    this.seqCounter++;
    this.tsOffset++;
    const sender = senderId === this.alice.id ? this.alice : this.bob;
    return {
      id: randomUUID(),
      chat_id: this.chatId,
      sender_id: senderId,
      type: 'text',
      content: text,
      entities: [],
      is_edited: false,
      is_deleted: false,
      is_pinned: false,
      is_forwarded: false,
      sequence_number: this.seqCounter,
      created_at: new Date(Date.now() + this.tsOffset).toISOString(),
      sender_name: sender.display_name,
    };
  }

  addMessage(senderId: string, text: string): SaturnMessage {
    const msg = this.buildSaturnMessage(senderId, text);
    this.messages.push(msg);
    return msg;
  }

  buildPollMessage(senderId: string, {
    question = 'Poll question',
    answers = ['Option 1', 'Option 2'],
    totalVoters = 0,
    isClosed = false,
    isAnonymous = false,
    isMultiple = false,
    isQuiz = false,
  }: {
    question?: string;
    answers?: string[];
    totalVoters?: number;
    isClosed?: boolean;
    isAnonymous?: boolean;
    isMultiple?: boolean;
    isQuiz?: boolean;
  } = {}): SaturnMessage {
    const msg = this.buildSaturnMessage(senderId, '');
    const pollId = randomUUID();

    return {
      ...msg,
      type: 'poll',
      content: '',
      poll: {
        id: pollId,
        message_id: msg.id,
        question,
        is_anonymous: isAnonymous,
        is_multiple: isMultiple,
        is_quiz: isQuiz,
        is_closed: isClosed,
        total_voters: totalVoters,
        created_at: msg.created_at,
        options: answers.map((text, position) => ({
          id: `${pollId}-option-${position + 1}`,
          poll_id: pollId,
          text,
          position,
          voters: 0,
        })),
      },
    };
  }

  seedMessage(message: SaturnMessage) {
    this.messages.push(message);
    this.seqCounter = Math.max(this.seqCounter, message.sequence_number);
  }

  findByUuid(uuid: string): SaturnMessage | undefined {
    return this.messages.find((m) => m.id === uuid);
  }

  findBySeq(seq: number): SaturnMessage | undefined {
    return this.messages.find((m) => m.sequence_number === seq);
  }

  deleteMessage(uuid: string): SaturnMessage | undefined {
    const idx = this.messages.findIndex((m) => m.id === uuid);
    if (idx === -1) return undefined;
    const [removed] = this.messages.splice(idx, 1);
    return removed;
  }

  editMessage(uuid: string, newText: string): SaturnMessage | undefined {
    const msg = this.findByUuid(uuid);
    if (!msg) return undefined;
    msg.content = newText;
    msg.is_edited = true;
    msg.edited_at = new Date().toISOString();
    return msg;
  }

  // Messages in DESC order (matches backend store)
  getMessagesDesc(): SaturnMessage[] {
    return [...this.messages].sort((a, b) => b.sequence_number - a.sequence_number);
  }

  unreadCountForUser(userId: string): number {
    const lastRead = this.lastReadByUserId[userId];
    if (!lastRead) return this.messages.length;
    const lastReadMsg = this.findByUuid(lastRead);
    if (!lastReadMsg) return this.messages.length;
    return this.messages.filter((m) => m.sequence_number > lastReadMsg.sequence_number).length;
  }

  buildChatListItem(forUserId: string): SaturnChatListItem {
    const otherUser = forUserId === this.alice.id ? this.bob : this.alice;
    const sorted = this.getMessagesDesc();
    return {
      id: this.chatId,
      type: 'direct',
      is_encrypted: false,
      max_members: 2,
      created_at: this.alice.created_at,
      updated_at: this.alice.created_at,
      last_message: sorted[0],
      member_count: 2,
      unread_count: this.unreadCountForUser(forUserId),
      other_user: otherUser,
    };
  }

  buildMembers(): SaturnChatMember[] {
    return [
      {
        chat_id: this.chatId,
        user_id: this.alice.id,
        role: 'owner',
        joined_at: this.alice.created_at,
        notification_level: 'all',
        display_name: this.alice.display_name,
      },
      {
        chat_id: this.chatId,
        user_id: this.bob.id,
        role: 'member',
        joined_at: this.bob.created_at,
        notification_level: 'all',
        display_name: this.bob.display_name,
      },
    ];
  }

  async broadcastWs(event: Record<string, unknown>) {
    const promises = this.allPages.map((page) =>
      page.evaluate((evt) => {
        (window as any).__TEST_WS__?.onmessage?.({ data: JSON.stringify(evt) });
      }, event).catch(() => { /* page may be closed */ }),
    );
    await Promise.all(promises);
  }

  async pushWsToPage(page: Page, event: Record<string, unknown>) {
    await page.evaluate((evt) => {
      (window as any).__TEST_WS__?.onmessage?.({ data: JSON.stringify(evt) });
    }, event).catch(() => {});
  }
}
