-- Live Translate Phase 2: user language preference + translation cache

ALTER TABLE user_settings ADD COLUMN IF NOT EXISTS default_translate_lang TEXT NULL;

CREATE TABLE IF NOT EXISTS message_translations (
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    lang TEXT NOT NULL,
    translated_text TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (message_id, lang)
);

CREATE INDEX IF NOT EXISTS idx_message_translations_message ON message_translations(message_id);