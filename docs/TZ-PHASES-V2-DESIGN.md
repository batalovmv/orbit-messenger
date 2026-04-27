# Orbit Messenger — Phases v2 Design

> **Date:** 2026-03-23
> **Author:** Claude (CTO) + Alex (Architect)
> **Status:** Approved

---

## Context & Problem

Orbit Messenger had **two conflicting phase systems**:
- `CLAUDE.md`: 7 phases (0-7) describing a "build from scratch" React 19 + Vite + shadcn stack
- `PHASES.md`: 11 phases (1-11) with different grouping

**Reality:** The frontend is a **Telegram Web A fork** (931 components, Teact + Webpack + SCSS). All UI exists. The work = implement Saturn API backend methods to power existing UI.

### Key decisions made:
- **Frontend:** TG Web A fork is the only frontend. No React 19 rewrite.
- **Backend:** 8 Go microservices from the start (gateway, auth, messaging, media, calls, ai, bots, integrations)
- **Timeline:** No hard deadline. Each phase = usable release.
- **MVP scope:** Full Telegram-like experience (text + groups + media + calls + push + stickers). No Premium paywall — all features free.
- **Desktop/Mobile:** Parallel track (Tauri + PWA), not a blocking phase.

### Current state (2026-03-23):
- Auth service: **complete** (JWT, httpOnly cookies, 2FA TOTP, invite flow)
- Saturn API methods: **15 of 419** implemented (3.6%)
- WebSocket: connection works, events not dispatched
- Known bugs: sendingState clock icon, Weekday.Today raw key

---

## Phase Overview (8 phases)

| # | Phase | Services | Saturn Methods | Key Result |
|---|-------|----------|---------------|------------|
| 1 | Core Messaging | gateway, messaging | ~35 | DM chats, reply/forward/edit, typing, read receipts |
| 2 | Groups & Channels | messaging+ | ~30 | Groups, channels, roles, permissions, pin, @mentions |
| 3 | Media & Files | media (new) | ~25 | Photos, videos, files, voice notes, video notes |
| 4 | Search, Notifications & Settings | gateway+, Meilisearch | ~25 | Search, push, privacy, settings |
| 5 | Rich Messaging | messaging+ | ~40 | Reactions, stickers, GIF, polls, no-Premium, scheduled |
| 6 | Calls | calls (new), coturn, Pion | ~20 | Voice/video P2P + group + screen sharing |
| 7 | E2E Encryption | auth+, messaging+, shared/crypto | ~15 | Signal Protocol, Safety Numbers, disappearing messages |
| 8 | AI, Bots & Production | ai, bots, integrations + hardening | ~30 | Claude AI, Bot API, webhooks, ScyllaDB, monitoring |
| — | Desktop & Mobile | — | 0 | Tauri + PWA (parallel from phase 4+) |
| | **Total** | **8 services** | **~220** | **Full Telegram-like for 150+ users** |

---

## Phase 1: Core Messaging

**Services:** gateway (WebSocket), messaging (CRUD + events)

**Goal:** People can text each other in DM. Reliably. With reply/forward/edit, like Telegram.

### Backend endpoints

| Endpoint | Status | Purpose |
|----------|--------|---------|
| `POST /chats/:id/messages` | ✅ exists | Send message |
| `GET /chats/:id/messages` | ✅ exists | Load history (cursor pagination) |
| `PATCH /messages/:id` | ⚠️ stub | Edit message |
| `DELETE /messages/:id` | ⚠️ stub | Delete (soft delete) |
| `POST /chats/direct` | ✅ exists | Create DM |
| `GET /users/me` | ✅ exists | Current user |
| `GET /users/:id` | ✅ exists | Profile |
| `PUT /users/me` | ✅ exists | Update profile |
| `GET /users?q=` | ✅ exists | Search users |
| `PATCH /chats/:id/read` | ✅ exists | Mark read |
| `POST /messages/:id/forward` | 🔴 new | Forward message |
| `GET /chats/:id/history` | 🔴 new | Jump to message / scroll to date |

### WebSocket events (gateway)

| Event | Status | Purpose |
|-------|--------|---------|
| `new_message` | ⚠️ connection exists, dispatch missing | New message |
| `message_updated` | 🔴 | Edit |
| `message_deleted` | 🔴 | Delete |
| `typing` | 🔴 | "Alice is typing..." |
| `user_status` | 🔴 | Online/offline |
| `messages_read` | 🔴 | Read receipts ✓✓ |

### Saturn API methods to implement (~35)

**Already working (15):** `fetchChats`, `fetchMessages`, `sendMessage`, `editMessage`, `deleteMessages`, `fetchUser`, `searchUsers`, `createDirectChat`, `fetchFullChat`, `getChatInviteLink`, `updateProfile`, `markMessageListRead`, `fetchCurrentUser`, `createGroupChat`, `fetchGlobalUsers`

**New:**
- `forwardMessages` — POST /messages/:id/forward
- `fetchMessageLink` — deep link to message
- `reportMessages` — report
- `fetchScheduledHistory` / `sendScheduledMessages` / `deleteScheduledMessages`
- `readHistory` — extended mark as read
- `fetchUpdateManager` — sync state on reconnect
- `fetchDifference` — get missed updates
- Update `sendMessage` for replyTo, entities (bold/italic/code)
- Update `fetchMessages` for jump-to-message by ID
- `fetchPinnedMessages` / `pinMessage` / `unpinAllMessages` / `toggleMessagePinned`

