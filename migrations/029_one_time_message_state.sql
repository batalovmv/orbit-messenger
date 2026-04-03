ALTER TABLE messages
  ADD COLUMN IF NOT EXISTS is_one_time BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS viewed_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS viewed_by UUID REFERENCES users(id) ON DELETE SET NULL;

UPDATE messages AS msg
SET is_one_time = true
WHERE msg.is_one_time = false
  AND EXISTS (
    SELECT 1
    FROM message_media mm
    JOIN media m ON m.id = mm.media_id
    WHERE mm.message_id = msg.id
      AND m.is_one_time = true
  );

CREATE INDEX IF NOT EXISTS idx_messages_one_time_unviewed
  ON messages (chat_id, viewed_at)
  WHERE is_one_time = true AND is_deleted = false;
