# Saturn backup-cron enablement (FirstVDS S3)

> Created 2026-05-05 after Saturn UI's S3 backup setting failed against
> FirstVDS — Saturn UI hardcodes AWS regional endpoints and gives no
> custom-endpoint field. Our own `backup-cron` container uses
> `aws s3 cp --endpoint-url` and works against any S3-compatible
> provider. This runbook activates it on Saturn.

---

## What the container does

- Runs `pg_dump | gzip | gpg --symmetric` every `BACKUP_CRON` tick
  (default `0 */4 * * *` — every 4h).
- Uploads `orbit-<UTC>.sql.gz.gpg` to `s3://${R2_BACKUP_BUCKET}/postgres/`
  via the AWS CLI with `--endpoint-url ${R2_ENDPOINT}`.
- Retention is enforced by a bucket-side lifecycle rule, NOT the script.

---

## Pre-flight

1. FirstVDS bucket exists, e.g. `orbit-postgres-backups`.
2. IAM key has Object Read/Write on that bucket only.
3. Generate a strong `BACKUP_ENCRYPTION_PASSPHRASE` (≥32 chars):
   ```bash
   openssl rand -base64 48
   ```
   **Store this somewhere safe outside Saturn** (1Password, etc.) —
   without it, the backups are unrecoverable.

---

## Activation

### 1. Push the `.saturn.yml` change

The `backup-cron` component is already declared in `.saturn.yml` as of
this commit. Pushing to `main` triggers Saturn to build the image but
the resource may need to be added manually first (see step 2).

### 2. Create the resource in Saturn

In Saturn dashboard for the project:

1. Architecture → **Add Service** → Custom container
2. Name: `orbit-backup-cron`
3. Source: link to this repo + Dockerfile path `deploy/backup-cron/Dockerfile`
4. Port: leave empty (no HTTP server, supercronic loop)
5. Healthcheck: disable (no port to probe)

### 3. Set env vars on `orbit-backup-cron`

| Variable | Value |
|---|---|
| `BACKUP_CRON` | `0 */4 * * *` (every 4h) — or `0 3 * * *` for daily 03:00 UTC |
| `DATABASE_URL` | Full postgres URL — copy from `orbit-messaging` or any service that has it (it's the same pool) |
| `BACKUP_ENCRYPTION_PASSPHRASE` | The 32+ char passphrase from pre-flight step 3 |
| `R2_ENDPOINT` | `https://s3.firstvds.ru` |
| `R2_ACCESS_KEY_ID` | FirstVDS S3 key |
| `R2_SECRET_ACCESS_KEY` | FirstVDS S3 secret |
| `R2_BACKUP_BUCKET` | `orbit-postgres-backups` (or your bucket name) |

### 4. Deploy + verify

1. Trigger Deploy on `orbit-backup-cron`.
2. Wait for first cron tick (or change `BACKUP_CRON` to a near-future
   minute for the smoke run, e.g. `*/5 * * * *`, then revert).
3. Saturn → orbit-backup-cron → Logs. You should see:
   ```
   [HH:MM:SS] dump+compress+encrypt start
   [HH:MM:SS] dump size: <bytes>
   [HH:MM:SS] upload to s3://orbit-postgres-backups/postgres/orbit-...
   [HH:MM:SS] upload complete: postgres/orbit-...sql.gz.gpg
   orbit_backup status=ok object=postgres/orbit-... size=... timestamp=...
   ```
4. On FirstVDS S3 console, verify the object exists and size > 1 KiB.

---

## Failure modes

| Symptom | Likely cause | Fix |
|---|---|---|
| `BACKUP_ENCRYPTION_PASSPHRASE must be at least 32 characters` | Passphrase too short | Regenerate with `openssl rand -base64 48` |
| `Could not connect to https://s3.firstvds.ru` | Wrong endpoint or DNS | Verify `R2_ENDPOINT=https://s3.firstvds.ru` (with scheme + no trailing slash) |
| `403 Forbidden` on upload | IAM key lacks Write | Recreate IAM key with Object Read+Write on the bucket |
| `dump artifact is suspiciously small` | pg_dump silently failed | Check `DATABASE_URL` correctness, network from container to postgres |
| No logs at all after schedule | Cron schedule wrong | Verify `BACKUP_CRON` is a valid 5-field crontab |

---

## Restore drill

```bash
# 1. Pull the latest object from FirstVDS S3
aws s3 cp \
  --endpoint-url https://s3.firstvds.ru \
  s3://orbit-postgres-backups/postgres/orbit-<UTC>.sql.gz.gpg \
  ./restore.sql.gz.gpg

# 2. Decrypt + decompress
gpg --batch --yes --passphrase "$BACKUP_ENCRYPTION_PASSPHRASE" \
    --output - --decrypt restore.sql.gz.gpg | gunzip > restore.sql

# 3. Apply against a SCRATCH database (never prod directly)
psql "$SCRATCH_DATABASE_URL" -f restore.sql
```

The dump uses `--clean --if-exists --no-owner --no-privileges` so it's
restorable onto a non-empty target without role-membership coupling.

---

## Open follow-ups

- Bucket-side lifecycle rule (keep 7 daily / 4 weekly / 12 monthly) —
  configure on FirstVDS S3 console; the script does not rotate.
- Prometheus freshness alert — needs a sidecar that scrapes the bucket
  listing OR a pushgateway hop from the cron. Manual log check is the
  pilot baseline.
- One full restore drill on staging within 30 days of activation, to
  prove the chain end-to-end.