### Critical bugs to fix
- ❌ Sent messages clock icon persists (sendingState)
- ❌ WebSocket events not dispatched to UI
- ❌ `Weekday.Today` raw key
- ❌ IndexedDB sync on chat re-navigation

### Phase 1 result
User logs in → sees chat list → opens DM → sends message → sees typing → sees ✓✓ → reply → edit → forward → pin. All real-time via WebSocket.

---

## Phase 2: Groups & Channels

**Services:** messaging (extend), gateway (WS events for groups)

**Goal:** Team spaces. Groups for discussions, channels for announcements.

### Backend endpoints

| Endpoint | Purpose |
|----------|---------|
| `POST /chats` (type=group) | Create group |
| `POST /chats` (type=channel) | Create channel |
| `PUT /chats/:id` | Edit name/description/avatar |
| `DELETE /chats/:id` | Delete/archive |
| `POST /chats/:id/members` | Add member |
| `DELETE /chats/:id/members/:userId` | Remove/leave |
| `PATCH /chats/:id/members/:userId` | Change role (owner/admin/member) |
| `GET /chats/:id/members` | Member list (paginated) |
| `GET /chats/:id/members/:userId` | Member info |
| `PUT /chats/:id/permissions` | Default group permissions |
| `PUT /chats/:id/members/:userId/permissions` | Per-user permissions |
| `POST /chats/:id/invite-link` | Generate invite link |
| `POST /chats/join/:inviteHash` | Join via link |
| `GET /chats/:id/admins` | Admin list |
| `POST /chats/:id/slow-mode` | Slow mode |

### WebSocket events (new)

| Event | Purpose |
|-------|---------|
| `chat_created` | New chat appeared for member |
| `chat_updated` | Name/photo/description changed |
| `chat_member_added` | Someone joined |
| `chat_member_removed` | Someone left/kicked |
| `chat_member_updated` | Role changed |

### Saturn API methods (~30)

`createChannel`, `editChatTitle`, `editChatDescription`, `updateChatPhoto`, `deleteChatPhoto`, `addChatMembers`, `deleteChatMember`, `leaveChat`, `deleteChat`, `deleteChannel`, `getChatMember`, `fetchMembers`, `updateChatMember`, `updateChatMemberBannedRights`, `updateChatAdmin`, `updateChannelAdmin`, `updateChatDefaultBannedRights`, `toggleChatIsProtected`, `toggleJoinToSend`, `toggleJoinRequest`, `exportChatInviteLink`, `editChatInviteLink`, `joinChat`, `fetchChatInviteInfo`, `toggleSlowMode`, `archiveChat`, `unarchiveChat`, `toggleChatPinned`, `setChatMuted`, `fetchTopics`, `createTopic`, `editTopic`, `deleteTopic`

### Permissions model

```
Owner  → everything
Admin  → manage members, pin, delete others' messages
Member → send (if allowed), read
Banned → read only (or nothing)
```

Bitmask permissions: `can_send_messages`, `can_send_media`, `can_add_members`, `can_pin_messages`, `can_change_info`, `can_delete_messages`, `can_ban_users`, `can_invite_via_link`

### Channels vs Groups

| | Group | Channel |
|-|-------|---------|
| Who writes | Everyone (by permissions) | Only admin/owner |
| Author visibility | Everyone | Anonymous (or channel name) |
| Comments | In the chat | Linked discussion group |
| Subscribers | Members | Readers |

### DB schema additions

```sql
ALTER TABLE chat_members ADD COLUMN role TEXT DEFAULT 'member';
ALTER TABLE chat_members ADD COLUMN permissions BIGINT DEFAULT 0;
ALTER TABLE chat_members ADD COLUMN custom_title TEXT;

CREATE TABLE chat_invite_links (
  id UUID PRIMARY KEY,
  chat_id UUID REFERENCES chats(id),
  creator_id UUID REFERENCES users(id),
  hash TEXT UNIQUE NOT NULL,
  expire_at TIMESTAMPTZ,
  usage_limit INT,
  usage_count INT DEFAULT 0,
  requires_approval BOOLEAN DEFAULT false,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE chat_join_requests (
  chat_id UUID REFERENCES chats(id),
  user_id UUID REFERENCES users(id),
  message TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  PRIMARY KEY (chat_id, user_id)
);
```

### Phase 2 result
Create "MST Dev Team" group → add 10 people → assign 2 admins → chat → pin important → invite link for new hires. Channel "MST Announcements" → owner posts, 150 read.

---

## Phase 3: Media & Files

**Services:** media (new), messaging (extend for media messages)

**Goal:** Send photos, videos, files, voice notes, video notes. Without this — it's not a messenger.

### Backend endpoints (media service)

| Endpoint | Purpose |
|----------|---------|
| `POST /media/upload` | Upload → R2, return media_id + URL |
| `POST /media/upload/chunked/init` | Start chunked upload (files >10MB) |
| `POST /media/upload/chunked/:uploadId` | Upload next chunk |
| `POST /media/upload/chunked/:uploadId/complete` | Finish chunked upload |
| `GET /media/:id` | Get file (presigned R2 URL, redirect) |
| `GET /media/:id/thumbnail` | Thumbnail (320px for photos, first frame for video) |
| `DELETE /media/:id` | Delete from R2 |
| `GET /media/:id/info` | Metadata: size, type, dimensions, duration |

