#!/bin/bash
set -e
cat >> "$PGDATA/postgresql.conf" <<EOF
archive_mode = on
archive_command = 'source /etc/wal-g.env.sh && wal-g wal-push %p'
archive_timeout = 60
wal_level = replica
EOF
