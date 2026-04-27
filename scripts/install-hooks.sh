#!/bin/bash
# Install repo-managed git hooks.
# Usage: bash scripts/install-hooks.sh
set -e

REPO_ROOT="$(git rev-parse --show-toplevel)"
HOOK_DIR="$REPO_ROOT/.git/hooks"
SRC_DIR="$REPO_ROOT/scripts/hooks"

mkdir -p "$HOOK_DIR"

for hook in pre-commit; do
  src="$SRC_DIR/$hook"
  dst="$HOOK_DIR/$hook"
  [ -f "$src" ] || { echo "missing $src"; exit 1; }
  cp "$src" "$dst"
  chmod +x "$dst"
  echo "installed: $dst"
done

echo "Done. Drift checks will run on every commit."
