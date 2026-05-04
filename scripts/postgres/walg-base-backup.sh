#!/usr/bin/env bash
# Daily WAL-G base backup + retention prune.
#
# Runs INSIDE the postgres container as the `postgres` user so it has
# direct PGDATA access (wal-g reads files from disk and brackets the
# read with pg_start_backup() / pg_stop_backup()). A sidecar would
# either need the docker socket or stream over pg_basebackup — both
# add operational surface we don't want.
#
# Schedule lives in /etc/walg-cron.tab; supervised by supercronic
# spawned from /docker-entrypoint.d/02-start-walg-cron.sh on every
# container start. Output is redirected through stdout/stderr so
# `docker logs orbit-postgres-1` sees it.
#
# Idempotent. Safe to run on demand by the on-call:
#   docker exec -u postgres orbit-postgres-1 /usr/local/bin/walg-base-backup.sh

set -euo pipefail

# shellcheck disable=SC1091
. /etc/wal-g.env.sh

ts() { date -u +%Y-%m-%dT%H:%M:%SZ; }
log() { echo "[$(ts)] walg-base-backup: $*"; }

trap 'log "FAIL exit=$?"' ERR

# Wait for postgres to accept connections — cron may fire seconds
# after a container restart while the server is still doing recovery.
# 60s ceiling is generous; if pg_isready still fails we want to bail
# rather than block the cron tick.
for i in $(seq 1 30); do
  if pg_isready -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-postgres}" -q; then
    break
  fi
  sleep 2
  if [[ $i -eq 30 ]]; then
    log "ABORT: postgres not ready after 60s"
    exit 1
  fi
done

log "starting base backup"

# wal-g writes its own progress lines to stderr; merge them into our
# stream so a single tail captures the whole run.
wal-g backup-push "$PGDATA" 2>&1

log "pruning to retain FULL 14"
wal-g delete retain FULL 14 --confirm 2>&1 || {
  # Retention failure is non-fatal for the backup itself — the new base
  # is in R2 either way. Log loudly and continue so an over-quota R2
  # bucket doesn't block fresh backups.
  log "WARN: retention prune failed; new base is still in R2"
}

log "OK"
