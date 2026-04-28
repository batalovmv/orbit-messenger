# Day 2 — Saturn ops day

> Created 2026-04-28 alongside the Day 2 deliverables PR.
>
> Day 2 is mostly hands-on QA: a real PITR drill, three browsers + an iPhone for the call matrix, manual Live Translate verification on prod, and one rollback dry-run cycle on staging. Pair this checklist with the helper script and the four upstream runbooks linked below.

Run order matters. Do **1 → 2 → 3 → 4** in sequence — the rollback cycle in step 4 disrupts staging, so park it last.

---

## 0. Pre-flight (5 min)

- [ ] Day 1 ops complete. WAL archiving on Saturn is `archive_mode=on`, `archived_count` is non-zero, and the R2 `wal-g/` prefix has fresh objects. (See [day-1-saturn-ops.md](day-1-saturn-ops.md).)
- [ ] Saturn dashboard: all 8 services green.
- [ ] Backup chain healthy: latest `pg_dump` in `s3://$R2_BACKUP_BUCKET/postgres/` is < 4 h old.
- [ ] You are running this against **staging**, not prod, for sections 1 and 4.

If WAL/PITR is not active yet, stop. PITR drill (section 1) is the first thing this checklist exercises and it depends on `archive_mode=on` plus a recent WAL-G base backup.

---

## 1. PITR restore drill on staging

Source runbook: [`pitr-restore.md`](pitr-restore.md). Section 5 of that file is the drill log — append a row when done.

We ship a small helper at `scripts/pitr-drill-marker.sh` that:

1. Inserts a sentinel row into `pitr_drill_markers` so the verification step can prove "the marker survived the round-trip" rather than relying on row-count drift inside a busy chat DB.
2. Captures pre-state (WAL LSN, archiver counters, row counts of users/messages/chats/chat_members) into a JSON file.
3. Forces `pg_switch_wal()` so the marker's WAL segment ships to R2 promptly.

### 1.1 Prepare

```bash
# On Saturn staging
./scripts/pitr-drill-marker.sh prepare \
    --container orbit-postgres-1 \
    --user orbit --db orbit \
    --out ./pitr-drill-state.json
```

Record the printed `marker_inserted_at` timestamp. Your `recovery_target_time` MUST be after this.

### 1.2 Restore per `pitr-restore.md`

Follow Section 4 of [`pitr-restore.md`](pitr-restore.md) verbatim:

- Stop staging Postgres
- Wipe `$PGDATA/*`
- `wal-g backup-fetch $PGDATA LATEST`
- `touch $PGDATA/recovery.signal`
- Append `restore_command` and `recovery_target_time` to `postgresql.conf` — set the target a few seconds AFTER the marker timestamp captured in 1.1
- Start Postgres, watch logs for "pausing at the end of recovery"
- Connect and run `SELECT pg_wal_replay_resume();`
- `SELECT pg_is_in_recovery();` must return `f`

Time the cycle. Saturn-side RTO target for the pilot is < 10 min for a single-tenant DB.

### 1.3 Verify

```bash
./scripts/pitr-drill-marker.sh verify \
    --container orbit-postgres-1 \
    --user orbit --db orbit \
    --in ./pitr-drill-state.json
```

The script asserts:
- `pg_is_in_recovery() = f` (already promoted)
- The marker row from 1.1 is present
- Row counts before/after diff is within the WAL replay window (printed for review)

Acceptance: marker row present, deltas explained by traffic between 1.1 and the recovery target.

### 1.4 Drill log

Append an entry to Section 5 of [`pitr-restore.md`](pitr-restore.md):

```markdown
### 2026-04-28

- **Operator**: <your name>
- **Staging env**: saturn.ac/staging
- **Backup used**: <backup name from `wal-g backup-list`>
- **Recovery target time**: <UTC timestamp>
- **Row counts before**: <from pitr-drill-state.json>
- **Row counts after restore**: <from `verify` output>
- **RTO observed**: ~X min (stop → promote)
- **Result**: PASS / FAIL
- **Notes**: ...
```

