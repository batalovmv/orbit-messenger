// Saturn backend response types (JSON shapes from REST API)

export interface SaturnUser {
  id: string;
  email?: string;
  phone?: string;
  display_name: string;
  avatar_url?: string;
  bio?: string;
  status: 'online' | 'offline' | 'recently';
  custom_status?: string;
  custom_status_emoji?: string;
  role: 'admin' | 'member';
  totp_enabled?: boolean;
  invited_by?: string;
  last_seen_at?: string;
  created_at: string;
  updated_at: string;
}

export interface SaturnChat {
  id: string;
  type: 'direct' | 'group' | 'channel';
  name?: string;
  description?: string;
  avatar_url?: string;
  created_by?: string;
  is_encrypted: boolean;
  max_members: number;
  created_at: string;
  updated_at: string;
}

export interface SaturnChatListItem extends SaturnChat {
  last_message?: SaturnMessage;
  member_count: number;
  unread_count: number;
  other_user?: SaturnUser;
}

export interface SaturnMessageEntity {
  type: string;
  offset: number;
  length: number;
  url?: string;
  language?: string;
  user_id?: string;
}

export interface SaturnMessage {
  id: string;
  chat_id: string;
  sender_id?: string;
  type: string;
  content?: string;
  entities?: SaturnMessageEntity[];
  reply_to_id?: string;
  reply_to_sequence_number?: number;
  is_edited: boolean;
  is_deleted: boolean;
  is_pinned: boolean;
  is_forwarded: boolean;
  forwarded_from?: string;
  sequence_number: number;
  created_at: string;
  edited_at?: string;
  sender_name: string;
  sender_avatar_url?: string;
}

export interface SaturnChatMember {
  chat_id: string;
  user_id: string;
  role: 'owner' | 'admin' | 'member' | 'readonly' | 'banned';
  last_read_message_id?: string;
  joined_at: string;
  muted_until?: string;
  notification_level: 'all' | 'mentions' | 'none';
  display_name: string;
  avatar_url?: string;
}

export interface SaturnSession {
  id: string;
  user_id: string;
  device_id?: string;
  ip_address?: string;
  user_agent?: string;
  expires_at: string;
  created_at: string;
}

export interface SaturnInvite {
  id: string;
  code: string;
  created_by?: string;
  email?: string;
  role: string;
  max_uses: number;
  use_count: number;
  used_by?: string;
  used_at?: string;
  expires_at?: string;
  is_active: boolean;
  created_at: string;
}

export interface SaturnLoginResponse {
  access_token: string;
  expires_in: number;
  user: SaturnUser;
}

export interface SaturnPaginatedResponse<T> {
  data: T[];
  cursor?: string;
  has_more: boolean;
}

export interface SaturnErrorResponse {
  error: string;
  message: string;
  status: number;
}

// WebSocket message format
export interface SaturnWsMessage {
  type: string;
  data: Record<string, unknown>;
}