### Server-side processing

| Type | Processing | Limits |
|------|-----------|--------|
| **Photo** | Resize → thumb 320px + medium 800px + original. EXIF strip. WebP convert | ≤ 10MB |
| **Video** | Extract first frame → thumbnail. Metadata (duration, resolution) | ≤ 2GB, stream via R2 presigned |
| **Files** | No processing. Icon by MIME type | ≤ 2GB |
| **Voice** | Waveform data (peak values for visualization). Duration | ≤ 200MB, OGG/WAV |
| **Video note** | Circular crop metadata. Duration ≤ 60s | ≤ 50MB, MP4 384px |
| **GIF** | Convert to MP4. Thumbnail | ≤ 20MB |

### Storage: Cloudflare R2

```
r2://orbit-media/
├── photos/{media_id}/original.webp, thumb_320.webp, medium_800.webp
├── videos/{media_id}/original.mp4, thumb.jpg
├── files/{media_id}/{original_filename}
├── voice/{media_id}/audio.ogg
└── videonote/{media_id}/video.mp4
```

### Message with media

```json
{
  "id": "msg-uuid",
  "text": "Check this sunset",
  "media_attachments": [
    {
      "media_id": "media-uuid",
      "type": "photo",
      "url": "https://r2.orbit.../photos/xxx/original.webp",
      "thumbnail_url": "https://r2.orbit.../photos/xxx/thumb_320.webp",
      "width": 1920,
      "height": 1080,
      "size_bytes": 245000
    }
  ]
}
```

### Saturn API methods (~25)

`uploadMedia`, `sendMediaMessage`, `downloadMedia`, `fetchMessageMedia`, `cancelMediaDownload`, `cancelMediaUpload`, `sendVoice`, `sendVideoNote`, `sendDocument`, `sendPhoto`, `sendVideo`, `sendAlbum`, `fetchSharedMedia`, `fetchCommonMedia`, `resendMedia`, `fetchMediaViewers`, `sendOneTimeMedia`, `openOneTimeMedia`, `fetchDocumentPreview`, `setMediaSpoiler`, `removeMediaSpoiler`

### WebSocket events

| Event | Purpose |
|-------|---------|
| `media_upload_progress` | Upload progress (for large files) |
| `media_ready` | Thumbnail/resize ready |

### DB schema

```sql
CREATE TABLE media (
  id UUID PRIMARY KEY,
  uploader_id UUID REFERENCES users(id),
  type TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  original_filename TEXT,
  size_bytes BIGINT NOT NULL,
  r2_key TEXT NOT NULL,
  thumbnail_r2_key TEXT,
  width INT,
  height INT,
  duration_seconds FLOAT,
  waveform_data BYTEA,
  is_one_time BOOLEAN DEFAULT false,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE message_media (
  message_id UUID REFERENCES messages(id),
  media_id UUID REFERENCES media(id),
  position INT DEFAULT 0,
  PRIMARY KEY (message_id, media_id)
);
```

### Phase 3 result
Drop photo in chat → see thumbnail → full image on click → gallery with swipes. Drag & drop file → progress bar → download. Hold mic → record voice → waveform in chat. 60s video note circle. PDF preview.

---

## Phase 4: Search, Notifications & Settings

**Services:** gateway (push), messaging (search), + Meilisearch (external)

**Goal:** Find any message in seconds. Never miss anything. Configure to your preference.

### Search — Meilisearch

| Endpoint | Purpose |
|----------|---------|
| `GET /search?q=&scope=messages` | Search messages (global or in-chat) |
| `GET /search?q=&scope=users` | Search users |
| `GET /search?q=&scope=chats` | Search chats/groups/channels |
| `GET /search?q=&scope=media` | Search by caption + filename |

**Filters:** `chat_id`, `from_user_id`, `date_from`/`date_to`, `type` (photo/video/file/voice/link), `has_media`

### Notifications — Three delivery channels

| Channel | Technology | When |
|---------|-----------|------|
| **Web Push** | VAPID | Browser in background / closed |
| **In-app** | WebSocket | App open, different chat |
| **FCM/APNs** | Firebase + Apple | Mobile / Desktop (later) |

| Endpoint | Purpose |
|----------|---------|
| `POST /push/subscribe` | Register push subscription |
| `DELETE /push/subscribe` | Unsubscribe |
| `PUT /users/me/notifications` | Global notification settings |
| `PUT /chats/:id/notifications` | Per-chat settings (mute, sound) |

**Delivery logic:**
1. New message → recipient online via WS? → in-app notification
2. Not online → Web Push / FCM / APNs
3. Chat muted? → don't push (except @mention)
4. DND hours? → don't push
5. @mention → ALWAYS push

### Settings

| Endpoint | Purpose |
|----------|---------|
| `GET/PUT /users/me/settings/privacy` | Last seen, avatar, calls, groups |
| `GET/PUT /users/me/settings/notifications` | Global notifications |
| `GET/PUT /users/me/settings/appearance` | Theme, language, font size |
| `GET /users/me/sessions` | Active sessions |
| `DELETE /users/me/sessions/:id` | Terminate session |
| `PUT /users/me/username` | Change @username |
| `PUT/DELETE /users/me/avatar` | Avatar management |

