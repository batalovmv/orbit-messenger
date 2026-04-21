#!/bin/bash
set -euo pipefail
source /etc/wal-g.env.sh
echo "[$(date)] Starting base backup..."
wal-g backup-push "$PGDATA"
echo "[$(date)] Cleaning old backups (retain 4)..."
wal-g delete retain FULL 4 --confirm
echo "[$(date)] Backup complete."
