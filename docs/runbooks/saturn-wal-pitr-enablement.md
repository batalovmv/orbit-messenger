# Saturn WAL/PITR enablement — verification + activation

> **Created 2026-04-27** after the local stack uncovered that `archive_mode=off` was the actual runtime state of postgres even though the WAL-G binary and `setup-archiving.sh` were baked into the image.
>
> The same risk exists on Saturn.ac if the `orbit-postgres` image was deployed AFTER the PGDATA volume was first initialised. Postgres only runs `/docker-entrypoint-initdb.d/*` on a fresh `initdb`. On every subsequent start the script is silently skipped and `archive_mode` reverts to its compiled default (off).
>
> This runbook describes how to (1) verify the current WAL state on Saturn without touching prod, (2) enable archiving without losing data, and (3) prove the WAL stream and base-backup pipeline end-to-end.
>
> 🔒 **Do not run any of the activation steps until the verification block has been read against prod and matches the local repro.**

---

## 1. Verification (read-only)

Run from the Saturn shell against the live `postgres` container.

```bash
# 1. Current archive state
docker exec orbit-postgres-1 psql -U orbit -d orbit \
  -c "SHOW archive_mode;" \
  -c "SHOW archive_command;" \
  -c "SHOW archive_timeout;" \
  -c "SHOW wal_level;"

# 2. Archiver telemetry — last archived WAL and any failures
docker exec orbit-postgres-1 psql -U orbit -d orbit \
  -c "SELECT * FROM pg_stat_archiver;"

# 3. Pending-restart check (tells us if a setting is queued but waiting for a bounce)
docker exec orbit-postgres-1 psql -U orbit -d orbit -c "
  SELECT name, setting, source, pending_restart
  FROM pg_settings
  WHERE name IN ('archive_mode','archive_command','archive_timeout','wal_level');"

# 4. Backup-cron container — fail-fast check for missing env
docker logs --tail 200 orbit-backup-cron-1 | grep -iE 'BACKUP_ENCRYPTION_PASSPHRASE|status=ok|error' | tail -20

# 5. Object listing in R2 — both WAL and pg_dump prefixes
aws --endpoint-url "$R2_ENDPOINT" s3 ls "s3://${R2_BACKUP_BUCKET}/postgres/" | tail
aws --endpoint-url "$R2_ENDPOINT" s3 ls "s3://${R2_BACKUP_WAL_BUCKET:-orbit-backup-wal}/wal-g/" | head
```

Decision matrix from the output:

| `archive_mode` | `pg_stat_archiver.archived_count` | Action |
|---|---|---|
| `on` | non-zero, recent timestamp | Healthy. Skip activation. Move to Section 4 (drill). |
| `on` | 0 / stale | Misconfigured archive_command or R2 creds — investigate before activation. |
| `off` | 0 (always 0 when off) | **Activate per Section 2.** This is the same drift we hit locally. |

---

## 2. Activation (writes prod state, requires brief restart)

> Estimated outage: 15-30 s for the postgres restart. WS will reconnect automatically (gateway hub).
>
> Window: pick a window when chat traffic is low. Coordinate with on-call.

### 2.1 Confirm env vars are present in the postgres container

The runtime config relies on `R2_ENDPOINT`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BACKUP_WAL_BUCKET`. They are wired in `docker-compose.yml` from `.env`. Verify they show up inside the container:

```bash
docker exec orbit-postgres-1 sh -c 'env | grep -E "^R2_" | sort'
```

If `R2_BACKUP_WAL_BUCKET` is missing, **stop**: `.env` was not updated after the 2026-04-27 backup-chain fix. Reapply the env update and `docker compose up -d postgres backup-cron` first.

### 2.2 Persist archive settings via ALTER SYSTEM

```bash
docker exec orbit-postgres-1 psql -U orbit -d orbit -c "ALTER SYSTEM SET archive_mode = 'on';"
docker exec orbit-postgres-1 psql -U orbit -d orbit -c "ALTER SYSTEM SET archive_command = '. /etc/wal-g.env.sh && wal-g wal-push %p';"
docker exec orbit-postgres-1 psql -U orbit -d orbit -c "ALTER SYSTEM SET archive_timeout = '60';"
```

Note `.` not `source` — postgres invokes `archive_command` via `/bin/sh`, not bash. `source` will silently fail under sh.

Sanity-read `postgresql.auto.conf`:

```bash
docker exec orbit-postgres-1 cat /var/lib/postgresql/data/postgresql.auto.conf
```

### 2.3 Restart postgres for `archive_mode` to take effect

`archive_mode` is a postmaster-level setting; reload is not enough.

```bash
docker restart orbit-postgres-1
```

Wait for healthy:

```bash
for i in 1 2 3 4 5 6 7 8 9 10; do
  st=$(docker inspect -f '{{.State.Health.Status}}' orbit-postgres-1)
  echo "$i: $st"; [ "$st" = healthy ] && break; sleep 3