**Privacy options:** Last seen (everyone/contacts/nobody), Avatar, Phone, Calls, Group add, Forwarded messages link.

### Saturn API methods (~25)

**Search:** `searchMessages`, `searchChatMessages`, `fetchSearchHistory`, `searchHashtag`, `getMessageByDate`

**Notifications:** `registerDevice`, `unregisterDevice`, `updateNotifySettings`, `getNotifySettings`, `updateGlobalNotifySettings`, `resetNotifySettings`, `muteChat`, `unmuteChat`

**Settings:** `getPrivacySettings`, `setPrivacySettings`, `fetchBlockedUsers`, `blockUser`, `unblockUser`, `fetchActiveSessions`, `terminateSession`, `terminateAllSessions`, `updateUsername`, `checkUsername`, `fetchLanguageStrings`, `updateProfilePhoto`, `deleteProfilePhoto`

### DB schema

```sql
CREATE TABLE push_subscriptions (
  id UUID PRIMARY KEY,
  user_id UUID REFERENCES users(id),
  endpoint TEXT NOT NULL,
  p256dh TEXT NOT NULL,
  auth TEXT NOT NULL,
  user_agent TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE notification_settings (
  user_id UUID REFERENCES users(id),
  chat_id UUID REFERENCES chats(id),
  muted_until TIMESTAMPTZ,
  sound TEXT DEFAULT 'default',
  show_preview BOOLEAN DEFAULT true,
  PRIMARY KEY (user_id, chat_id)
);

CREATE TABLE privacy_settings (
  user_id UUID REFERENCES users(id) PRIMARY KEY,
  last_seen TEXT DEFAULT 'everyone',
  avatar TEXT DEFAULT 'everyone',
  phone TEXT DEFAULT 'contacts',
  calls TEXT DEFAULT 'everyone',
  groups TEXT DEFAULT 'everyone',
  forwarded TEXT DEFAULT 'everyone'
);

CREATE TABLE blocked_users (
  user_id UUID REFERENCES users(id),
  blocked_user_id UUID REFERENCES users(id),
  created_at TIMESTAMPTZ DEFAULT NOW(),
  PRIMARY KEY (user_id, blocked_user_id)
);

CREATE TABLE user_settings (
  user_id UUID REFERENCES users(id) PRIMARY KEY,
  theme TEXT DEFAULT 'auto',
  language TEXT DEFAULT 'ru',
  font_size INT DEFAULT 16,
  send_by_enter BOOLEAN DEFAULT true,
  dnd_from TIME,
  dnd_until TIME
);
```

### Phase 4 result
Global search: type "report" → find message from February → click → jump. In-chat: magnifier → "budget" → all mentions. Push: close tab → new message → Web Push → click → opens chat. Settings: hide last seen, mute noisy group, switch to dark theme.

---

## Phase 5: Rich Messaging

**Services:** messaging (extend), gateway (new WS events)

**Goal:** Reactions, stickers, GIF, polls, scheduled messages. What makes a messenger alive. ALL features free (no Premium paywall).

### Reactions

| Endpoint | Purpose |
|----------|---------|
| `POST /messages/:id/reactions` | Add reaction (emoji) |
| `DELETE /messages/:id/reactions` | Remove reaction |
| `GET /messages/:id/reactions` | Who reacted (user list) |
| `PUT /chats/:id/available-reactions` | Which reactions allowed (admin) |

### Stickers

| Endpoint | Purpose |
|----------|---------|
| `GET /stickers/featured` | Recommended packs |
| `GET /stickers/search?q=` | Search stickers |
| `GET /stickers/sets/:id` | Get pack |
| `POST /stickers/sets/:id/install` | Install pack |
| `DELETE /stickers/sets/:id/install` | Remove pack |
| `GET /stickers/installed` | My packs |
| `GET /stickers/recent` | Recent stickers |

**Formats:** Static (WebP), Animated (TGS/Lottie), Video (WebM). Stored in R2, indexed in Meilisearch.

**TG Import:** User enters `t.me/addstickers/packname` → backend fetches via TG Bot API → uploads to R2 → available for all Orbit users.

### GIF

| Endpoint | Purpose |
|----------|---------|
| `GET /gifs/search?q=` | Search (proxy to Tenor API) |
| `GET /gifs/trending` | Trending |
| `GET /gifs/saved` | Saved GIFs |
| `POST/DELETE /gifs/saved` | Save/remove |

### Polls

| Endpoint | Purpose |
|----------|---------|
| `POST /chats/:id/messages` (type=poll) | Create poll |
| `POST /messages/:id/poll/vote` | Vote |
| `DELETE /messages/:id/poll/vote` | Retract vote |
| `POST /messages/:id/poll/close` | Close poll |

### Scheduled Messages

| Endpoint | Purpose |
|----------|---------|
| `POST /chats/:id/messages?scheduled_at=` | Schedule message |
| `GET /chats/:id/messages/scheduled` | List scheduled |
| `PATCH /messages/:id/scheduled` | Edit time/text |
| `DELETE /messages/:id/scheduled` | Delete |
| `POST /messages/:id/scheduled/send-now` | Send immediately |

