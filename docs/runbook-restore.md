# Orbit PostgreSQL restore runbook

Steps to restore Orbit's production database from a backup produced by
[`scripts/backup-postgres.sh`](../scripts/backup-postgres.sh). Backups run every
4 hours (RPO ≈ 4h until WAL/PITR lands) and are encrypted GPG (AES-256,
symmetric) over gzipped `pg_dump` plain SQL.

## Prerequisites

- `psql`, `gpg`, `gzip`, `aws` CLIs installed locally (or inside a recovery
  container). The same image the backup script uses is fine.
- Read access to the R2 backup bucket:
  `R2_ENDPOINT`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BACKUP_BUCKET`.
- The `BACKUP_ENCRYPTION_PASSPHRASE` used when the backup was taken. Store this
  separately from R2 credentials — anyone holding both can restore.
- A target database. For disaster recovery this is a freshly provisioned
  Postgres instance; do **not** restore onto the live `orbit` database without
  dropping it first (the dump uses `--clean --if-exists`, but this will happily
  wipe real rows).

## 1. Pick a backup

```bash
AWS_ACCESS_KEY_ID="$R2_ACCESS_KEY_ID" \
AWS_SECRET_ACCESS_KEY="$R2_SECRET_ACCESS_KEY" \
aws s3 ls --endpoint-url "$R2_ENDPOINT" \
  "s3://${R2_BACKUP_BUCKET}/postgres/" \
  | sort
```

Backups are named `orbit-YYYYMMDDTHHMMSSZ.sql.gz.gpg`. Latest is last. For
point-in-time recovery use the closest backup **before** the incident.

## 2. Download and decrypt

```bash
OBJECT="postgres/orbit-20260418T030000Z.sql.gz.gpg"

aws s3 cp --endpoint-url "$R2_ENDPOINT" \
  "s3://${R2_BACKUP_BUCKET}/${OBJECT}" \
  /tmp/orbit-restore.sql.gz.gpg

# Decrypt and decompress in one pipe so plaintext never hits disk.
gpg --batch --yes --decrypt \
    --passphrase-fd 3 \
    --output /tmp/orbit-restore.sql.gz \
    /tmp/orbit-restore.sql.gz.gpg 3<<<"$BACKUP_ENCRYPTION_PASSPHRASE"

gunzip /tmp/orbit-restore.sql.gz
```

You now have `/tmp/orbit-restore.sql`.

## 3. Apply

Against a **fresh** database (recommended):

```bash
createdb --host "$PGHOST" --port "$PGPORT" --username "$PGUSER" orbit_restored
psql --host "$PGHOST" --port "$PGPORT" --username "$PGUSER" \
     --dbname orbit_restored \
     --single-transaction \
     --file /tmp/orbit-restore.sql
```

The dump uses `--clean --if-exists`, so it will drop and recreate each object
before loading data. `--single-transaction` rolls back the whole restore on any
error rather than leaving a half-populated database.

## 4. Verify

Quick smoke checks:

```sql
SELECT COUNT(*) FROM messages;      -- should match pre-incident roughly
SELECT MAX(created_at) FROM messages; -- last message timestamp
SELECT COUNT(*) FROM users;
SELECT relname, n_live_tup
  FROM pg_stat_user_tables
  ORDER BY n_live_tup DESC LIMIT 20;
```

Sanity-check the schema version:

```sql
SELECT * FROM schema_migrations ORDER BY version DESC LIMIT 5;
```

The latest applied migration should match the one currently in
`migrations/NNN_*.sql` for the deployed release.

## 5. Cut over

- Point services at the new database (update `DATABASE_URL`).
- Expect a WebSocket reconnect wave as sessions re-authenticate. The
  [nginx resolver fix](../web/nginx.conf) and
  [REST-to-WS reconnect kick](../web/src/api/saturn/client.ts) should pick
  that up within ~15s.
- Invalidate JWTs if the incident was a credential leak: bump
  `JWT_SIGNING_KEY` in auth service env and restart. All outstanding tokens
  become invalid on the next validation round-trip.

## 6. Clean up

```bash
shred -u /tmp/orbit-restore.sql /tmp/orbit-restore.sql.gz /tmp/orbit-restore.sql.gz.gpg
```

Plaintext SQL on disk is a data-breach exposure if the recovery host is
compromised. `shred -u` overwrites the file before unlinking.

## Testing this procedure

The restore itself is the only part of backup strategy that *proves* backups
are usable. Run through steps 1-4 against a throwaway database at least
monthly. If step 4 ever surfaces a row-count gap or a missing table, treat
it as a P1 incident — the backup is not actually working.
