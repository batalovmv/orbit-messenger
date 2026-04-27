#!/usr/bin/env bash
# Daily PostgreSQL backup for Orbit Messenger.
#
# Dumps the orbit database, compresses with gzip, encrypts with GPG
# (symmetric, passphrase from env), and uploads to the R2 backup bucket.
# Backups are named with an ISO-8601 UTC timestamp so lexical sort == time
# sort. Intended to run once a day via a cron container on Saturn; the
# schedule lives in deploy configuration, not here.
#
# Retention is enforced by a separate lifecycle rule on the R2 bucket
# (keep 7 daily + 4 weekly + 12 monthly). Rotating from inside this
# script would re-authenticate every run and race with in-flight uploads.
#
# Required env:
#   DATABASE_URL                 - postgres://user:pass@host:port/db
#   BACKUP_ENCRYPTION_PASSPHRASE - symmetric GPG passphrase (>=32 chars)
#   R2_ENDPOINT                  - S3-compatible endpoint URL
#   R2_ACCESS_KEY_ID             - S3 access key
#   R2_SECRET_ACCESS_KEY         - S3 secret key
#   R2_BACKUP_BUCKET             - bucket name, e.g. "orbit-backups"
#
# Optional:
#   BACKUP_PREFIX                - object key prefix, default "postgres/"
#   BACKUP_TMPDIR                - staging dir, default /tmp

set -euo pipefail

: "${DATABASE_URL:?DATABASE_URL is required}"
: "${BACKUP_ENCRYPTION_PASSPHRASE:?BACKUP_ENCRYPTION_PASSPHRASE is required}"
: "${R2_ENDPOINT:?R2_ENDPOINT is required}"
: "${R2_ACCESS_KEY_ID:?R2_ACCESS_KEY_ID is required}"
: "${R2_SECRET_ACCESS_KEY:?R2_SECRET_ACCESS_KEY is required}"
: "${R2_BACKUP_BUCKET:?R2_BACKUP_BUCKET is required}"

BACKUP_PREFIX="${BACKUP_PREFIX:-postgres/}"
TMPDIR="${BACKUP_TMPDIR:-/tmp}"

# The cron container runs as `nobody`, which has no writable home, so gpg's
# default GNUPGHOME (~/.gnupg) is unwritable. Pin it to a per-run temp dir
# inside TMPDIR so symmetric encryption can store its session state.
GNUPGHOME="$(mktemp -d "${TMPDIR}/gnupg.XXXXXX")"
chmod 700 "$GNUPGHOME"
export GNUPGHOME

# Fail fast if the passphrase is obviously too weak. A symmetric GPG
# backup is only as strong as its passphrase.
if [ "${#BACKUP_ENCRYPTION_PASSPHRASE}" -lt 32 ]; then
  echo "BACKUP_ENCRYPTION_PASSPHRASE must be at least 32 characters" >&2
  exit 1
fi

TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
WORKDIR="$(mktemp -d "${TMPDIR}/orbit-backup.XXXXXX")"
trap 'rm -rf "$WORKDIR" "$GNUPGHOME"' EXIT

DUMP_FILE="$WORKDIR/orbit-${TIMESTAMP}.sql.gz.gpg"
OBJECT_KEY="${BACKUP_PREFIX}orbit-${TIMESTAMP}.sql.gz.gpg"

log() { printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; }

log "dump+compress+encrypt start"
# Stream-pipe the whole dump through gzip and gpg so nothing plaintext
# is ever written to disk. --clean --if-exists makes the dump restorable
# onto a non-empty database; --no-owner keeps it portable across roles.
pg_dump \
  --dbname="$DATABASE_URL" \
  --format=plain \
  --clean \
  --if-exists \
  --no-owner \
  --no-privileges \
  | gzip -6 \
  | gpg --batch --yes --symmetric --cipher-algo AES256 \
        --passphrase-fd 3 \
        --output "$DUMP_FILE" \
        3<<<"$BACKUP_ENCRYPTION_PASSPHRASE"

DUMP_SIZE="$(stat -c %s "$DUMP_FILE" 2>/dev/null || stat -f %z "$DUMP_FILE")"
log "dump size: ${DUMP_SIZE} bytes"

# Sanity check — a zero-byte dump means pg_dump failed silently through
# the pipe. gzip + gpg will still produce a valid (but empty) output, so
# we must verify here, not trust the pipeline exit code alone.
if [ "${DUMP_SIZE}" -lt 1024 ]; then
  echo "dump artifact is suspiciously small (${DUMP_SIZE} bytes) — aborting" >&2
  exit 2
fi

log "upload to s3://${R2_BACKUP_BUCKET}/${OBJECT_KEY}"
# Use the AWS CLI with R2's S3 API. --endpoint-url points at R2. We
# stream from the file (not stdin) so aws cli gets content-length and
# can pick a sensible multipart strategy.
AWS_ACCESS_KEY_ID="$R2_ACCESS_KEY_ID" \
AWS_SECRET_ACCESS_KEY="$R2_SECRET_ACCESS_KEY" \
AWS_EC2_METADATA_DISABLED=true \
aws s3 cp \
  --endpoint-url "$R2_ENDPOINT" \
  --only-show-errors \
  "$DUMP_FILE" \
  "s3://${R2_BACKUP_BUCKET}/${OBJECT_KEY}"

log "upload complete: ${OBJECT_KEY}"

# Emit a final status line parseable by log scrapers / alerting.
printf 'orbit_backup status=ok object=%s size=%s timestamp=%s\n' \
  "$OBJECT_KEY" "$DUMP_SIZE" "$TIMESTAMP"
