# ADR 002 — No Broadcast Channels

**Status:** ACCEPTED (removed 2026-04-07, migration 035).

## Context

The original TZ included Telegram-style broadcast **channels**: one-to-many
read-only feeds with optional signatures, separate notification preferences,
and channel-specific permissions. Phase 2 implemented them in migration
`016_phase2_groups_channels`: `chats.type='channel'`, `chats.is_signatures`,
`user_settings.notify_channels_muted/preview`, channel-aware default
permissions, and matching UI.

Reality of a 150-user corporate deployment:
- No actual demand for broadcast feeds — announcements fit in pinned messages
  or admin-only groups.
- Channels duplicated group machinery (members, permissions, invites) with
  subtly different rules → permanent source of edge cases in RBAC and
  notification routing.
- Mobile/web UI carried channel-only branches (signatures, "mute channel",
  read-only banner) that never paid for themselves.

## Decision

Drop channels entirely. **Direct messages + groups cover every corporate
use case.** Announcements use admin-only groups with `permissions=0` for
non-admins (effectively read-only for members).

## Consequences

- Migration `035_remove_channels` (2026-04-07):
  - Deleted all rows where `chats.type='channel'` (CASCADE clears
    members/messages/invites/join_requests).
  - CHECK on `chats.type` reduced to `IN ('direct','group')`.
  - Dropped `chats.is_signatures`, `user_settings.notify_channels_muted`,
    `user_settings.notify_channels_preview`, channel-scoped
    `notification_settings`.
- RBAC simplified: chat-level permission bitmask (`pkg/permissions`) only
  branches on direct vs group.
- Channel-specific UI removed from `web/`; bot installation scope flags no
  longer enumerate `channel`.
- Migration is **destructive and not reversible** — channel rows are gone, not
  archived.

**Do not reintroduce a channel type.** If broadcast semantics are ever needed,
model them as a group flag rather than a third chat type.