Commit the doc update on a `docs/pitr-drill-...` branch. **Don't put the JSON state file in git** — it is just operator scratch.

**Ack:** drill PASS, RTO logged, drill log committed.

---

## 2. Cross-browser call matrix

Source runbook: [`cross-browser-call-test.md`](cross-browser-call-test.md). 8D.5 in [`docs/8d-qa-checklist.md`](../8d-qa-checklist.md) overlaps and is more product-shaped — read both, then run the call test as the canonical procedure.

### 2.1 Coverage required by Day 2 spec

| Browser × Pair | What to verify |
|---|---|
| Chrome ↔ Firefox | P2P call (audio + video). All 7 surfaces from `cross-browser-call-test.md`. |
| Safari iOS ↔ Chrome | P2P on the same iPhone you used for Day 1 push test. Wi-Fi → LTE switch mid-call (toggle Wi-Fi off): expect ICE restart, not call drop. |
| Chrome Android ↔ Chrome desktop | Confirms the `androidTapRecovery.ts` tap path (look for `[tap-recovery] synthetic click dispatched` in console). |
| TURN fallback | On one host, block UDP outbound (firewall rule on the LAN router OR `iptables -I OUTPUT -p udp --dport 3478 -j DROP`). Restart the call. The `chrome://webrtc-internals` ICE candidate list should show only `relay` (TURN over TLS/TCP). Audio + video must still flow. |
| Push on closed app | Foreground a different app on the iPhone, lock screen. Caller dials. Banner with "Принять / Отклонить" should appear within ~5 s. Tap "Принять" → opens the call. |

### 2.2 Group-call + screen-share caveat (READ BEFORE TESTING)

The Day 2 spec asks for "Chrome+Firefox: P2P, group SFU, screen-share". Migration `068_calls_feature_flags.sql` ships **two kill-switches OFF for the pilot**:

- `calls_group_enabled` — group voice/video over SFU
- `calls_screen_share_enabled` — screen sharing toggle

Decision tree:

| You want to test | What to do |
|---|---|
| **P2P 1-1 only** | Both flags stay off. This is the default pilot scope. |
| **Group SFU + screen-share too** | Open the AdminPanel, flip both flags ON in feature-flags tab, refresh both browsers. **Flip both back OFF before exiting Day 2** so the pilot ships with the documented baseline. |

Either way, log which mode you tested in the Day 2 sign-off.

### 2.3 Per-browser failure capture

When something breaks, do not retry blindly — WebRTC state is sticky. Capture and move on:

- Browser + OS + version
- Step number from `cross-browser-call-test.md`
- DevTools console errors
- A `chrome://webrtc-internals` dump (or Firefox `about:webrtc` → "Save Page")
- WS frames panel from DevTools network tab

**Ack:** All five rows in the matrix above PASS, or a per-row failure ticket is filed with the artefacts above.

---

## 3. Live Translate prod check

Source: section 8D.4 of [`docs/8d-qa-checklist.md`](../8d-qa-checklist.md). Run on **prod** (Saturn pilot URL), not staging.

Run all four sub-sections:

- [ ] **UI strings** — open the app, open Settings, open chat list, open a chat. Strings are RU. Switch language to EN, sample the same screens, strings are EN. Switch back to RU.
- [ ] **Auto-translate in chat** — enable auto-translate (toggle in chat header or settings, depending on rollout). Have the desktop sender post a message in EN. Recipient should see the RU translation appear within ~2 s.
- [ ] **Manual translate** — long-press / right-click an EN message → "Translate". RU translation appears below the original.
- [ ] **Settings sync** — flip "Translate Entire Chats" / "Show Translate Button". `GET /api/v1/users/me/settings` must reflect the new value. `PUT` round-trip works.

Last-known regression: section 8D.4 in the QA checklist explicitly calls out the AI provider key (Anthropic) — if `ANTHROPIC_API_KEY` is missing or rate-limited, manual translate will silently fall back to a 5xx. Watch the gateway logs for `ai-translator` errors during this section.

**Ack:** UI strings, auto-translate, manual-translate, and settings round-trip all PASS on prod. No AI 5xx in gateway logs during the test.

---

