ALTER TABLE messages
  ADD COLUMN IF NOT EXISTS grouped_id TEXT;

CREATE INDEX IF NOT EXISTS idx_messages_grouped_id ON messages(grouped_id)
  WHERE grouped_id IS NOT NULL;
