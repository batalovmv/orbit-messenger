# Migrations changelog

Одна строка на миграцию. Источник правды о том, что менялось в схеме БД Orbit.

Формат: `NNN — name (YYYY-MM-DD): что сделано`. Дата = первое появление файла в git. Миграции, удаляющие фичи, помечены `КРИТИЧНО`.

Гэп `046, 047, 050, 051` — это бывшие E2E-миграции (Phase 7 Signal Protocol). Файлы удалены вместе с откатом фичи; их содержимое подчищается в `053_drop_e2e_remnants`. Не пытаться восстановить.

---

## 001 — create_extensions (2026-03-24)
Включено расширение `pgcrypto` (нужно для `gen_random_uuid()`).

## 002 — create_users (2026-03-24)
Создана таблица `users`: id, email (UNIQUE), password_hash, phone, display_name, avatar_url, bio, status (online/offline/recently), custom_status, custom_status_emoji, role (admin/member), totp_secret, totp_enabled, invited_by, invite_code, last_seen_at, created_at, updated_at. Индекс `idx_users_email`.

## 003 — create_devices (2026-03-24)
Создана таблица `devices`: id, user_id (FK→users CASCADE), device_name, device_type (web/desktop/ios/android), identity_key (BYTEA), push_token, push_type (vapid/fcm/apns), last_active_at, created_at.

## 004 — create_sessions (2026-03-24)
Создана таблица `sessions`: id, user_id (FK→users CASCADE), device_id (FK→devices SET NULL), token_hash, ip_address (INET), user_agent, expires_at, created_at. Индексы `idx_sessions_user`, `idx_sessions_token`.

## 005 — create_invites (2026-03-24)
Создана таблица `invites`: id, code (UNIQUE), created_by, email, role, max_uses, use_count, used_by, used_at, expires_at, is_active, created_at.

## 006 — create_chats (2026-03-24)
Создана таблица `chats`: id, type (direct/group/channel), name, description, avatar_url, created_by, is_protected, max_members (default 200000), created_at, updated_at. **Примечание:** канальный тип позже удалён в 035, E2E-колонки добавлялись и удалялись в 046/047/053.

## 007 — create_chat_members (2026-03-24)
Создана таблица `chat_members`: chat_id, user_id, role (owner/admin/member/readonly/banned), last_read_message_id, joined_at, muted_until, notification_level. PK (chat_id, user_id). Индекс `idx_chat_members_user`.

## 008 — create_direct_chat_lookup (2026-03-24)
Создана таблица `direct_chat_lookup` (быстрый поиск DM): user1_id, user2_id, chat_id. PK (user1_id, user2_id) с CHECK (user1_id < user2_id) для канонического порядка.

## 009 — create_messages (2026-03-24)
Создана последовательность `messages_seq` (позже удалена в 014) и таблица `messages`: id, chat_id (FK CASCADE), sender_id, type (text/photo/video/file/voice/videonote/sticker/poll/system), content, reply_to_id, is_edited, is_deleted, is_pinned, is_forwarded, forwarded_from, thread_id, sequence_number, created_at, edited_at. Индексы `idx_messages_chat_seq`, `idx_messages_chat_created`.

## 010 — create_updated_at_trigger (2026-03-24)
Функция `update_updated_at()` и триггеры на `users.updated_at`, `chats.updated_at`. Используется всеми таблицами с `updated_at`.

## 011 — fix_foreign_keys (2026-03-24)
Добавлены индексы и FK-политики: `idx_devices_user`; `invites.created_by/used_by` → SET NULL; `chat_members.last_read_message_id` → SET NULL; `direct_chat_lookup.user1_id/user2_id` → CASCADE; `messages.sender_id/forwarded_from` → SET NULL.

## 012 — add_message_entities (2026-03-24)
Добавлен `messages.entities JSONB` (rich text formatting: bold, italic, code и т.п.).

## 013 — fix_direct_chat_lookup_cascade (2026-03-24)
`direct_chat_lookup.chat_id` FK → CASCADE при удалении чата.

## 014 — per_chat_sequence (2026-03-29)
**Архитектурный сдвиг:** глобальный `messages_seq` заменён на per-chat счётчик. Добавлено `chats.next_sequence_number BIGINT`, инициализировано из `MAX(sequence_number)+1` для каждого чата. Удалён DEFAULT у `messages.sequence_number` — теперь приложение атомарно инкрементит. Удалена последовательность `messages_seq`.

## 015 — reset_admin_password (2026-03-30)
**Пустая миграция.** Сброс admin-пароля делается через `/auth/reset-admin` с `ORBIT_ADMIN_RESET_KEY` или bootstrap. Хеши паролей в git НЕ коммитятся.

