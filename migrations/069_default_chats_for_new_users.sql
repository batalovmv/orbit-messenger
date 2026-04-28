-- 069_default_chats_for_new_users.sql
-- Welcome flow (Day 3 of pilot ops sprint): new invited users auto-join
-- chats marked as `is_default_for_new_users = true`, in `default_join_order`
-- order so admins control which chat is added first (matters for the
-- chat-list ordering on first WS connect).
--
-- Both columns are NOT NULL with a non-default-flag default so the rollout
-- is safe: existing chats get is_default_for_new_users=false, no behaviour
-- change until an admin flips the flag in the chat settings UI.
--
-- The partial index keeps the planner cheap on the registration hot path —
-- the SELECT in auth_service.JoinDefaultChats only ever reads rows where
-- the flag is true, and there will be a handful of them (3-5 in practice
-- for the pilot tenant).

ALTER TABLE chats
    ADD COLUMN IF NOT EXISTS is_default_for_new_users BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE chats
    ADD COLUMN IF NOT EXISTS default_join_order INT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_chats_default_for_new_users
    ON chats (default_join_order)
    WHERE is_default_for_new_users = true;
