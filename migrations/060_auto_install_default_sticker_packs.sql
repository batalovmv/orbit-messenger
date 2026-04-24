-- Auto-install the 3 official sticker packs for all existing users.
-- Idempotent: ON CONFLICT DO NOTHING skips rows that already exist.

INSERT INTO user_installed_stickers (user_id, pack_id, position)
SELECT
    u.id,
    packs.pack_id::uuid,
    packs.position
FROM users u
CROSS JOIN (
    VALUES
        ('5f52fd0a-8c59-4f3b-a9b3-6c26e3e95c51', 0),
        ('10000000-0000-4000-8000-0000000000a1', 1),
        ('10000000-0000-4000-8000-0000000000b1', 2)
) AS packs(pack_id, position)
ON CONFLICT (user_id, pack_id) DO NOTHING;