## 016 — phase2_groups_channels (2026-03-30)
Phase 2: группы/каналы/permissions/invite-links. Добавлено: `chat_members.permissions BIGINT`, `chat_members.custom_title`; `chats.default_permissions` (255 — позже исправлено в 020), `chats.slow_mode_seconds`, `chats.is_signatures` (удалено в 035). Созданы `chat_invite_links` (id, chat_id, creator_id, hash UNIQUE, title, expire_at, usage_limit, usage_count, requires_approval, is_revoked) и `chat_join_requests` (chat_id, user_id, message, status pending/approved/rejected, reviewed_by). Каналы — read-only по умолчанию.

## 017 — phase3_media (2026-03-30)
Phase 3: медиа. Создана `media` (id, uploader_id, type photo/video/file/voice/videonote/gif, mime_type, original_filename, size_bytes, r2_key, thumbnail_r2_key, medium_r2_key, width, height, duration_seconds, waveform_data, is_one_time, processing_status pending/processing/ready/failed). Junction `message_media` (message_id, media_id, position, is_spoiler) для альбомов.

## 018 — security_audit_fixes (2026-03-31)
Security-аудит фиксы: UNIQUE (chat_id, sequence_number) на messages; `idx_direct_chat_lookup_user2`; `idx_chats_created_by`; `chats.created_by` → SET NULL; `chat_invite_links.creator_id` CASCADE → SET NULL; `chat_join_requests.reviewed_at`/`updated_at`+триггер, `chat_join_requests.reviewed_by` → SET NULL; UNIQUE на `media.r2_key`; FK `messages.thread_id` → SET NULL + partial-index; `devices.updated_at`+триггер; `media.updated_at`+триггер.

## 019 — media_add_updated_at (2026-03-31)
Дубликат-фикс для `media.updated_at` + триггер `set_updated_at` (на случай если 018 не доехал).

## 020 — security_audit_fixes_v2 (2026-03-31)
Сбой permissions-логики: `chats.default_permissions` default 255 (AllPermissions) → 15 (DefaultGroupPermissions); существующие группы исправлены. `chat_members.permissions` sentinel 0 → -1 (чтобы можно было явно ставить permissions=0 = "забрать всё").

## 021 — phase4_settings (2026-04-01)
Phase 4: настройки. Созданы: `privacy_settings` (last_seen/avatar/phone/calls/groups/forwarded — everyone/contacts/nobody), `blocked_users` (user_id, blocked_user_id, CHECK неравенство), `user_settings` (theme, language=ru, font_size, send_by_enter, dnd_from/until), `notification_settings` (per-chat: muted_until, sound, show_preview), `push_subscriptions` (Web Push VAPID: endpoint, p256dh, auth, user_agent).

## 022 — phase5_rich_messaging (2026-04-02)
Phase 5: реакции, стикеры, GIF, опросы, отложенные. Созданы: `message_reactions`, `chat_available_reactions` (mode all/selected/none), `sticker_packs`, `stickers` (file_type webp/tgs/webm), `user_installed_stickers`, `recent_stickers`, `saved_gifs` (Tenor), `polls` (is_anonymous, is_multiple, is_quiz, correct_option, close_at), `poll_options`, `poll_votes`, `scheduled_messages` (chat_id, sender_id, content, entities, scheduled_at, is_sent).

## 023 — phase5_default_sticker_pack (2026-04-02)
Сидинг официального пака `Orbit Basics` (id `5f52fd0a-...`) + 4 стикера (😀 🔥 🚀 ✅) inline-SVG в data URI.

## 024 — phase5_scheduled_parity (2026-04-02)
`scheduled_messages` догнали обычные: добавлены `reply_to_id`, `media_ids UUID[]`, `is_spoiler`, `poll_payload JSONB`.

## 025 — seed_sticker_packs (2026-04-03)
Расширены `sticker_packs.description`, `sticker_packs.is_featured`. `stickers.file_type` теперь принимает `svg`. Засеяно ещё несколько официальных паков (большой INSERT).

## 026 — member_preferences (2026-04-03)
Добавлены `chat_members.is_pinned`, `is_muted`, `is_archived`.

## 027 — phase5_poll_solution (2026-04-03)
Quiz-объяснение: `polls.solution`, `polls.solution_entities JSONB`.

## 028 — add_grouped_id (2026-04-03)
Добавлено `messages.grouped_id TEXT` + partial-index. Группировка media-альбомов.