Go cron job checks every 10 seconds → sends messages where `scheduled_at <= now()`.

### No Premium — all features free

Remove all `isPremium` checks from TG Web A frontend (`return true` everywhere):
- Custom emoji in name/status → free for all
- Animated emoji in messages → free
- All sticker packs → free
- Emoji status → free
- Extended upload limits → free
- No ads ever

### Saturn API methods (~40)

**Reactions:** `sendReaction`, `fetchMessageReactionsList`, `fetchAvailableReactions`, `setDefaultReaction`, `setChatEnabledReactions`

**Stickers:** `fetchStickerSets`, `fetchRecentStickers`, `fetchFavoriteStickers`, `fetchFeaturedStickers`, `searchStickers`, `installStickerSet`, `uninstallStickerSet`, `addRecentSticker`, `removeRecentSticker`, `addFavoriteSticker`, `removeFavoriteSticker`, `fetchCustomEmoji`, `fetchCustomEmojiSets`

**GIF:** `fetchGifs`, `searchGifs`, `fetchSavedGifs`, `saveGif`, `removeGif`

**Polls:** `sendPoll`, `votePoll`, `closePoll`, `fetchPollVoters`

**Scheduled:** `fetchScheduledHistory`, `sendScheduledMessages`, `editScheduledMessage`, `deleteScheduledMessages`, `rescheduleMessage`

**Other:** `fetchSavedMessages`, `toggleSavedDialogPinned`, `fetchCommonChats`

### DB schema

```sql
CREATE TABLE message_reactions (
  message_id UUID REFERENCES messages(id),
  user_id UUID REFERENCES users(id),
  emoji TEXT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  PRIMARY KEY (message_id, user_id, emoji)
);

CREATE TABLE chat_available_reactions (
  chat_id UUID REFERENCES chats(id) PRIMARY KEY,
  mode TEXT DEFAULT 'all',
  allowed_emojis TEXT[]
);

CREATE TABLE user_installed_stickers (
  user_id UUID REFERENCES users(id),
  pack_id UUID REFERENCES sticker_packs(id),
  position INT DEFAULT 0,
  installed_at TIMESTAMPTZ DEFAULT NOW(),
  PRIMARY KEY (user_id, pack_id)
);

CREATE TABLE recent_stickers (
  user_id UUID REFERENCES users(id),
  sticker_id UUID REFERENCES stickers(id),
  used_at TIMESTAMPTZ DEFAULT NOW(),
  PRIMARY KEY (user_id, sticker_id)
);

CREATE TABLE polls (
  id UUID PRIMARY KEY,
  message_id UUID REFERENCES messages(id),
  question TEXT NOT NULL,
  is_anonymous BOOLEAN DEFAULT true,
  is_multiple BOOLEAN DEFAULT false,
  is_quiz BOOLEAN DEFAULT false,
  correct_option INT,
  close_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE poll_options (
  id UUID PRIMARY KEY,
  poll_id UUID REFERENCES polls(id),
  text TEXT NOT NULL,
  position INT NOT NULL
);

CREATE TABLE poll_votes (
  poll_id UUID REFERENCES polls(id),
  option_id UUID REFERENCES poll_options(id),
  user_id UUID REFERENCES users(id),
  voted_at TIMESTAMPTZ DEFAULT NOW(),
  PRIMARY KEY (poll_id, user_id, option_id)
);
```

### WebSocket events

| Event | Purpose |
|-------|---------|
| `reaction_added` / `reaction_removed` | Real-time reactions |
| `poll_vote` | Poll results update |
| `poll_closed` | Poll ended |

### Phase 5 result
Long-press message → reaction fire → animated for everyone. Sticker picker → install "MST Memes" → send. GIF: type "celebration" → send. Poll "Where for team party?" → 4 options → real-time voting. Schedule "Happy birthday!" for tomorrow 9:00.

---

## Phase 6: Voice & Video Calls

**Services:** calls (new), gateway (signaling via WS)

**Goal:** Call each other without Zoom. 1-on-1 and group. Screen sharing.

### Architecture

```
User A (browser)                    User B (browser)
    │                                    │
    ├── WebRTC PeerConnection ──────────┤  (P2P if possible)
    │                                    │
    │   ┌─────────────┐                 │
    ├──►│ TURN server  │◄───────────────┤  (relay if NAT blocks)
    │   │ (coturn)     │                 │
    │   └─────────────┘                 │
    │                                    │
    │   ┌─────────────┐                 │
    ├──►│ SFU server   │◄───────────────┤  (group calls)
    │   │ (Pion)       │                 │
    │   └─────────────┘                 │
    │                                    │
    └── WebSocket (signaling) ──────────┘
```

**P2P** — 1-on-1 calls. Direct browser-to-browser.
**TURN (coturn)** — relay when P2P impossible (corporate NAT). VPS on Hetzner.
**SFU (Pion)** — group calls up to 50 people. Each sends one stream, SFU distributes.

### Backend endpoints (calls service)

| Endpoint | Purpose |
|----------|---------|
| `POST /calls` | Initiate call (voice/video) |
| `PUT /calls/:id/accept` | Accept |
| `PUT /calls/:id/decline` | Decline |
| `PUT /calls/:id/end` | End call |
| `GET /calls/:id` | Call status |
| `GET /calls/history` | Call history |
| `POST /calls/:id/participants` | Add participant (group) |
| `DELETE /calls/:id/participants/:userId` | Remove participant |
| `PUT /calls/:id/mute` | Mute self |
| `PUT /calls/:id/screen-share/start` | Start screen sharing |
| `PUT /calls/:id/screen-share/stop` | Stop |
| `GET /calls/:id/ice-servers` | Get TURN/STUN credentials |

