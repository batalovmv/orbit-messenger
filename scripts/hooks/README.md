# Git hooks

Repo-managed hooks. Source of truth lives here; `.git/hooks/` is a copy.

## Install

```bash
bash scripts/install-hooks.sh
```

Re-run any time hooks change.

## What runs on `pre-commit`

1. **Migration drift** — `docs/canon/state.json.last_migration` must equal the highest `NNN_*.sql` in `migrations/`. Forces canon update when adding a migration.
2. **`@ts-ignore` / `@ts-expect-error` discipline** — new suppressions in staged TS/JS must include a `TODO(...)` marker so debt is searchable.
3. **Tracked canon files** — `AGENTS.md`, `docs/canon/state.json`, `docs/canon/README.md` must be tracked. Catches accidental `.gitignore` mishaps.

The hook does **not** run tests or builds — that gate lives in Docker (Saturn won't deploy a failing build). See `docs/canon/conventions.md` → CI.

## Bypass (don't)

`git commit --no-verify` skips the hook. Use only for genuine emergencies; the checks exist because every one of them caught a real drift in the past.
