-- Seed a minimal official sticker pack so fresh installs can use sticker UI immediately.

INSERT INTO sticker_packs (
    id,
    title,
    short_name,
    author_id,
    thumbnail_url,
    is_official,
    is_animated,
    sticker_count
)
VALUES (
    '5f52fd0a-8c59-4f3b-a9b3-6c26e3e95c51',
    'Orbit Basics',
    'orbit_basics',
    NULL,
    NULL,
    TRUE,
    FALSE,
    4
)
ON CONFLICT (id) DO UPDATE SET
    title = EXCLUDED.title,
    short_name = EXCLUDED.short_name,
    author_id = EXCLUDED.author_id,
    thumbnail_url = EXCLUDED.thumbnail_url,
    is_official = EXCLUDED.is_official,
    is_animated = EXCLUDED.is_animated,
    sticker_count = EXCLUDED.sticker_count;

INSERT INTO stickers (
    id,
    pack_id,
    emoji,
    file_url,
    file_type,
    width,
    height,
    position
)
VALUES
(
    'eaa67fd2-4bd3-4aa0-95f0-2cd7c0fc7d91',
    '5f52fd0a-8c59-4f3b-a9b3-6c26e3e95c51',
    '😀',
    'data:image/svg+xml;charset=UTF-8,%3Csvg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 512 512%22%3E%3Crect width=%22512%22 height=%22512%22 rx=%2296%22 fill=%22%23FFF7ED%22/%3E%3Ctext x=%2250%25%22 y=%2255%25%22 dominant-baseline=%22middle%22 text-anchor=%22middle%22 font-size=%22256%22%3E%F0%9F%98%80%3C/text%3E%3C/svg%3E',
    'webp',
    512,
    512,
    0
),
(
    'ff0e4c91-8fb2-4d86-ae0c-bf7dcefc44b4',
    '5f52fd0a-8c59-4f3b-a9b3-6c26e3e95c51',
    '🔥',
    'data:image/svg+xml;charset=UTF-8,%3Csvg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 512 512%22%3E%3Crect width=%22512%22 height=%22512%22 rx=%2296%22 fill=%22%23FFF1F2%22/%3E%3Ctext x=%2250%25%22 y=%2255%25%22 dominant-baseline=%22middle%22 text-anchor=%22middle%22 font-size=%22256%22%3E%F0%9F%94%A5%3C/text%3E%3C/svg%3E',
    'webp',
    512,
    512,
    1
),
(
    '0328e589-797f-453e-a8d1-d212f11edf7a',
    '5f52fd0a-8c59-4f3b-a9b3-6c26e3e95c51',
    '🚀',
    'data:image/svg+xml;charset=UTF-8,%3Csvg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 512 512%22%3E%3Crect width=%22512%22 height=%22512%22 rx=%2296%22 fill=%22%23EFF6FF%22/%3E%3Ctext x=%2250%25%22 y=%2255%25%22 dominant-baseline=%22middle%22 text-anchor=%22middle%22 font-size=%22256%22%3E%F0%9F%9A%80%3C/text%3E%3C/svg%3E',
    'webp',
    512,
    512,
    2
),
(
    '2ab6957f-d2c0-4739-a58c-5b6a5e79042d',
    '5f52fd0a-8c59-4f3b-a9b3-6c26e3e95c51',
    '✅',
    'data:image/svg+xml;charset=UTF-8,%3Csvg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 512 512%22%3E%3Crect width=%22512%22 height=%22512%22 rx=%2296%22 fill=%22%23ECFDF5%22/%3E%3Ctext x=%2250%25%22 y=%2255%25%22 dominant-baseline=%22middle%22 text-anchor=%22middle%22 font-size=%22256%22%3E%E2%9C%85%3C/text%3E%3C/svg%3E',
    'webp',
    512,
    512,
    3
)
ON CONFLICT (id) DO UPDATE SET
    pack_id = EXCLUDED.pack_id,
    emoji = EXCLUDED.emoji,
    file_url = EXCLUDED.file_url,
    file_type = EXCLUDED.file_type,
    width = EXCLUDED.width,
    height = EXCLUDED.height,
    position = EXCLUDED.position;