### WebSocket signaling

| Event | Direction | Purpose |
|-------|-----------|---------|
| `call_incoming` | server → client | Incoming call (ringtone) |
| `call_accepted` | server → client | Peer accepted |
| `call_declined` | server → client | Peer declined |
| `call_ended` | server → client | Call ended |
| `call_participant_joined` | server → client | Someone joined group call |
| `call_participant_left` | server → client | Someone left |
| `webrtc_offer` | client ↔ client | SDP offer |
| `webrtc_answer` | client ↔ client | SDP answer |
| `webrtc_ice_candidate` | client ↔ client | ICE candidate |
| `call_muted` / `call_unmuted` | both | Mic status |
| `screen_share_started/stopped` | both | Screen sharing |

### Saturn API methods (~20)

`createCall`, `acceptCall`, `declineCall`, `hangUp`, `joinGroupCall`, `leaveGroupCall`, `toggleCallMute`, `toggleCallCamera`, `startScreenShare`, `stopScreenShare`, `fetchCallParticipants`, `fetchCallHistory`, `rateCall`, `sendWebRtcOffer`, `sendWebRtcAnswer`, `sendIceCandidate`, `fetchIceServers`, `inviteToCall`, `setCallSpeaker`

### DB schema

```sql
CREATE TABLE calls (
  id UUID PRIMARY KEY,
  type TEXT NOT NULL, -- voice/video
  mode TEXT NOT NULL, -- p2p/group
  chat_id UUID REFERENCES chats(id),
  initiator_id UUID REFERENCES users(id),
  status TEXT DEFAULT 'ringing', -- ringing/active/ended/missed/declined
  started_at TIMESTAMPTZ,
  ended_at TIMESTAMPTZ,
  duration_seconds INT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE call_participants (
  call_id UUID REFERENCES calls(id),
  user_id UUID REFERENCES users(id),
  joined_at TIMESTAMPTZ,
  left_at TIMESTAMPTZ,
  is_muted BOOLEAN DEFAULT false,
  is_camera_off BOOLEAN DEFAULT false,
  is_screen_sharing BOOLEAN DEFAULT false,
  PRIMARY KEY (call_id, user_id)
);
```

### Infrastructure

| Service | Where | Purpose |
|---------|-------|---------|
| **coturn** | Hetzner VPS | TURN relay for NAT traversal |
| **Pion SFU** | Saturn.ac (near gateway) | Group calls |

### Phase 6 result
Phone button in chat → peer sees incoming → accepts → P2P voice. Video button → video call with camera. In group → "Start call" → 10 join → video grid → someone shares screen → everyone sees. Call history in profile.

---

## Phase 7: Security & E2E Encryption

**Services:** auth (key server), messaging (encrypt/decrypt), shared/crypto

**Goal:** Even the server admin cannot read DMs. Cryptographically guaranteed. Zero-Knowledge.

### Signal Protocol flow

```
Alice wants to message Bob (first time):
1. Alice → Server: "give me Bob's public keys"
2. Server → Alice: Bob's Identity Key + Signed PreKey + One-Time PreKey
3. Alice: X3DH handshake → computes shared secret
4. Alice: encrypts message AES-256-GCM → sends ciphertext
5. Bob: receives ciphertext → X3DH with Alice's keys → decrypts
6. Onwards: Double Ratchet — new key after EVERY message
```

### Key Server (auth service extension)

| Endpoint | Purpose |
|----------|---------|
| `POST /keys/identity` | Upload Identity Key (on registration, once) |
| `POST /keys/signed-prekey` | Upload Signed PreKey (rotate weekly) |
| `POST /keys/one-time-prekeys` | Upload batch of One-Time PreKeys (100) |
| `GET /keys/:userId/bundle` | Get key bundle to start session |
| `GET /keys/:userId/identity` | Get Identity Key (for Safety Numbers) |
| `GET /keys/count` | How many One-Time PreKeys left |
| `GET /keys/transparency-log` | Public key change log |

### Encrypted message format

```json
{
  "id": "msg-uuid",
  "text": null,
  "encrypted": true,
  "ciphertext": "base64-encoded-blob",
  "sender_identity_key": "base64",
  "session_version": 3
}
```

Server stores `ciphertext` — only recipient can decrypt on their device.

### Sender Keys (for groups)

1-on-1: Double Ratchet (separate session per pair).
Groups: Sender Keys — each participant generates Sender Key, shares via individual E2E channels. One encryption for entire group.
On member leave → all remaining regenerate Sender Keys.

### Saturn API methods (~15)

`uploadIdentityKey`, `uploadSignedPreKey`, `uploadOneTimePreKeys`, `fetchKeyBundle`, `fetchIdentityKey`, `fetchPreKeyCount`, `sendEncryptedMessage`, `fetchKeyTransparencyLog`, `verifyIdentity`, `setDisappearingTimer`, `fetchDisappearingTimer`

### Disappearing Messages

