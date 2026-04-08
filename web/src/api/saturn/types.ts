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
  role: 'superadmin' | 'compliance' | 'admin' | 'member';
  is_active: boolean;
  deactivated_at?: string;
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
  is_pinned?: boolean;
  is_muted?: boolean;
  is_archived?: boolean;
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
  reactions?: SaturnReactionSummary[];
  poll?: SaturnPoll;
  grouped_id?: string;
  is_one_time?: boolean;
  viewed_at?: string;
  viewed_by?: string;
}

export interface SaturnMediaAttachment {
  media_id: string;
  type: 'photo' | 'video' | 'file' | 'voice' | 'videonote' | 'gif' | 'sticker';
  mime_type: string;
  page_count?: number;
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
  is_pinned?: boolean;
  is_muted?: boolean;
  is_archived?: boolean;
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

export interface SaturnSearchResponse<Result = Record<string, unknown>> {
  results: Result[];
  total: number;
  query: string;
  scope: 'messages' | 'users' | 'chats';
}

export interface SaturnSearchMatchRegion {
  start: number;
  length: number;
}

export type SaturnSearchMatchPosition = Record<string, SaturnSearchMatchRegion[]>;

export interface SaturnMessageSearchHit {
  id: string;
  chat_id: string;
  sender_id?: string;
  content?: string;
  type?: string;
  has_media?: boolean;
  created_at_ts: number;
  sequence_number: number;
  _matchesPosition?: SaturnSearchMatchPosition;
}

export interface SaturnUserSearchHit {
  id: string;
  email?: string;
  phone?: string;
  display_name?: string;
  avatar_url?: string;
  bio?: string;
  status?: 'online' | 'offline' | 'recently';
  custom_status?: string;
  custom_status_emoji?: string;
  role?: 'superadmin' | 'compliance' | 'admin' | 'member';
  totp_enabled?: boolean;
  invited_by?: string;
  last_seen_at?: string;
  created_at?: string;
  updated_at?: string;
}

export interface SaturnChatSearchHit {
  id: string;
  type?: 'direct' | 'group' | 'channel';
  name?: string;
  description?: string;
  avatar_url?: string;
  created_by?: string;
  is_encrypted?: boolean;
  max_members?: number;
  created_at?: string;
  updated_at?: string;
  default_permissions?: number;
  slow_mode_seconds?: number;
  is_signatures?: boolean;
}

export interface SaturnReactionSummary {
  emoji: string;
  count: number;
  user_ids: string[];
}

export interface SaturnReaction {
  message_id: string;
  user_id: string;
  emoji: string;
  created_at: string;
  display_name?: string;
  avatar_url?: string;
}

export interface SaturnChatAvailableReactions {
  chat_id: string;
  mode: 'all' | 'selected' | 'none';
  allowed_emojis?: string[];
  updated_at?: string;
}

export interface SaturnSticker {
  id: string;
  pack_id: string;
  emoji?: string;
  file_url: string;
  preview_url?: string;
  thumbnail_url?: string;
  file_type: 'webp' | 'tgs' | 'webm' | 'svg';
  width?: number;
  height?: number;
  position: number;
  is_custom_emoji?: boolean;
  is_free?: boolean;
  should_use_text_color?: boolean;
}

export interface SaturnStickerPack {
  id: string;
  title: string;
  short_name: string;
  description?: string;
  author_id?: string;
  thumbnail_url?: string;
  is_official: boolean;
  is_featured?: boolean;
  is_animated: boolean;
  sticker_count: number;
  stickers?: SaturnSticker[];
  is_installed?: boolean;
  created_at: string;
  updated_at: string;
}

export interface SaturnSavedGIF {
  id: string;
  user_id: string;
  tenor_id: string;
  url: string;
  preview_url?: string;
  width?: number;
  height?: number;
  created_at: string;
}

export interface SaturnTenorGIF {
  tenor_id: string;
  url: string;
  preview_url: string;
  width: number;
  height: number;
  title?: string;
}

export interface SaturnPoll {
  id: string;
  message_id: string;
  question: string;
  is_anonymous: boolean;
  is_multiple: boolean;
  is_quiz: boolean;
  correct_option?: number;
  solution?: string;
  solution_entities?: SaturnMessageEntity[];
  is_closed: boolean;
  close_at?: string;
  options: SaturnPollOption[];
  total_voters: number;
  created_at: string;
}

export interface SaturnPollOption {
  id: string;
  poll_id: string;
  text: string;
  position: number;
  voters: number;
  is_chosen?: boolean;
  is_correct?: boolean;
}

export interface SaturnPollVote {
  poll_id: string;
  option_id: string;
  user_id: string;
  voted_at: string;
}

export interface SaturnScheduledPoll {
  question: string;
  options: string[];
  is_anonymous: boolean;
  is_multiple: boolean;
  is_quiz: boolean;
  correct_option?: number;
  solution?: string;
  solution_entities?: SaturnMessageEntity[];
}

export interface SaturnScheduledMessage {
  id: string;
  chat_id: string;
  sender_id: string;
  content?: string;
  entities?: SaturnMessageEntity[];
  reply_to_id?: string;
  reply_to_sequence_number?: number;
  type: string;
  media_attachments?: SaturnMediaAttachment[];
  is_spoiler?: boolean;
  poll?: SaturnScheduledPoll;
  scheduled_at: string;
  is_sent: boolean;
  sent_at?: string;
  created_at: string;
  updated_at: string;
}

// WebSocket message format
export interface SaturnWsMessage {
  type: string;
  data: Record<string, unknown>;
}

// Admin / Audit types
export interface SaturnAuditEntry {
  id: number;
  actor_id: string;
  action: string;
  target_type: string;
  target_id?: string;
  details?: Record<string, unknown>;
  ip_address?: string;
  user_agent?: string;
  created_at: string;
  actor_name?: string;
}
