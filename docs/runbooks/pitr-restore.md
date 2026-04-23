# PITR Restore Drill — Orbit Messenger

> **Scope**: PostgreSQL 16 point-in-time recovery on Saturn.ac **staging** using WAL-G + Cloudflare R2.  
> **Audience**: Ops on-call.  
> **Est. time**: 15–30 min depending on DB size and WAL volume.  
> **Frequency**: Run drill at least once per quarter. Log each run in [Section 5](#5-drill-log).

> ⛔ **Never run this procedure against production.** Staging only.

---

## 1. Prerequisites

Confirm all of the following before starting:

- [ ] `wal-g` binary installed and accessible (`wal-g --version` → v3.0.x)
- [ ] SSH / console access to Saturn.ac staging confirmed
- [ ] `WALG_S3_PREFIX` env var configured (e.g. `s3://orbit-backups/staging/wal-g`)
- [ ] Cloudflare R2 credentials available in environment:
  ```bash
  export AWS_ACCESS_KEY_ID=<key>
  export AWS_SECRET_ACCESS_KEY=<secret>
  export AWS_ENDPOINT_URL=https://<account>.r2.cloudflarestorage.com
  export AWS_S3_FORCE_PATH_STYLE=true
  export AWS_REGION=auto
  export WALG_S3_PREFIX=s3://<R2_BACKUP_WAL_BUCKET>/wal-g
  ```
  Or source the pre-configured env file: `source /etc/wal-g.env.sh`

  > **Note**: WAL archiving is wired in the Docker image (`scripts/postgres/`). Requires `R2_BACKUP_WAL_BUCKET`, `R2_ENDPOINT`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY` in environment. Not confirmed active on Saturn.ac production — verify R2 credentials are set.
- [ ] PostgreSQL 16 CLI available on host (`psql`, `pg_ctl`, `pg_isready`)
- [ ] Target data directory known (default: `/var/lib/postgresql/data`)
- [ ] Write access to `postgresql.conf` on staging
- [ ] Free disk space ≥ DB size × 2
- [ ] Downstream services (gateway, auth, messaging, etc.) stopped or pointed away from staging DB

---

## 2. Quick Restore Reference

Minimal steps for fast recovery (full detail in Section 3):

```bash
# 1. List available base backups
source /etc/wal-g.env.sh && wal-g backup-list

# 2. Stop PostgreSQL
pg_ctl stop -D $PGDATA

# 3. Clear data directory (keep tablespace symlinks)
rm -rf $PGDATA/*

# 4. Restore latest base backup
source /etc/wal-g.env.sh && wal-g backup-fetch $PGDATA LATEST

# 5. Create recovery signal and configure WAL restore
touch $PGDATA/recovery.signal
echo "restore_command = 'source /etc/wal-g.env.sh && wal-g wal-fetch %f %p'" >> $PGDATA/postgresql.conf
# Optional: set recovery target time
# echo "recovery_target_time = '2026-04-22 12:00:00'" >> $PGDATA/postgresql.conf

# 6. Start PostgreSQL — it will replay WAL automatically
pg_ctl start -D $PGDATA
```

---

## 3. Backup Verification (Detailed)

List available backups and confirm a recent one exists:

```bash
wal-g backup-list
```

Expected output:

```
name                          last_modified        wal_segment_backup_start
base_000000010000000000000002  2026-04-22T03:00:05Z 000000010000000000000002
base_000000010000000000000005  2026-04-23T03:00:07Z 000000010000000000000005
```

For full metadata:

```bash
wal-g backup-list DETAIL
```

**Check**:
- At least one backup dated within the last 24 hours exists
- No `ERROR` lines in the output

> If no recent backup is present — **stop here**. Investigate the backup job (`scripts/postgres/backup.sh`) before proceeding.

---

## 4. Restore Procedure

### 4.1 Stop PostgreSQL

```bash
# systemd
sudo systemctl stop postgresql

# or Docker Compose
docker compose -f /opt/orbit/docker-compose.yml stop db
```

Verify fully stopped:

```bash
sudo systemctl status postgresql   # should show "inactive (dead)"
# or
docker compose ps db               # should show "exited"
```

### 4.2 Clear the data directory

> ⚠️ This wipes current data. Double-check you are on **staging**.

```bash
sudo rm -rf /var/lib/postgresql/data/*
```

### 4.3 Fetch the backup

Restore the latest backup:

```bash
sudo -u postgres wal-g backup-fetch /var/lib/postgresql/data LATEST
```

Or restore a specific backup by name (copy name from `wal-g backup-list` output):

```bash
sudo -u postgres wal-g backup-fetch /var/lib/postgresql/data base_000000010000000000000005
```

### 4.4 Create `recovery.signal`

PostgreSQL 12+ uses a signal file to enter recovery mode:

```bash
sudo -u postgres touch /var/lib/postgresql/data/recovery.signal
```

### 4.5 Set `recovery_target_time` in `postgresql.conf`

Append the PITR block to `postgresql.conf` (or `postgresql.auto.conf`):

```bash
sudo -u postgres tee -a /var/lib/postgresql/data/postgresql.conf <<'EOF'

# --- PITR restore drill — remove after drill ---
restore_command = 'wal-g wal-fetch %f %p'
recovery_target_time = '2026-04-23 02:00:00 UTC'
recovery_target_action = 'promote'
# -----------------------------------------------
EOF
```

Replace `recovery_target_time` with the exact UTC timestamp you want to recover to.

> ⚠️ The target time must be **after** the base backup was taken and **before** the event you are recovering from.

### 4.6 Start PostgreSQL

```bash
sudo systemctl start postgresql
# or
docker compose -f /opt/orbit/docker-compose.yml start db
```

### 4.7 Monitor recovery progress

```bash
sudo journalctl -u postgresql -f
# or
docker compose logs -f db
```

Look for:

```
LOG:  starting point-in-time recovery to 2026-04-23 02:00:00+00
LOG:  restored log file "000000010000000000000003" from archive
...
LOG:  recovery stopping before commit of transaction ..., time 2026-04-23 02:00:01+00
LOG:  pausing at the end of recovery
```

The instance pauses at the target time and waits for explicit promotion.

### 4.8 Promote the instance

Connect and promote:

```bash
psql -U postgres -d orbit
```

```sql
-- Confirm we are still in recovery
SELECT pg_is_in_recovery();
-- Expected: t

-- Promote / resume
SELECT pg_wal_replay_resume();

-- Confirm promotion complete
SELECT pg_is_in_recovery();
-- Expected: f
```

### 4.9 Clean up recovery settings

Remove or comment out the PITR block added in step 4.5 from `postgresql.conf` so it does not affect future restarts:

```bash
sudo -u postgres vi /var/lib/postgresql/data/postgresql.conf
# Remove/comment the lines between "--- PITR restore drill ---" markers
```

---

## 5. Verification Queries

Run immediately after promotion. Record the numbers in the drill log.

```sql
-- Row counts across key tables
SELECT 'users'        AS table_name, COUNT(*) AS row_count FROM users
UNION ALL
SELECT 'messages',                   COUNT(*)              FROM messages
UNION ALL
SELECT 'chats',                      COUNT(*)              FROM chats
UNION ALL
SELECT 'chat_members',               COUNT(*)              FROM chat_members;
```

Additional sanity checks:

```sql
-- Most recent message — timestamp must be <= recovery_target_time
SELECT MAX(created_at) FROM messages;

-- Latest users
SELECT id, username, created_at FROM users ORDER BY created_at DESC LIMIT 5;

-- Orphaned chat_members (expected: 0)
SELECT COUNT(*) FROM chat_members cm
LEFT JOIN chats c ON c.id = cm.chat_id
WHERE c.id IS NULL;

-- pg_stat for top tables (cross-check live row counts)
SELECT schemaname, tablename, n_live_tup
FROM pg_stat_user_tables
ORDER BY n_live_tup DESC
LIMIT 10;
```

Instance ready check:

```bash
pg_isready -U orbit -d orbit
# Expected: /var/run/postgresql:5432 - accepting connections
```

---

## Rollback / Abort

If something goes wrong during the drill:

1. Stop PostgreSQL immediately: `sudo systemctl stop postgresql`
2. Restore from a different named backup (specify backup name instead of `LATEST`)
3. Or restore from a pre-drill snapshot if one was taken
4. Document the failure in the drill log with the full error output
5. Do **not** point any services at a partially-recovered staging DB

---

## 5. Drill Log

Fill in one entry per drill run and commit the updated file (`docs: update PITR drill log YYYY-MM-DD`).

---

### 2026-04-23

- **Operator**: [name]
- **Staging env**: saturn.ac/staging
- **Backup used**: [backup name from `wal-g backup-list`]
- **Recovery target time**: [timestamp, e.g. `2026-04-23 02:00:00 UTC`]
- **Row counts before**: users=X, messages=Y, chats=Z, chat_members=W
- **Row counts after restore**: users=X, messages=Y, chats=Z, chat_members=W
- **Result**: PASS / FAIL
- **Notes**: [any issues encountered]

---

<!-- Add new entries above this line, newest first -->