## 029 — one_time_message_state (2026-04-03)
Добавлены `messages.is_one_time`, `viewed_at`, `viewed_by`. Бэкфилл: сообщения с одноразовыми media помечены `is_one_time=true`. Partial-index `idx_messages_one_time_unviewed`.

## 030 — audit_fixes (2026-04-04)
`idx_messages_sender_id`, `idx_messages_reply_to_id` (partial). CHECK длины: `polls.solution<=1024`, `sticker_packs.short_name<=64`. `chat_invite_links.updated_at`+триггер.

## 031 — global_notification_settings (2026-04-06)
В `user_settings` добавлены: `notify_users_muted/preview`, `notify_groups_muted/preview`, `notify_channels_muted/preview` (последние две позже удалены в 035).

## 032 — search_history (2026-04-06)
Создана `search_history` (id, user_id, query, scope=global, UNIQUE (user_id, query)). История поисковых запросов.

## 033 — saved_messages (2026-04-06)
Создана `saved_messages_lookup` (user_id PK → chat_id) — ссылка на self-чат «Saved Messages».

## 034 — phase6_calls (2026-04-06)
Phase 6: звонки. Созданы `calls` (type voice/video, mode p2p/group, chat_id, initiator_id, status ringing/active/ended/missed/declined, started_at, ended_at, duration_seconds) и `call_participants` (call_id, user_id, joined_at, left_at, is_muted, is_camera_off, is_screen_sharing).

## 035 — remove_channels (2026-04-07)
**КРИТИЧНО: каналы удалены. Не возрождать.** Orbit — корпоративный мессенджер, нужны только DM + groups. Удалены: все строки `chats WHERE type='channel'` (CASCADE подчищает messages/members/invites/join_requests), CHECK теперь `IN ('direct','group')`, `chats.is_signatures`, `user_settings.notify_channels_muted`, `user_settings.notify_channels_preview`, соответствующие notification_settings.

## 036 — rbac_system (2026-04-08)
RBAC + аудит. `users.role` теперь `superadmin/compliance/admin/member` (существующие admins → superadmin). Добавлено `users.is_active`, `deactivated_at`, `deactivated_by`. `invites.role` тот же CHECK. Создана **append-only** `audit_log` (actor_id, action, target_type, target_id, details JSONB, ip_address, user_agent) с триггерами `prevent_audit_mutation()` на UPDATE/DELETE — RAISE EXCEPTION. Индексы по actor/target/action/created_at.

## 037 — call_rating (2026-04-09)
В `calls` добавлены `rating INT 1..5`, `rating_comment`, `rated_by`, `rated_at`. Partial-index `idx_calls_rated`. Атомарность через `UPDATE ... WHERE rated_by IS NULL`.

## 038 — calls_unique_active (2026-04-10)
Партиал-уникальный индекс `idx_calls_chat_active_unique ON calls(chat_id) WHERE status IN ('ringing','active')` — два активных звонка в одном чате невозможны.

## 039 — poll_votes_composite_fk (2026-04-10)
Целостность голосов: удалены висящие `poll_votes`, добавлен UNIQUE (poll_id, id) на `poll_options` и композитный FK `poll_votes(poll_id, option_id) → poll_options(poll_id, id) CASCADE`. Невозможно проголосовать опцией из другого опроса.

## 040 — saved_messages_lookup_chat_unique (2026-04-10)
Дедуп `saved_messages_lookup` по chat_id (UNIQUE) — один self-чат на пользователя.

## 041 — bot_accounts (2026-04-10)
Phase 8: `users.account_type` (human/bot/system, default human), `users.username` (TEXT). UNIQUE-индекс на username (только для not-NULL), partial-index по account_type для не-человеков.

## 042 — bots (2026-04-10)
Phase 8: ядро ботов. Созданы `bots` (user_id UNIQUE→users CASCADE, owner_id, description, short_description, is_system, is_inline, webhook_url, webhook_secret_hash, is_active), `bot_tokens` (bot_id, token_hash UNIQUE, token_prefix, is_active, last_used_at), `bot_commands` (bot_id, command, description, UNIQUE bot_id+command), `bot_installations` (bot_id, chat_id, installed_by, scopes BIGINT, is_active).

## 043 — integrations (2026-04-10)
Phase 8: интеграции. Созданы `integration_connectors` (name UNIQUE, type inbound_webhook/outbound_webhook/polling, bot_id, config JSONB, secret_hash), `integration_routes` (connector_id, chat_id, event_filter, template, UNIQUE), `integration_deliveries` (status pending/delivered/failed/dead_letter — позже расширено в 048; attempt_count, max_attempts=5, next_retry_at, correlation_key, external_event_id).