## 4. Rollback dry-run on staging

Source: [`runbook-rollback.md`](../runbook-rollback.md). The Day 2 spec calls for "Take current main → revert last commit → push → Saturn redeploy → verify migrations are forward/backward compatible → time the cycle, target < 5 min".

> ⛔ **Staging only.** Do not run this against the prod branch.

### 4.1 Pre-flight migration safety check

Before doing the revert, scan migrations applied since the last rollback point. The Day 2 ack matters because **migration 067 is mildly destructive**:

> `067_drop_notification_mode_all.sql` — drops `users_notification_priority_mode_check` (CHECK constraint), rewrites every `notification_priority_mode = 'all'` row to `'smart'`, then re-adds a stricter CHECK that forbids `'all'`.

If you revert past commit `<sha-that-shipped-067>`, the older code accepts `'all'` as a valid value and may try to write it. The CHECK on the live DB will 400 those updates. Either:

- Keep the rollback target ≥ the commit that shipped 067 (preferred — limits the time-window for the dry-run), OR
- Write a compensating forward migration `069_restore_notification_mode_all.sql` that relaxes the CHECK before you revert, OR
- Just accept that auto-revert breaks on this code path and document it in the dry-run notes.

Check the recent migrations bucket fast:

```bash
ls -t migrations/*.sql | head -10
grep -lE "ALTER.*DROP|UPDATE.*WHERE" migrations/06[5-9]_*.sql migrations/07*.sql 2>/dev/null
```

### 4.2 Cycle on staging

```bash
git fetch origin master
git checkout origin/master

# Capture last-good for the report
last_good=$(git log -2 --format=%H | tail -1)
last_bad=$(git log -1 --format=%H)

git checkout -b dryrun/rollback-$(date +%Y%m%d-%H%M)
git revert --no-edit "$last_bad"

# Time it from this point
T0=$(date +%s)
git push -u origin "$(git rev-parse --abbrev-ref HEAD)"
```

> If staging is wired to auto-deploy from master only, push the revert directly to master on a staging-only fork OR use the Saturn UI's "Redeploy from a specific commit" option. **Do not push reverts to the production master in a dry-run.**

Watch the Saturn dashboard. When all 8 services are green again, capture `T1=$(date +%s)`. The cycle delta is `T1 - T0`.

### 4.3 Smoke

After staging is back green, run the abbreviated smoke (the post-deploy runbook list):

- [ ] Gateway `/health/live` and `/health/ready` return 200
- [ ] Login works, JWT issued, no auth 401 storm in logs
- [ ] Send a message in a test chat → arrives within 1 s on the other side
- [ ] WS reconnects automatically (kill the tab, reopen, observe gateway log "ws connect")
- [ ] If anything WebRTC was in scope: spot-check a 1-1 P2P call

### 4.4 Restore staging

The dry-run is a dry run — clean up:

```bash
git checkout master
git push origin --delete "$(git rev-parse --abbrev-ref HEAD@{1})"  # delete the dryrun branch
# Tell Saturn to redeploy staging from current master.
```

### 4.5 Record the timing

Append a row to `runbook-rollback.md` (section "Recent prod changes — release-specific smoke checklist") OR keep a dedicated dry-run log:

```
| Date | Operator | Bad sha | Last-good sha | Cycle (push→green) | Smoke result | Notes |
| 2026-04-28 | <name> | <bad> | <good> | X min | PASS/FAIL | ... |
```

**Ack:** Cycle < 5 min from `git push` to all-green, smoke PASS on staging, dry-run branch deleted, staging restored to master HEAD.

---

## Sign-off

Once all four sections are acked, paste the four ack lines into the pilot tracker and message the on-call channel:

> Day 2 ops complete. PITR drill RTO=<X>m, marker round-tripped. Cross-browser matrix passed (Chrome×FF, Safari iOS×Chrome, Android Chrome, TURN-only fallback, push-on-closed). Live Translate UI/auto/manual/settings all green on prod. Rollback dry-run cycle: <Y>m staging, smoke PASS, no migration-067 hazard.

Day 3 (welcome flow) is unblocked.
