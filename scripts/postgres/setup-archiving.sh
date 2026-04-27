#!/bin/bash
# Runs at FIRST initdb only (Postgres ignores /docker-entrypoint-initdb.d on
# subsequent starts). For existing volumes the same settings are applied via
# `ensure-archiving.sh` on every container start (chained from the official
# entrypoint via docker-entrypoint.d).
set -e
cat >> "$PGDATA/postgresql.conf" <<'EOF'
archive_mode = on
# Postgres invokes archive_command via /bin/sh — use POSIX `.` not `source`.
archive_command = '. /etc/wal-g.env.sh && wal-g wal-push %p'
archive_timeout = 60
wal_level = replica
EOF
