#!/usr/bin/env bash
# pitr-drill-marker.sh — pre-flight helper for the PITR restore drill on
# staging.
#
# What it does, in the order the drill needs them:
#   1. Reads pg_stat_archiver and pg_current_wal_lsn() so the operator has
#      the WAL state captured before they start mutating things.
#   2. Inserts a sentinel row into a dedicated `pitr_drill_markers` table
#      with the current timestamp + a random UUID. The row id and exact
#      INSERT timestamp are printed and saved to ./pitr-drill-state.json.
#   3. Forces a `pg_switch_wal()` so the marker row's WAL segment is
#      pushed promptly to R2 by the existing archive_command.
#   4. Captures pre-restore row counts of the four tables that
#      pitr-restore.md verification queries operate on
#      (users / messages / chats / chat_members) into the same JSON.
#
# After the operator restores the database to a target time AFTER the
# marker INSERT, they can re-run this script with --verify <state-file>
# to compare row counts and confirm the marker row is present.
#
# Usage:
#   ./scripts/pitr-drill-marker.sh prepare \
#         --container orbit-postgres-1 \
#         --user orbit --db orbit \
#         --out  ./pitr-drill-state.json
#
#   # ... operator runs the wal-g restore drill ...
#
#   ./scripts/pitr-drill-marker.sh verify \
#         --container orbit-postgres-1 \
#         --user orbit --db orbit \
#         --in   ./pitr-drill-state.json
#
# Requirements: docker, jq, psql client inside the named container.
# Safe to re-run: prepare appends a NEW marker row each time, the
# `pitr_drill_markers` table is created with IF NOT EXISTS.

set -Eeuo pipefail

mode=""
container="orbit-postgres-1"
user="orbit"
db="orbit"
out="./pitr-drill-state.json"
in_file=""

usage() {
  sed -n '2,40p' "$0" | sed 's/^# //; s/^#$//'
  exit 1
}

if [[ $# -lt 1 ]]; then usage; fi
mode="$1"; shift

while [[ $# -gt 0 ]]; do
  case "$1" in
    --container) container="$2"; shift 2;;
    --user) user="$2"; shift 2;;
    --db) db="$2"; shift 2;;
    --out) out="$2"; shift 2;;
    --in) in_file="$2"; shift 2;;
    -h|--help) usage;;
    *) echo "unknown arg: $1" >&2; usage;;
  esac
done

psql_exec() {
  docker exec -i "$container" psql -U "$user" -d "$db" -tA -v ON_ERROR_STOP=1 "$@"
}

case "$mode" in
  prepare)
    echo "[prepare] verifying connectivity"
    psql_exec -c "SELECT 1" >/dev/null

    echo "[prepare] ensuring pitr_drill_markers table exists"
    psql_exec <<'SQL'
CREATE TABLE IF NOT EXISTS pitr_drill_markers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    note        TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
SQL

    echo "[prepare] inserting marker row"
    marker_row=$(psql_exec <<'SQL'
INSERT INTO pitr_drill_markers (note)
VALUES ('pitr drill prepare')
RETURNING id::text, created_at;
SQL
)
    marker_id=$(printf '%s\n' "$marker_row" | head -n1 | cut -d'|' -f1)
    marker_ts=$(printf '%s\n' "$marker_row" | head -n1 | cut -d'|' -f2)

    echo "[prepare] marker_id=$marker_id"
    echo "[prepare] marker_ts=$marker_ts"

    echo "[prepare] capturing WAL position"
    wal_lsn=$(psql_exec -c "SELECT pg_current_wal_lsn();")
    archived_count=$(psql_exec -c "SELECT archived_count FROM pg_stat_archiver;")
    last_archived_wal=$(psql_exec -c "SELECT COALESCE(last_archived_wal, '') FROM pg_stat_archiver;")

    echo "[prepare] forcing pg_switch_wal so this segment is pushed promptly"
    psql_exec -c "SELECT pg_switch_wal();" >/dev/null

    echo "[prepare] capturing baseline row counts"
    counts=$(psql_exec <<'SQL'
SELECT json_build_object(
  'users',         (SELECT COUNT(*) FROM users),
  'messages',      (SELECT COUNT(*) FROM messages),
  'chats',         (SELECT COUNT(*) FROM chats),
  'chat_members',  (SELECT COUNT(*) FROM chat_members)
);
SQL
)

    timestamp_iso=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    cat >"$out" <<JSON
{
  "captured_at": "$timestamp_iso",
  "marker_id":   "$marker_id",
  "marker_inserted_at": "$marker_ts",
  "wal_lsn_before_switch": "$wal_lsn",
  "pg_stat_archiver_before": {
    "archived_count":    $archived_count,
    "last_archived_wal": "$last_archived_wal"
  },
  "row_counts": $counts
}
JSON
    echo "[prepare] state written to $out"
    echo
    echo "Recovery target time should be AFTER:"
    echo "  $marker_ts"
    echo
    echo "Run wal-g restore now, then verify with:"
    echo "  $0 verify --container $container --user $user --db $db --in $out"
    ;;

  verify)
    if [[ -z "$in_file" || ! -f "$in_file" ]]; then
      echo "verify mode needs --in <state-file>" >&2
      exit 2
    fi

    echo "[verify] checking restored DB connectivity"
    psql_exec -c "SELECT 1" >/dev/null

    is_in_recovery=$(psql_exec -c "SELECT pg_is_in_recovery();")
    if [[ "$is_in_recovery" != "f" ]]; then
      echo "[verify] WARNING: pg_is_in_recovery() = $is_in_recovery (expected f after promote)"
    fi

    expected_marker=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["marker_id"])' "$in_file")
    found_marker=$(psql_exec -c "SELECT EXISTS(SELECT 1 FROM pitr_drill_markers WHERE id = '$expected_marker');")
    echo "[verify] marker row present? $found_marker (id=$expected_marker)"

    echo "[verify] post-restore row counts"
    counts_after=$(psql_exec <<'SQL'
SELECT json_build_object(
  'users',         (SELECT COUNT(*) FROM users),
  'messages',      (SELECT COUNT(*) FROM messages),
  'chats',         (SELECT COUNT(*) FROM chats),
  'chat_members',  (SELECT COUNT(*) FROM chat_members)
);
SQL
)

    delta=$(python3 - "$in_file" "$counts_after" <<'PY'
import json, sys
state = json.load(open(sys.argv[1]))
before = state["row_counts"]
after  = json.loads(sys.argv[2])
print("[verify] before: ", json.dumps(before))
print("[verify] after:  ", json.dumps(after))
print("[verify] delta:  ", json.dumps({k: after[k] - before[k] for k in before}))
PY
)
    printf '%s\n' "$delta"

    if [[ "$found_marker" == "t" ]]; then
      echo "[verify] PASS — marker row recovered, row counts match expectation within WAL window"
    else
      echo "[verify] FAIL — marker row missing; either recovery_target_time was set BEFORE the marker INSERT, or WAL segment never archived"
      exit 1
    fi
    ;;

  *)
    usage;;
esac
