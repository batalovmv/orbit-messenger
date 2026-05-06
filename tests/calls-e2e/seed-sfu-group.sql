-- Seeds a dedicated 3-person group chat used by the SFU 3-browser e2e test.
-- Members: test@orbit.local, user2@orbit.local, loadtest_0@orbit.local —
-- all already exist in seed-loadtest-users.sql or initial seed and share
-- password LoadTest!2026 (calls-e2e README documents the convention).
--
-- Idempotent: if the group already exists by deterministic UUID, reuses it
-- and re-inserts membership rows (ON CONFLICT keeps existing).
--
-- Run before SFU e2e:
--   docker exec -i orbit-postgres-1 psql -U orbit -d orbit < tests/calls-e2e/seed-sfu-group.sql
--
-- Why we don't reuse existing "Orbit First Run" / smoke groups:
--   They contain users whose passwords we don't know (e2e-new-user-*, pwa-local-*).
--   A fixed 3-user group with deterministic credentials makes the test
--   reproducible.

WITH ids AS (
  SELECT
    'cccccccc-3333-4444-5555-666666666666'::uuid AS chat_id,
    (SELECT id FROM users WHERE email = 'test@orbit.local')          AS u_test,
    (SELECT id FROM users WHERE email = 'user2@orbit.local')         AS u_user2,
    (SELECT id FROM users WHERE email = 'loadtest_0@orbit.local')    AS u_load0
)
INSERT INTO chats (id, type, name, created_by)
SELECT chat_id, 'group', 'SFU E2E Test Group', u_test FROM ids
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name;

INSERT INTO chat_members (chat_id, user_id, role)
SELECT chat_id, u_test, 'owner' FROM (
  SELECT 'cccccccc-3333-4444-5555-666666666666'::uuid AS chat_id,
         (SELECT id FROM users WHERE email = 'test@orbit.local') AS u_test
) s
ON CONFLICT DO NOTHING;

INSERT INTO chat_members (chat_id, user_id, role)
SELECT chat_id, u_user2, 'member' FROM (
  SELECT 'cccccccc-3333-4444-5555-666666666666'::uuid AS chat_id,
         (SELECT id FROM users WHERE email = 'user2@orbit.local') AS u_user2
) s
ON CONFLICT DO NOTHING;

INSERT INTO chat_members (chat_id, user_id, role)
SELECT chat_id, u_load0, 'member' FROM (
  SELECT 'cccccccc-3333-4444-5555-666666666666'::uuid AS chat_id,
         (SELECT id FROM users WHERE email = 'loadtest_0@orbit.local') AS u_load0
) s
ON CONFLICT DO NOTHING;

SELECT c.id, c.name, count(cm.user_id) AS members
FROM chats c LEFT JOIN chat_members cm ON cm.chat_id = c.id
WHERE c.id = 'cccccccc-3333-4444-5555-666666666666'
GROUP BY c.id, c.name;
