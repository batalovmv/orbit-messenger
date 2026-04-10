-- Phase 8 fixes: triggers, missing column, processing status, idempotent

-- Add updated_at column to integration_deliveries (was missing per convention)
ALTER TABLE integration_deliveries ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Add 'processing' to delivery status check (for atomic claim pattern)
ALTER TABLE integration_deliveries DROP CONSTRAINT IF EXISTS integration_deliveries_status_check;
ALTER TABLE integration_deliveries ADD CONSTRAINT integration_deliveries_status_check
    CHECK (status IN ('pending', 'processing', 'delivered', 'failed', 'dead_letter'));

-- Add updated_at triggers for all Phase 8 tables (idempotent)
DROP TRIGGER IF EXISTS set_updated_at ON bots;
CREATE TRIGGER set_updated_at BEFORE UPDATE ON bots
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

DROP TRIGGER IF EXISTS set_updated_at ON bot_installations;
CREATE TRIGGER set_updated_at BEFORE UPDATE ON bot_installations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

DROP TRIGGER IF EXISTS set_updated_at ON integration_connectors;
CREATE TRIGGER set_updated_at BEFORE UPDATE ON integration_connectors
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

DROP TRIGGER IF EXISTS set_updated_at ON integration_routes;
CREATE TRIGGER set_updated_at BEFORE UPDATE ON integration_routes
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

DROP TRIGGER IF EXISTS set_updated_at ON integration_deliveries;
CREATE TRIGGER set_updated_at BEFORE UPDATE ON integration_deliveries
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();
