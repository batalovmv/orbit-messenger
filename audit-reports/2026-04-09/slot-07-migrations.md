# Slot 07: Migrations Audit

## Status: COMPLETED

## Scope
- `migrations/`

## Focus
- Missing indexes on FK and hot query paths
- `CASCADE` rules
- `NOT NULL` constraints
- `UNIQUE` for invariants
- Idempotency, including migration `037`
- Soft-delete consistency
- Canonical DM ordering
- `TIMESTAMPTZ` everywhere
- `BIGINT` for sizes and bitmasks
- Default values matching code expectations
- Enum-like `TEXT` values matching documented constants

## Files Checklist
- [x] `migrations/001_create_extensions.sql`
- [x] `migrations/002_create_users.sql`
- [x] `migrations/003_create_devices.sql`
- [x] `migrations/004_create_sessions.sql`
- [x] `migrations/005_create_invites.sql`
- [x] `migrations/006_create_chats.sql`
- [x] `migrations/007_create_chat_members.sql`
- [x] `migrations/008_create_direct_chat_lookup.sql`
- [x] `migrations/009_create_messages.sql`
- [x] `migrations/010_create_updated_at_trigger.sql`
- [x] `migrations/011_fix_foreign_keys.sql`
- [x] `migrations/012_add_message_entities.sql`
- [x] `migrations/013_fix_direct_chat_lookup_cascade.sql`
- [x] `migrations/014_per_chat_sequence.sql`
- [x] `migrations/015_reset_admin_password.sql`
- [x] `migrations/016_phase2_groups_channels.sql`
- [x] `migrations/017_phase3_media.sql`
- [x] `migrations/018_security_audit_fixes.sql`
- [x] `migrations/019_media_add_updated_at.sql`
- [x] `migrations/020_security_audit_fixes_v2.sql`
- [x] `migrations/021_phase4_settings.sql`
- [x] `migrations/022_phase5_rich_messaging.sql`
- [x] `migrations/023_phase5_default_sticker_pack.sql`
- [x] `migrations/024_phase5_scheduled_parity.sql`
- [x] `migrations/025_seed_sticker_packs.sql`
- [x] `migrations/026_member_preferences.sql`
- [x] `migrations/027_phase5_poll_solution.sql`
- [x] `migrations/028_add_grouped_id.sql`
- [x] `migrations/029_one_time_message_state.sql`
- [x] `migrations/030_audit_fixes.sql`
- [x] `migrations/031_global_notification_settings.sql`
- [x] `migrations/032_search_history.sql`
- [x] `migrations/033_saved_messages.sql`
- [x] `migrations/034_phase6_calls.sql`
- [x] `migrations/035_remove_channels.sql`
- [x] `migrations/036_rbac_system.sql`
- [x] `migrations/037_call_rating.sql`

## Findings
- Нет подтверждённых `HIGH` / `CRITICAL` после pass 2.

## Medium

### M1. Poll vote integrity is not enforced at the schema level
- Severity: `MEDIUM`
- Confidence: `0.96`
- Files: `migrations/022_phase5_rich_messaging.sql:119-136`
- Why it matters: `poll_votes` хранит `poll_id` и `option_id`, но связывает их только отдельными FK на `polls(id)` и `poll_options(id)`. Ничто не запрещает строку, где `poll_id` относится к одному опросу, а `option_id` к другому. Это ломает подсчёт голосов и quiz-логику на уровне данных; поздних миграций, которые добавляют composite FK или хотя бы `UNIQUE (poll_id, id)` на `poll_options`, нет.
- Fix direction: добавить `UNIQUE (poll_id, id)` на `poll_options` и composite FK `(poll_id, option_id)` в `poll_votes`.