Timers: 24h / 7d / 30d / Off. Field `expires_at` in messages. Go cron deletes expired. Client also deletes locally.

### Safety Numbers

Hash of two Identity Keys → unique number per user pair. Warning shown if key changes (new device, reinstall).

### Rollout plan

1. Opt-in for DM → 2. Default for new DM → 3. Groups opt-in → 4. Default everywhere

### Impact on other features

| Feature | With E2E |
|---------|---------|
| Search | Client-side only (Meilisearch can't see plaintext) |
| Push preview | "New message" without text |
| Media | Encrypted AES-256-GCM before R2 upload |
| Multi-device | Sender encrypts for EACH recipient device |

### DB schema

```sql
CREATE TABLE user_keys (
  user_id UUID REFERENCES users(id),
  device_id TEXT NOT NULL,
  identity_key BYTEA NOT NULL,
  signed_prekey BYTEA NOT NULL,
  signed_prekey_signature BYTEA NOT NULL,
  signed_prekey_id INT NOT NULL,
  uploaded_at TIMESTAMPTZ DEFAULT NOW(),
  PRIMARY KEY (user_id, device_id)
);

CREATE TABLE one_time_prekeys (
  id SERIAL PRIMARY KEY,
  user_id UUID REFERENCES users(id),
  device_id TEXT NOT NULL,
  key_id INT NOT NULL,
  public_key BYTEA NOT NULL,
  used BOOLEAN DEFAULT false
);

CREATE TABLE key_transparency_log (
  id SERIAL PRIMARY KEY,
  user_id UUID REFERENCES users(id),
  event_type TEXT NOT NULL,
  public_key_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT NOW()
);
```

### Phase 7 result
Open DM → lock icon "End-to-end encrypted". Send → server sees only ciphertext. Safety Numbers → scan QR → "Verified". Disappearing messages → set 24h → everything gone after 24h. Server admin opens DB → sees blob, not text.

---

## Phase 8: AI, Bots, Integrations & Production Hardening

**Services:** ai (new), bots (new), integrations (new), + all services (hardening)

**Goal:** Final phase. Claude AI built-in. Bots work. MST tools connected. Everything monitored, reliable, ready for 150+ users.

### Part A: AI Integration (ai service)

| Endpoint | Purpose |
|----------|---------|
| `POST /ai/summarize` | Chat summary for period (Claude API) |
| `POST /ai/translate` | Translate N messages to language X |
| `POST /ai/reply-suggest` | 3 reply suggestions |
| `POST /ai/transcribe` | Voice → text (Whisper API) |
| `POST /ai/search` | Semantic search (embeddings) |
| `GET /ai/usage` | AI usage stats |

**Streaming:** SSE for summaries and translations.
**Rate limiting:** 20 AI requests/min/user. Free for all.

**Saturn API methods (~10):** `summarizeChat`, `translateMessages`, `suggestReply`, `transcribeVoice`, `semanticSearch`, `explainMessage`, `fetchAiUsage`

### Part B: Bot API (bots service)

| Endpoint | Purpose |
|----------|---------|
| `POST /bots` | Create bot (get token) |
| `GET /bots` | My bots |
| `PUT/DELETE /bots/:id` | Edit/delete |
| `POST /bots/:id/commands` | Set /commands |
| `POST /bots/:id/webhook` | Set webhook URL |
| `GET /bots/:id/webhook/logs` | Call logs |

**Bot API (Telegram-compatible):** `getMe`, `sendMessage`, `editMessageText`, `deleteMessage`, `answerCallbackQuery`, `setWebhook`/`deleteWebhook`, `getUpdates`, `sendPhoto`/`sendDocument`/`sendVoice`

**Saturn API methods (~10):** `fetchBotInfo`, `sendBotCommand`, `answerCallbackQuery`, `fetchInlineResults`, `sendInlineResult`, `requestBotWebView`, `closeBotWebView`, `loadAttachBot`, `toggleAttachBot`

### Part C: Integrations (integrations service)

| Integration | Purpose |
|------------|---------|
| **InsightFlow** | Conversions → message in "MST Alerts" channel |
| **Keitaro** | Postbacks → alert in chat |
| **Saturn.ac** | Deploy status → notification in #dev |
| **HR-bot** | Direct integration (move from TG to Orbit) |
| **ASA Analytics** | Campaign alerts |

**Webhook system:**

| Endpoint | Purpose |
|----------|---------|
| `POST /webhooks` | Create webhook (get URL + secret) |
| `GET /webhooks` | List webhooks |
| `PUT/DELETE /webhooks/:id` | Edit/delete |
| `GET /webhooks/:id/logs` | Call history |
| `POST /webhooks/:id/test` | Test call |

**Saturn API methods (~10):** `fetchWebhooks`, `createWebhook`, `editWebhook`, `deleteWebhook`, `fetchWebhookLogs`, `testWebhook`, `fetchIntegrations`, `toggleIntegration`

### Part D: Production Hardening

#### Infrastructure targets

| Component | Current | Target |
|-----------|---------|--------|
| Messages DB | PostgreSQL | ScyllaDB (partition: chat_id + month) |
| Cache | None | Redis 7 (online status, typing, sessions, rate limits) |
| Message queue | None | NATS JetStream (guaranteed delivery) |
| Search | None | Meilisearch (from phase 4, finalize) |
| CDN | Saturn static | Cloudflare CDN for frontend |
| Media | R2 (from phase 3) | R2 + CDN cache |

#### ScyllaDB for messages

```cql
CREATE TABLE messages (
  chat_id UUID,
  bucket INT,
  sequence_number BIGINT,
  sender_id UUID,
  content TEXT,
  encrypted BOOLEAN,
  ciphertext BLOB,
  media_ids LIST<UUID>,
  reply_to BIGINT,
  edited_at TIMESTAMP,
  deleted BOOLEAN,
  created_at TIMESTAMP,
  PRIMARY KEY ((chat_id, bucket), sequence_number)
) WITH CLUSTERING ORDER BY (sequence_number DESC);
```

#### Redis

```
online:{userId}              → TTL 5min (heartbeat)
typing:{chatId}:{userId}     → TTL 6sec
session:{token}              → user data
ratelimit:{userId}:{endpoint} → counter
```

#### NATS JetStream

```
streams:
  MESSAGES  → guaranteed message delivery
  EVENTS    → WebSocket events (typing, status, reactions)
  PUSH      → push notification queue
  WEBHOOKS  → webhook delivery queue
```

#### Monitoring

| Tool | What |
|------|------|
| **Prometheus** | RPS, latency p50/p95/p99, error rate, WS connections |
| **Grafana** | Real-time dashboards |
| **Structured logging** | JSON → Loki or stdout |
| **OpenTelemetry** | Distributed tracing |
| **Uptime ping** | External healthcheck every 30 sec |
| **Alerts** | → Orbit channel "MST Monitoring" (dogfooding!) |

#### Performance targets

| Metric | Target |
|--------|--------|
| Message delivery | p99 < 100ms |
| API response | p95 < 200ms |
| WS connections | 500 concurrent/instance |
| Media upload | > 100 MB/s aggregate |
| Search | < 50ms per query |
| Concurrent users | 150+ without degradation |

#### Security audit

- OWASP Top 10 checklist
- Dependency scan
- Rate limiting on ALL endpoints
- Input validation (XSS, SQL injection)
- CORS whitelist
- Secrets rotation
- Penetration test

### Phase 8 result
**AI:** Press sparkle → "Summarize last hour" → streaming summary. Voice → "Transcribe" → text. "Suggest reply" → 3 options.
**Bots:** HR-bot in Orbit. `/stats` → bot responds. InsightFlow webhook → "#alerts: New conversion!"
**Production:** Grafana green. 150 users online. Messages <100ms. ScyllaDB handles 1000 msg/sec. Redis caches status. Daily backups. Alerts in Orbit if anything fails.

---

## Desktop & Mobile — Parallel Track

Not a separate phase. Parallel work starting from phase 4+:

| Platform | Technology | When to start |
|----------|-----------|---------------|
| **Desktop (Mac/Win/Linux)** | Tauri 2.0 wrapping TG Web A | After phase 4 |
| **Mobile PWA** | Service Worker + manifest (already in TG Web A) | Immediately |
| **Mobile Native** | React Native (if PWA insufficient) | After phase 6, evaluate need |

---

## Features NOT in scope (Telegram-specific)

These TG Web A features are Telegram-specific and will NOT be implemented:

- **Telegram Premium / Stars / Boost** — no monetization, all free
- **Telegram Payments** — no payment processing
- **Telegram Stories** — not needed for corporate messenger
- **Telegram Passport** — identity verification
- **Telegram Ads / Sponsored messages** — no ads ever
- **Secret Chats** (TG-specific) — replaced by E2E for ALL chats (phase 7)
- **Nearby People** — not relevant
- **People Nearby / Location sharing** — evaluate post-launch

These stubs (~199 of 419) will remain as no-ops in Saturn API.

---

## Migration from current PHASES.md

| Old Phase | New Phase | Notes |
|-----------|-----------|-------|
| Old Phase 1 (Core Messaging) | **New Phase 1** | Expanded: includes reply/forward/edit (was in old Phase 6) |
| Old Phase 2 (Groups & Channels) | **New Phase 2** | Same scope |
| Old Phase 3 (Media & Files) | **New Phase 3** | Same scope |
| Old Phase 4 (Search & Notifications) | **New Phase 4** | Expanded: includes Settings (was in old Phase 6) |
| Old Phase 5 (Voice & Video Calls) | **New Phase 6** | Moved later (Rich Messaging first) |
| Old Phase 6 (Polish & UX) | **Split** | Reply/Forward/Edit → Phase 1. Stickers/Reactions → Phase 5. Settings → Phase 4. |
| Old Phase 7 (Security & E2E) | **New Phase 7** | Same scope |
| Old Phase 8 (Desktop & Mobile) | **Parallel track** | Not a blocking phase |
| Old Phase 9 (AI Integration) | **New Phase 8A** | Combined with Bots + Production |
| Old Phase 10 (Integrations) | **New Phase 8C** | Combined |
| Old Phase 11 (Production Hardening) | **New Phase 8D** | Combined |

### CLAUDE.md phase table update needed

Old (CLAUDE.md):
```
Phase 0: Concept → Phase 7: Polish + Launch
```

New:
```
Phase 1: Core Messaging → Phase 8: AI, Bots & Production
```

Both CLAUDE.md and PHASES.md should be updated to reflect this document.
