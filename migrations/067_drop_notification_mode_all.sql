-- 067_drop_notification_mode_all.sql
-- Smart Notifications: drop the 'all' mode.
--
-- The original migration 057 introduced three modes — 'smart', 'all', 'off' —
-- but the gateway notification path only ever branched on 'off' (suppress) vs
-- everything else (run the AI classifier). 'all' was effectively a synonym
-- for 'smart' on the wire, while the UI advertised it as a third distinct
-- option. That gap shipped as a contract-level bug for the pilot.
--
-- Rather than wire up a real "send everything without classifying" path
-- (extra surface, extra cost, no demonstrated user need) we collapse to
-- two modes: 'smart' (AI-classified, the killer feature) and 'off'.
--
-- Procedure: drop the existing CHECK, migrate every 'all' row to 'smart',
-- then re-add a stricter CHECK. Existing 'smart' / 'off' rows are untouched.

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_notification_priority_mode_check;

UPDATE users SET notification_priority_mode = 'smart'
    WHERE notification_priority_mode = 'all';

ALTER TABLE users ADD CONSTRAINT users_notification_priority_mode_check
    CHECK (notification_priority_mode IN ('smart', 'off'));
