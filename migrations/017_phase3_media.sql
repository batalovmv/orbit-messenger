-- Phase 3: Media & Files
-- Tables: media, message_media

-- Media files metadata
CREATE TABLE media (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  uploader_id UUID NOT NULL REFERENCES users(id),
  type TEXT NOT NULL CHECK (type IN ('photo','video','file','voice','videonote','gif')),
  mime_type TEXT NOT NULL,
  original_filename TEXT,
  size_bytes BIGINT NOT NULL,
  r2_key TEXT NOT NULL,
  thumbnail_r2_key TEXT,
  medium_r2_key TEXT,
  width INT,
  height INT,
  duration_seconds FLOAT,
  waveform_data BYTEA,
  is_one_time BOOLEAN DEFAULT false,
  processing_status TEXT NOT NULL DEFAULT 'pending' CHECK (processing_status IN ('pending','processing','ready','failed')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_media_uploader ON media(uploader_id);
CREATE INDEX idx_media_created ON media(created_at DESC);

-- Junction table: messages <-> media (supports albums via position)
CREATE TABLE message_media (
  message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  media_id UUID NOT NULL REFERENCES media(id) ON DELETE CASCADE,
  position INT NOT NULL DEFAULT 0,
  is_spoiler BOOLEAN DEFAULT false,
  PRIMARY KEY (message_id, media_id)
);

CREATE INDEX idx_message_media_media ON message_media(media_id);
