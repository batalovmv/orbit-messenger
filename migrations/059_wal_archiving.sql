-- Enable WAL archiving for point-in-time recovery.
-- WAL files are pushed to R2 via wal-g (configured in wal-g.env.sh).
-- After applying, restart PostgreSQL to load new settings.
-- Verify: SHOW archive_mode; should return 'on'

ALTER SYSTEM SET archive_mode = on;
ALTER SYSTEM SET archive_command = 'source /etc/wal-g.env.sh && wal-g wal-push %p';
ALTER SYSTEM SET archive_timeout = 60;