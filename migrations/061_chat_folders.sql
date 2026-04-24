-- Chat folders (per-user, integer ID auto-increment)
CREATE TABLE IF NOT EXISTS chat_folders (
    id          SERIAL PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 64),
    emoticon    TEXT,
    color       INT,
    position    INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_chat_folders_user_position UNIQUE (user_id, position) DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX IF NOT EXISTS idx_chat_folders_user ON chat_folders(user_id, position);

CREATE TRIGGER trg_chat_folders_updated_at
    BEFORE UPDATE ON chat_folders
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- Chats included in or excluded from a folder
CREATE TABLE IF NOT EXISTS chat_folder_chats (
    folder_id   INT NOT NULL REFERENCES chat_folders(id) ON DELETE CASCADE,
    chat_id     UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    is_pinned   BOOLEAN NOT NULL DEFAULT FALSE,
    is_excluded BOOLEAN NOT NULL DEFAULT FALSE,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (folder_id, chat_id)
);

CREATE INDEX IF NOT EXISTS idx_chat_folder_chats_folder ON chat_folder_chats(folder_id);
