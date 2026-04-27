# Orbit production rollback runbook

What to do when a deploy breaks prod. Saturn auto-deploys on `git push origin
main` (blue-green), so rollback = push a fix commit → Saturn redeploys.

## Decision: rollback vs hotfix forward

| Signal | Action |
|--------|--------|
| UI broken, users can't send messages, login fails | **Rollback now** |
| Regression persists > 5 min, no obvious one-liner fix | **Rollback now** |
| Isolated bug, fix is < 10 min and < 20 lines | **Hotfix forward** |
| Error rate spiked but stabilising on its own | Monitor 5 min, then decide |

Rule of thumb: if you need to *think* about the fix, rollback first, think
later.

## Rollback procedure

### 1. Identify the last good commit

```bash
# Show recent deploys (last 10 commits on main)
git log --oneline -10 main

# Find the SHA of the last known-good state.
# Look at CI status — green checkmark = safe to revert to.
git log --oneline --format="%h %s" -10 main
```

Pick `<good-sha>` — the commit **before** the broken one.

### 2. Revert the bad commit(s)

**Always use `git revert`** — force-push to `main` is forbidden (see
AGENTS.md). Revert creates a new commit that undoes the bad change while
preserving history.

```bash
# Single bad commit
git revert --no-edit <bad-sha>
git push origin main

# Multiple sequential bad commits (oldest first)
git revert --no-edit <oldest-bad-sha>^..<newest-bad-sha>
git push origin main
```

If revert has merge conflicts — resolve them, then:

```bash
git add .
git revert --continue
git push origin main
```

### 3. Wait for Saturn redeploy

- Open Saturn dashboard → check that the new commit triggered a build
- Build typically takes 1–3 min per service
- Watch for green status on all 8 services

```bash
# Quick healthcheck after redeploy
curl -sf <GATEWAY_URL>/health/live   # TODO: replace with prod gateway URL
curl -sf <GATEWAY_URL>/health/ready
```

### 4. Smoke-check

Run the post-deploy checklist from [runbook-post-deploy.md](runbook-post-deploy.md).
At minimum:

- [ ] Gateway health returns 200
- [ ] Login works
- [ ] Send a message in test chat → arrives
- [ ] WebSocket connects

## Database migration rollback

Orbit migrations are **forward-only** (`migrations/NNN_*.sql`). There is no
built-in down migration.

### If the bad deploy included a migration

**Before doing anything**, answer these questions:

| Question | If YES |
|----------|--------|
| Did the migration only ADD columns/tables? | Safe to revert code and leave migration applied — new columns will be unused |
| Did the migration DROP or ALTER existing columns? | **Dangerous** — reverting code may break because it expects old schema |
| Did the migration corrupt or delete user data? | Escalate immediately, consider DB restore |

### Options (ordered by risk, lowest first)

1. **Leave migration, revert code only** — works when migration was additive
   (new tables, new nullable columns). The unused schema objects cause no harm.

2. **Write a compensating migration** — create `migrations/NNN+1_rollback_NNN.sql`
   that undoes the schema change. Deploy it as a normal forward migration.
   ```bash
   # Example: migration 056 added a column, we want to drop it
   # Create migrations/057_rollback_056.sql with:
   # ALTER TABLE foo DROP COLUMN IF EXISTS bar;
   ```

3. **Restore from backup** — nuclear option. Use
   [runbook-restore.md](runbook-restore.md). RPO ≈ 4 hours. Only when data is
   corrupted or options 1–2 are not viable.

### Pre-rollback checklist for migrations

- [ ] Confirmed which migration(s) the bad deploy applied (`SELECT * FROM schema_migrations ORDER BY version DESC LIMIT 5;`)
- [ ] Checked whether migration was additive or destructive
- [ ] If destructive: confirmed backup exists and is recent enough
- [ ] Notified team in Slack before proceeding

## Timeline

| Step | Time |
|------|------|
| Identify bad commit | ~1 min |
| `git revert` + push | ~2 min |
| Saturn redeploy | ~2–3 min |
| Smoke checks | ~2 min |
| **Total** | **~7–8 min** |

## Escalation

Escalate (wake up a second person) when:

- Migration dropped/altered columns and code revert alone won't fix it
- Data loss suspected — any row deletes, truncates in the bad deploy
- Auth service (`services/auth`) won't start after revert — all users locked out
- Revert itself causes merge conflicts you can't resolve in < 5 min
- You're unsure — **escalating too early is always better than too late**

Escalation channel: `#orbit-incidents` in Slack, tag `@orbit-oncall`.
