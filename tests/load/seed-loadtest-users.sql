-- Seeds 150 deterministic test users for the k6-150-users.js load profile.
-- Password: LoadTest!2026 (bcrypt cost 10).
-- Idempotent: ON CONFLICT updates the hash so the password is always known.
--
-- Run once before k6:
--   docker exec -i orbit-postgres-1 psql -U orbit -d orbit < tests/load/seed-loadtest-users.sql

INSERT INTO users (email, password_hash, display_name, status, role, is_active, account_type)
SELECT 'loadtest_'||g||'@orbit.local',
       '$2a$10$GMDdEpu3ildz7yLOSbcviudZHQ3bX5byxlELIaUwr6WciLjWicHD6',
       'Load Test '||g,
       'offline',
       'member',
       true,
       'human'
FROM generate_series(0, 149) g
ON CONFLICT (email) DO UPDATE SET password_hash = EXCLUDED.password_hash;

SELECT count(*) AS seeded_users FROM users WHERE email LIKE 'loadtest_%@orbit.local';
