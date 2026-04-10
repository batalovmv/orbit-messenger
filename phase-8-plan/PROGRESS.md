# Phase 8: Bots & Integrations — Progress Tracker

Started: pending
Branch: master

---

<!-- Append task completion entries below this line -->

## TASK-01: Migration 041 - Add account_type and username to users
Status: DONE
Files: migrations/041_bot_accounts.sql

## TASK-02: Migration 042 - Create bots tables
Status: DONE
Files: migrations/042_bots.sql

## TASK-03: Migration 043 - Create integrations tables
Status: DONE
Files: migrations/043_integrations.sql

## TASK-04: Migration 044 - Message bot extensions
Status: DONE
Files: migrations/044_message_bot_extensions.sql

## TASK-05: System permissions - add bot/integration management
Status: DONE
Files: pkg/permissions/system.go

## TASK-06: CHECKPOINT
Status: DONE
Files: migrations/041_bot_accounts.sql, migrations/042_bots.sql, migrations/043_integrations.sql, migrations/044_message_bot_extensions.sql, pkg/permissions/system.go
Notes: pkg go build/vet passed. Verified 4 phase-8 migration files exist and SQL was reviewed. Repo already also contains 041_feature_flags.sql and 042_e2e_keys.sql, so migration ordering now depends on lexicographic filenames.

## TASK-07: Docker-compose and env - add bots + integrations services
Status: DONE
Files: docker-compose.yml, .env.example

## TASK-08: CHECKPOINT
Status: DONE
Files: docker-compose.yml, .env.example
Notes: docker compose config --quiet passed after supplying temporary dummy env values required by existing compose interpolation. YAML structure validated; only Docker warned that the top-level version field is obsolete.