## 044 — message_bot_extensions (2026-04-10)
Бот-расширения сообщений: `messages.reply_markup JSONB` (inline-клавиатуры), `messages.via_bot_id`.

## 045 — feature_flags (2026-04-10)
Создана `feature_flags` (key PK, enabled, description, metadata JSONB) + триггер. Засеян флаг `e2e_dm_enabled=false` (E2E так и не включён).

## 046 — drop (E2E remnants)
**КРИТИЧНО: миграция отсутствует — относилась к Phase 7 (Signal Protocol).** Phase 7 откачен в пользу server-side AES-256-GCM at-rest. Артефакты подчищаются в 053. Не возрождать.

## 047 — drop (E2E remnants)
**КРИТИЧНО: миграция отсутствует — относилась к Phase 7 (Signal Protocol).** См. 053. Не возрождать.

## 048 — phase8_fixes (2026-04-10)
Фиксы Phase 8: `integration_deliveries.updated_at`, статус расширен до включения `processing` (атомарный claim), `set_updated_at` триггеры на bots/bot_installations/integration_connectors/integration_routes/integration_deliveries.

## 049 — ai_usage (2026-04-15)
Создана `ai_usage` (user_id, endpoint, model, input_tokens, output_tokens, cost_cents, created_at). Учёт расхода Claude/Whisper, бэкстоп per-minute rate-limit. Индексы `(user_id, created_at DESC)`, `(endpoint, created_at DESC)`.

## 050 — drop (E2E remnants)
**КРИТИЧНО: миграция отсутствует — относилась к Phase 7 (Signal Protocol).** См. 053. Не возрождать.

## 051 — drop (E2E remnants)
**КРИТИЧНО: миграция отсутствует — относилась к Phase 7 (Signal Protocol).** См. 053. Не возрождать.

## 052 — ai_usage (2026-04-16)
Идемпотентный re-run 049 (CREATE TABLE/INDEX IF NOT EXISTS). На случай свежих окружений где 049 не применился до 053.

## 053 — drop_e2e_remnants (2026-04-16)
**КРИТИЧНО: остатки E2E удалены. Не возрождать Signal Protocol.** Phase 7 был откачен. Удалены таблицы `compliance_keys`, `key_transparency_log`, `one_time_prekeys`, `user_keys` (CASCADE). Удалены колонки `chats.is_encrypted`, `chats.disappearing_timer`, `messages.encrypted_content`, `messages.expires_at`, `media.is_encrypted` + индекс `idx_media_is_encrypted`. Все таблицы пустые (E2E не доехал в прод). Шифрование сообщений теперь — server-side AES-256-GCM в messaging-store layer.

## 054 — bot_privacy_inline_menu (2026-04-17)
BotFather-парити для `bots`: `is_privacy_enabled` (default true), `can_join_groups` (default true), `can_read_all_group_messages` (default false), `about_text` (бэкфилл из description, max 120), `inline_placeholder`, `menu_button JSONB`.

## 055 — integration_delivery_attempts (2026-04-17)
Создана `integration_delivery_attempts` (delivery_id, attempt_no, status, response_status, response_body_snippet, error, ran_at). Каждая попытка outbound-webhook = отдельная строка для админ-таймлайна (раньше хранился только last_error).

## 056 — live_translate (2026-04-21)
Live Translate Phase 2: `user_settings.default_translate_lang`, таблица-кэш `message_translations` (message_id, lang, translated_text, PK).

## 057 — smart_notifications (2026-04-22)
AI-классификация уведомлений. Таблица `notification_priority_feedback` (user_id, message_id, classified_priority, user_override_priority — все из набора urgent/important/normal/low). `users.notification_priority_mode` (smart/all/off, default smart). Таблица `chat_notification_overrides` (per-user per-chat priority override).

## 058 — translate_prefs (2026-04-23)
В `user_settings`: `can_translate BOOLEAN default false`, `can_translate_chats BOOLEAN default false`. Тогглы Show Translate Button / Translate Entire Chats теперь персистные.

## 059 — chat_drafts (2026-04-23)
В `chat_members` добавлены `draft_text`, `draft_date` — per-user per-chat черновики.

## 060 — auto_install_default_sticker_packs (2026-04-23)
Автоустановка трёх официальных стикер-паков (`5f52fd0a-...`, `10000000-...-a1`, `10000000-...-b1`) всем существующим пользователям через CROSS JOIN. Идемпотентно.

