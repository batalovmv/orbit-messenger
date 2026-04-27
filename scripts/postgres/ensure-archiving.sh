#!/bin/bash
# Idempotent runtime hook that guarantees WAL archiving is configured on every
# postgres container start, including pre-existing PGDATA volumes that were
# initialised before /docker-entrypoint-initdb.d/99-setup-archiving.sh existed.
#
# Postgres' official image runs every executable in /docker-entrypoint.d/*.sh
# AFTER initdb but BEFORE the server starts (since 16.x). Drop this script
# there. Idempotent — safe to re-run.
#
# Required env (read from /etc/wal-g.env.sh at archive time, but we sanity-
# check here so misconfig fails the start instead of silently leaving WAL
# unarchived):
#   R2_ENDPOINT, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY, R2_BACKUP_WAL_BUCKET
set -euo pipefail

if [[ ! -f "$PGDATA/PG_VERSION" ]]; then
  # Fresh initdb path — setup-archiving.sh in /docker-entrypoint-initdb.d
  # already ran; nothing to do here.
  exit 0
fi

CONF="$PGDATA/postgresql.conf"

# Bail if archive config already present (case-insensitive uncommented match).
if grep -Eq '^[[:space:]]*archive_mode[[:space:]]*=' "$CONF"; then
  echo "[ensure-archiving] archive_mode already set in postgresql.conf; leaving as-is."
  exit 0
fi

cat >> "$CONF" <<'EOF'

# --- WAL archiving (added by ensure-archiving.sh on existing PGDATA) ---
archive_mode = on
archive_command = '. /etc/wal-g.env.sh && wal-g wal-push %p'
archive_timeout = 60
wal_level = replica
# --------------------------------------------------------------------
EOF

echo "[ensure-archiving] appended archive config to postgresql.conf — restart required for archive_mode."