done
```

Confirm:

```bash
docker exec orbit-postgres-1 psql -U orbit -d orbit \
  -c "SHOW archive_mode;" -c "SHOW archive_command;"
```

Expected: `on` and the `wal-g wal-push` command.

### 2.4 Force a WAL switch and verify the push

```bash
docker exec orbit-postgres-1 psql -U orbit -d orbit -c "SELECT pg_switch_wal();"
sleep 5
docker exec orbit-postgres-1 psql -U orbit -d orbit -c "
  SELECT archived_count, last_archived_wal, last_archived_time,
         failed_count, last_failed_wal, last_failed_time
  FROM pg_stat_archiver;"
```

`archived_count` must increase and `failed_count` must stay 0. If `failed_count` ticks up, look in postgres logs for the wal-g error before going further.

### 2.5 Take the first base backup

```bash
docker exec -u postgres -e PGUSER=orbit -e PGDATABASE=orbit orbit-postgres-1 \
  bash -lc '. /etc/wal-g.env.sh && wal-g backup-push "$PGDATA"'

# List
docker exec -u postgres -e PGUSER=orbit -e PGDATABASE=orbit orbit-postgres-1 \
  bash -lc '. /etc/wal-g.env.sh && wal-g backup-list'
```

You should see exactly one `base_<lsn>` entry with today's date.

---

## 3. Schedule full backups (one-time)

Add a cron entry for daily full base backups. The simplest route is a sibling supercronic container that runs `wal-g backup-push`. Until that is wired, run manually as part of the on-call checklist.

A future PR will add `deploy/walg-cron/` mirroring `deploy/backup-cron/`. Tracked in `PHASES.md` 8D.

Recommended schedule for a 150-user pilot tenant:

| Backup | Schedule | Retention |
|---|---|---|
| WAL push (continuous) | every commit + `archive_timeout=60s` cap | 14 days |
| WAL-G base backup | daily 02:00 UTC | 14 days (`wal-g delete retain FULL 14 --confirm`) |
| Encrypted `pg_dump` | every 4 h (existing supercronic container) | R2 lifecycle: 7 daily / 4 weekly / 12 monthly |

Belt-and-braces is intentional: WAL-G gives PITR; pg_dump gives a logical fallback that can be restored partial-table or onto a different major version.

---

## 4. Drill (do once after activation, then quarterly)

Follow `docs/runbooks/pitr-restore.md` against the **staging** environment, not prod. Append a row to that file's drill log. PASS criteria: marker row present in restored DB, `pg_is_in_recovery() = f` after promote, row counts match the source within the WAL replay window.

---

## 5. Failure modes the local repro surfaced

The local stack uncovered four real bugs that would have ticked silently in prod. Each is now patched in `scripts/postgres/`, `scripts/backup-postgres.sh`, `docker-compose.yml`, and `.env`:

| Symptom | Cause | Fix |
|---|---|---|
| `archive_mode = off` on a postgres container that was supposed to have WAL-G | `setup-archiving.sh` only ran on initdb; existing PGDATA volumes never got the config. | New `ensure-archiving.sh` runs at every container start via `/docker-entrypoint.d/`. |
| `pg_dump` cron failing every 4 h | `BACKUP_ENCRYPTION_PASSPHRASE` not set in `.env`; script's strict check killed every run. | Passphrase added to `.env` (≥32 chars). On Saturn, ensure the same env is present and rotated periodically. |
| `gpg: can't create directory '//.gnupg': Permission denied` | Container runs as `nobody`; default `$HOME` is `/`, unwritable. | Script now `mktemp -d` and exports `GNUPGHOME` to a per-run dir. |
| `Could not connect to http://localhost:9000/...` from the cron container | `R2_ENDPOINT=http://localhost:9000` in `.env` is for browser/host access; backup-cron talks across the docker network. | New `R2_ENDPOINT_INTERNAL` knob defaults to `http://minio:9000` for in-network services; the prod override flips it to the real R2 URL. |

When applying this runbook on Saturn, double-check each of those.
