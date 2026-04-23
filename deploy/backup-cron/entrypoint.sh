#!/usr/bin/env bash
# Render the supercronic crontab from $BACKUP_CRON + the backup command,
# then exec supercronic so signals (SIGTERM on stop) are delivered to
# the cron loop itself rather than to this shim.
set -euo pipefail

: "${BACKUP_CRON:?BACKUP_CRON is required (e.g. '0 3 * * *')}"

CRONTAB=/tmp/orbit-backup.cron
printf '%s /usr/local/bin/backup-postgres.sh\n' "$BACKUP_CRON" > "$CRONTAB"

# -passthrough-logs keeps script stdout/stderr attached to container logs
# so Saturn / docker logs see the same output the script writes.
exec /usr/local/bin/supercronic -passthrough-logs "$CRONTAB"