### M2. Saved messages lookup does not enforce the one-user-to-one-chat invariant
- Severity: `MEDIUM`
- Confidence: `0.92`
- Files: `migrations/033_saved_messages.sql:2-4`
- Why it matters: таблица фиксирует только `PRIMARY KEY (user_id)`. Для self-chat lookup это недостаточно: один и тот же `chat_id` может быть привязан к нескольким пользователям, потому что `UNIQUE (chat_id)` нет. При race или ошибке в create/get path это позволяет переиспользовать чужой saved-messages chat и разрушает приватный инвариант фичи. Поздних миграций, которые закрывают это ограничение, нет.
- Fix direction: добавить `UNIQUE (chat_id)` и отдельно проверить, нужен ли ещё guard на тип/состав self-chat.

### M3. Latest migration chain still violates the documented startup-idempotency requirement
- Severity: `MEDIUM`
- Confidence: `0.94`
- Files: `migrations/022_phase5_rich_messaging.sql:15-16,25,46,161`, `migrations/034_phase6_calls.sql:3,17,29,32,35`, `migrations/036_rbac_system.sql:24,36-54`, `migrations/037_call_rating.sql:5-7`
- Why it matters: в `037` явно зафиксировано, что каждый сервис запускает migrator при старте, а DDL должен быть идемпотентным. Но в более свежей части цепочки всё ещё есть bare `CREATE TABLE`, `CREATE INDEX` и `CREATE TRIGGER` без защиты от повторного выполнения. На fresh install или restore со снапшота ниже этих версий параллельный старт сервисов всё ещё может развалиться на duplicate-object ошибках до того, как все инстансы поднимутся.
- Fix direction: либо сериализовать миграции централизованным lock/advisory-lock механизмом, либо привести последние миграции к реально repeat-safe форме для concurrent startup.

## Low Bucket
- `users.status` всё ещё использует `('online', 'offline', 'recently')`, хотя в текущих конвенциях задокументированы `online / offline / away / dnd`. Аналогично `messages.type` и `scheduled_messages.type` держат `videonote` и `poll`, а не каноничные `video_note` и `gif`. Это дрейф enum-like `TEXT` значений относительно текущих констант/документации. См. `migrations/002_create_users.sql:9`, `migrations/009_create_messages.sql:7`, `migrations/022_phase5_rich_messaging.sql:148`.
- Несколько mutable-таблиц имеют `updated_at`, но без trigger, или вообще остаются без полноценного audit-friendly timestamp lifecycle: `privacy_settings`, `user_settings`, `notification_settings`, `calls`, `chat_invite_links`. См. `migrations/021_phase4_settings.sql`, `migrations/030_audit_fixes.sql:19-24`, `migrations/034_phase6_calls.sql:3-15`.
- Есть ещё незакрытые FK/hot-path index gaps, но уже не тянут на отдельный finding: как минимум `invites.created_by`, `invites.used_by`, `chat_members.last_read_message_id`, `notification_settings.chat_id`, `calls.initiator_id`, `calls.rated_by`.
- Несколько инвариантов остаются только на уровне приложения, а не БД: `reply_to_id`, `thread_id` и `last_read_message_id` не гарантируют принадлежность тому же чату; `scheduled_messages.media_ids UUID[]` не имеет референциальной целостности; `search_history` делает `UNIQUE (user_id, query)`, игнорируя `scope`.
- Есть места, где default/NULL semantics остаются расплывчатыми для кода: `chat_invite_links.updated_at` добавлен без `NOT NULL`, `call_participants.joined_at` nullable, `calls.rated_at` nullable по схеме и не связан с `rating`/`rated_by` дополнительным `CHECK`.

## Pass 2 Verification
- Перечитан весь каталог `migrations/` от `001` до `037`; чек-лист закрыт целиком.
- Для каждого кандидата в finding отдельно проверено, нет ли поздней миграции, которая его закрывает. Для `poll_votes` и `saved_messages_lookup` поздних фиксов нет.
- Идемпотентность перепроверена по хвосту цепочки (`022`, `034`, `036`, `037`) и сопоставлена с явно задокументированным требованием в `037`.
- Конвенции по enum-like `TEXT`, `TIMESTAMPTZ`, `BIGINT` и soft-delete сверены с `CLAUDE.md` и текущим `PHASES.md`.
