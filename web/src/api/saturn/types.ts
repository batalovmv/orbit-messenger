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
  default_permissions: number;
  slow_mode_seconds: number;
  is_signatures: boolean;
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
  media_attachments?: SaturnMediaAttachment[];
}

export interface SaturnMediaAttachment {
  media_id: string;
  type: 'photo' | 'video' | 'file' | 'voice' | 'videonote' | 'gif';
  mime_type: string;
  url?: string;
  thumbnail_url?: string;
  medium_url?: string;
  original_filename?: string;
  size_bytes: number;
  width?: number;
  height?: number;
  duration_seconds?: number;
  waveform_data?: number[];
  position: number;
  is_spoiler: boolean;
  is_one_time: boolean;
  processing_status: string;
}

export interface SaturnSharedMediaItem {
  message_id: string;
  sequence_number: number;
  chat_id: string;
  sender_id: string;
  content?: string;
  created_at: string;
  attachment: SaturnMediaAttachment;
}

export interface SaturnChatMember {
  chat_id: string;
  user_id: string;
  role: 'owner' | 'admin' | 'member' | 'readonly' | 'banned';
  permissions: number;
  custom_title?: string;
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

export interface SaturnInviteLink {
  id: string;
  chat_id: string;
  creator_id: string;
  hash: string;
  title?: string;
  expire_at?: string;
  usage_limit: number;
  usage_count: number;
  requires_approval: boolean;
  is_revoked: boolean;
  created_at: string;
}

export interface SaturnJoinRequest {
  chat_id: string;
  user_id: string;
  message?: string;
  status: 'pending' | 'approved' | 'rejected';
  reviewed_by?: string;
  created_at: string;
  display_name: string;
  avatar_url?: string;
}

// Phase 4: Settings, Privacy, Search

export interface SaturnPrivacySettings {
  user_id: string;
  last_seen: 'everyone' | 'contacts' | 'nobody';
  avatar: 'everyone' | 'contacts' | 'nobody';
  phone: 'everyone' | 'contacts' | 'nobody';
  calls: 'everyone' | 'contacts' | 'nobody';
  groups: 'everyone' | 'contacts' | 'nobody';
  forwarded: 'everyone' | 'contacts' | 'nobody';
  created_at: string;
  updated_at: string;
}

export interface SaturnUserSettings {
  user_id: string;
  theme: 'auto' | 'light' | 'dark';
  language: string;
  font_size: number;
  send_by_enter: boolean;
  dnd_from?: string;
  dnd_until?: string;
  created_at: string;
  updated_at: string;
}

export interface SaturnNotificationSettings {
  user_id: string;
  chat_id: string;
  muted_until?: string;
  sound: string;
  show_preview: boolean;
}

export interface SaturnBlockedUser {
  user_id: string;
  blocked_user_id: string;
  created_at: string;
  display_name: string;
  avatar_url?: string;
}

export interface SaturnPushSubscription {
  id: string;
  user_id: string;
  endpoint: string;
  p256dh: string;
  auth: string;
  user_agent?: string;
  created_at: string;
}

export interface SaturnSearchResponse {
  results: Record<string, unknown>[];
  total: number;
  query: string;
  scope: 'messages' | 'users' | 'chats';
}

// WebSocket message format
export interface SaturnWsMessage {
  type: string;
  data: Record<string, unknown>;
}