## 061 — chat_folders (2026-04-24)
Created `chat_folders` (SERIAL id, user_id, title 1..64, emoticon, color, position, UNIQUE (user_id, position) DEFERRABLE) и `chat_folder_chats` (folder_id, chat_id, is_pinned, is_excluded, added_at). Per-user папки чатов.

## 062 — bot_hr_requests (2026-04-24)
Phase 8F HR-бот шаблон: `bot_hr_requests` (bot_id, chat_id, user_id, request_type vacation/sick_leave/day_off, start_date, end_date, reason, status pending/approved/rejected, approver_id, decision_note, CHECK end_date>=start_date). Минимальный schema без иерархии менеджеров — для 150-человечной компании.

## 063 — bot_audit_log (2026-04-25)
Создана `bot_audit_log` (actor_id, bot_id, action create/update/delete/token_rotate/install/uninstall/set_webhook/delete_webhook/set_commands, details JSONB, source_ip, user_agent). Audit для bot-management операций (отдельно от `audit_log`).

## 064 — bot_approvals (2026-04-25)
Создана `bot_approval_requests` (bot_id, chat_id, requester_id, approval_type 1..64, subject 1..200, payload JSONB, status pending/approved/rejected/cancelled, version INT default 1, decided_by, decided_at, decision_note) + триггер updated_at. Универсальный approval-flow для ботов.

## 065 — bot_share_user_emails (2026-04-25)
В `bots` добавлено `share_user_emails BOOLEAN default false`. Opt-in: владелец бота явно разрешает инжект `user.email` в `Update.from`. Дефолт false для безопасности — существующие боты не меняют поведение.

## 066 — maintenance_mode_and_audit_search (2026-04-27)
В `feature_flags` добавлен сид `maintenance_mode` (enabled=false, metadata `{message,block_writes}`). Используется одновременно как kill-switch и как баннер «технические работы»: gateway middleware блокирует мутирующие запросы для не-superadmin, фронт показывает баннер. Поиск в `audit_log` остался на ILIKE (объём маленький — см. design memo) — индексы pg_trgm/tsvector отложены до фактического роста.

## 067 — drop_notification_mode_all (2026-04-27)
**КРИТИЧНО (контракт):** в `users.notification_priority_mode` удалена опция `'all'` из CHECK. Существующие строки `'all'` мигрированы в `'smart'`. Причина: gateway никогда не реализовывал отдельную ветку для `'all'` — она вела себя как `'smart'`, при этом UI рекламировал её как третий режим. Чтобы не плодить псевдо-фичу, остаются только `'smart'` (AI-классификация, killer feature) и `'off'` (выключено). Default остаётся `'smart'`.

## 068 — calls_feature_flags (2026-04-27)
В `feature_flags` добавлены два сида: `calls_group_enabled` и `calls_screen_share_enabled` (оба `false`). Используются как kill-switch для пилота — group calls и screen share имеют известные UX-пробелы (SFU init flow, track-replace toggle). P2P 1-1 voice/video не зависит от этих флагов — это baseline пилота.

## 069 — default_chats_for_new_users (2026-04-28)
Welcome flow: в `chats` добавлены `is_default_for_new_users BOOLEAN NOT NULL default false` + `default_join_order INT NOT NULL default 0` + partial index `idx_chats_default_for_new_users(default_join_order) WHERE is_default_for_new_users=true`. Авто-добавляет нового invited юзера в чаты с флагом=true (вызывается auth.Register после успешного user create через POST /internal/users/:id/join-default-chats к messaging). Admin может ручным backfill добавить уже существующих юзеров. Idempotent через ON CONFLICT (chat_id,user_id) DO NOTHING. Default OFF для всех существующих чатов — поведение не меняется до ручного флипа в AdminPanel.

## 070 — users_oidc_identity (2026-05-06)
OIDC SSO (ADR 006): в `users` добавлены `oidc_subject TEXT` и `oidc_provider TEXT` (оба nullable) + partial unique index `idx_users_oidc_identity(oidc_provider, oidc_subject) WHERE oidc_subject IS NOT NULL`. Партиальность важна — индекс не пухнет от NULL-строк password-юзеров. Один юзер может быть привязан максимум к одной OIDC-identity; password+invite users продолжают работать без изменений. Subject выставляется впервые при OIDC-callback'е через атомарный `UPDATE WHERE oidc_subject IS NULL` (см. `LinkOIDCSubject` в auth/internal/store/user_store.go) — защита от перепривязки чужой identity. Routes `/auth/oidc/{provider}/{authorize,callback}` отдают 404 пока `OIDC_PROVIDER_KEY` env не выставлен; смена поведения управляется через env, не через миграцию.
