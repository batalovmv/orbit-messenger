-- Add entities column to messages for rich text formatting (bold, italic, code, etc.)
ALTER TABLE messages ADD COLUMN IF NOT EXISTS entities JSONB;
