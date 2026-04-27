-- 068_calls_feature_flags.sql
-- Seed two pilot kill-switches for calls features that are not part of the
-- pilot scope. Both default OFF; toggle on from the AdminPanel after the
-- corresponding gaps land:
--
--   calls_group_enabled         — group voice/video calls (SFU init UX gaps)
--   calls_screen_share_enabled  — screen share toggle (track-replace UX gaps)
--
-- P2P 1-1 voice/video calls remain ungated — they are the pilot baseline.

INSERT INTO feature_flags (key, enabled, description, metadata)
VALUES (
    'calls_group_enabled',
    false,
    'Enable group voice/video calls (SFU). Off for pilot — group init UX has gaps.',
    '{}'::jsonb
)
ON CONFLICT (key) DO NOTHING;

INSERT INTO feature_flags (key, enabled, description, metadata)
VALUES (
    'calls_screen_share_enabled',
    false,
    'Enable screen sharing in calls. Off for pilot — toggle UI not fully wired.',
    '{}'::jsonb
)
ON CONFLICT (key) DO NOTHING;
