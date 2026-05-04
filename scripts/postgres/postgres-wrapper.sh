#!/usr/bin/env bash
# Wrapper around `postgres` that handles every-start hooks the official
# postgres image does NOT support natively.
#
# Background: postgres:16 only honours /docker-entrypoint-initdb.d/*
# (first-init only) and has no /docker-entrypoint.d/ equivalent. Files
# we drop in /docker-entrypoint.d/ are silently ignored — that's why
# ensure-archiving.sh and 02-start-walg-cron.sh need a host script to
# actually invoke them.
#
# Order of operations:
#   1. (if PGDATA exists) re-apply WAL archive config — covers volumes
#      that were initialised before WAL/PITR work landed
#   2. spawn supercronic in background as postgres user, scheduling
#      the daily wal-g base backup
#   3. exec the official docker-entrypoint with `postgres "$@"` so PID
#      1 stays on postgres (graceful SIGTERM, healthcheck behaviour)
#
# Idempotent end-to-end. Safe to add/remove hooks without rebuild.

set -euo pipefail

log() { echo "[postgres-wrapper] $*" >&2; }

# 1. Re-apply WAL archive config if the volume already exists. The
#    /docker-entrypoint-initdb.d/99-setup-archiving.sh ran once at
#    initdb; this runs on every restart and is idempotent.
if [[ -x /usr/local/bin/ensure-archiving.sh ]]; then
  log "running ensure-archiving"
  /usr/local/bin/ensure-archiving.sh || log "ensure-archiving exited non-zero (continuing)"
fi

# 2. Start supercronic in the background as postgres so the cron job
#    inherits the same uid that owns PGDATA. Non-fatal if supercronic
#    is missing — postgres still starts.
if [[ -x /usr/local/bin/supercronic && -x /usr/local/bin/walg-base-backup.sh ]]; then
  : "${WALG_BACKUP_CRON:=0 2 * * *}"
  CRONTAB=/etc/walg-cron.tab
  printf '%s /usr/local/bin/walg-base-backup.sh\n' "$WALG_BACKUP_CRON" > "$CRONTAB"
  chown postgres:postgres "$CRONTAB"
  su postgres -s /bin/bash -c \
    '/usr/local/bin/supercronic -passthrough-logs /etc/walg-cron.tab' &
  log "walg-cron started: schedule='${WALG_BACKUP_CRON}' pid=$!"
else
  log "walg-cron skipped (supercronic or walg-base-backup.sh missing)"
fi

# 3. Hand off to the official entrypoint so initdb-on-fresh-volume,
#    PGUSER/PGPASSWORD setup, and exec patterns all stay intact.
exec docker-entrypoint.sh "$@"
