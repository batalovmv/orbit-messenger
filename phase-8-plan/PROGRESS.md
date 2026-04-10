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

## TASK-09: Bots service go.mod
Status: DONE
Files: services/bots/go.mod, services/bots/go.sum

## TASK-10: Bots model package
Status: DONE
Files: services/bots/internal/model/models.go, services/bots/go.sum

## TASK-11: Bot store
Status: DONE
Files: services/bots/internal/store/bot_store.go, services/bots/go.mod, services/bots/go.sum

## TASK-12: Token store
Status: DONE
Files: services/bots/internal/store/token_store.go

## TASK-13: Command store
Status: DONE
Files: services/bots/internal/store/command_store.go

## TASK-14: Installation store
Status: DONE
Files: services/bots/internal/store/installation_store.go

## TASK-15: CHECKPOINT
Status: DONE
Files: services/bots/go.mod, services/bots/go.sum, services/bots/internal/model/models.go, services/bots/internal/store/bot_store.go, services/bots/internal/store/token_store.go, services/bots/internal/store/command_store.go, services/bots/internal/store/installation_store.go
Notes: go build/vet passed for services/bots. Local toolchain and local pkg module force go.mod resolution to go 1.25.0 with pgx/v5 v5.9.1; attempts to pin this module to go 1.24 and pgx/v5 v5.7.2 caused build to require go mod tidy.

## TASK-16: Bot service layer
Status: DONE
Files: services/bots/internal/service/bot_service.go

## TASK-17: Bot handler - CRUD endpoints
Status: DONE
Files: services/bots/internal/handler/bot_handler.go

## TASK-18: Token handler
Status: DONE
Files: services/bots/internal/handler/token_handler.go

## TASK-19: Command handler
Status: DONE
Files: services/bots/internal/handler/command_handler.go

## TASK-20: Installation handler
Status: DONE
Files: services/bots/internal/handler/installation_handler.go

## TASK-21: Bots cmd/main.go - full wiring
Status: DONE
Files: services/bots/cmd/main.go, services/bots/go.mod, services/bots/go.sum

## TASK-22: CHECKPOINT
Status: DONE
Files: services/bots/cmd/main.go, services/bots/go.mod, services/bots/go.sum, services/bots/internal/service/bot_service.go, services/bots/internal/handler/bot_handler.go, services/bots/internal/handler/token_handler.go, services/bots/internal/handler/command_handler.go, services/bots/internal/handler/installation_handler.go
Notes: services/bots go build/vet passed. Actual module resolution drifted from plan because local toolchain is go1.26.0 and replaced local pkg module declares go 1.25.0; go mod tidy resolved pgx/nats/redis/x/crypto to newer compatible versions.
