-- Add updated_at column to media table (required by convention for mutable tables)
ALTER TABLE media ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Apply the existing updated_at trigger (created in 010_create_updated_at_trigger.sql)
DROP TRIGGER IF EXISTS set_updated_at ON media;
CREATE TRIGGER set_updated_at
    BEFORE UPDATE ON media
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at();
