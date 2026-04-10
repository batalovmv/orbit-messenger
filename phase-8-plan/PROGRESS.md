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

## TASK-23: Messaging client for inter-service calls
Status: DONE
Files: services/bots/internal/client/messaging_client.go

## TASK-24: Bot API auth middleware
Status: DONE
Files: services/bots/internal/botapi/middleware.go

## TASK-25: Bot API models
Status: DONE
Files: services/bots/internal/botapi/models.go, services/bots/internal/model/models.go

## TASK-26: Bot API handler - getMe, sendMessage, editMessage, deleteMessage
Status: DONE
Files: services/bots/internal/botapi/handler.go, services/bots/internal/service/bot_service.go, services/bots/internal/store/installation_store.go, services/bots/internal/store/bot_store.go

## TASK-27: Bot API callback handler
Status: DONE
Files: services/bots/internal/botapi/callback_handler.go

## TASK-28: Bot API webhook handler
Status: DONE
Files: services/bots/internal/botapi/webhook_handler.go, services/bots/internal/service/bot_service.go, services/bots/internal/store/bot_store.go
Notes: webhook secret is stored as SHA-256 digest per plan; downstream delivery will use the stored digest as the HMAC key because raw secret is not recoverable from the DB schema specified by the plan.

## TASK-29: Bot API updates handler
Status: DONE
Files: services/bots/internal/botapi/updates_handler.go

## TASK-30: CHECKPOINT
Status: DONE
Files: services/bots/cmd/main.go, services/bots/internal/client/messaging_client.go, services/bots/internal/botapi/middleware.go, services/bots/internal/botapi/models.go, services/bots/internal/botapi/handler.go, services/bots/internal/botapi/callback_handler.go, services/bots/internal/botapi/webhook_handler.go, services/bots/internal/botapi/updates_handler.go
Notes: Bot API routes are registered under /bot/:token with token middleware, and services/bots go build/vet passed.

## TASK-31: NATS subscriber for bot events
Status: DONE
Files: services/bots/internal/service/nats_subscriber.go, services/bots/internal/store/installation_store.go

## TASK-32: Webhook delivery worker
Status: DONE
Files: services/bots/internal/service/webhook_worker.go
Notes: due the plan's hashed-secret storage, webhook signing uses the persisted digest as the HMAC key during delivery.

## TASK-33: Update queue for getUpdates
Status: DONE
Files: services/bots/internal/service/update_queue.go

## TASK-34: CHECKPOINT
Status: DONE
Files: services/bots/cmd/main.go, services/bots/internal/service/nats_subscriber.go, services/bots/internal/service/webhook_worker.go, services/bots/internal/service/update_queue.go, services/bots/internal/botapi/handler.go
Notes: services/bots go build/vet passed. cmd/main.go now wires Bot API update queue, webhook worker, and bot NATS subscriptions on startup.

## TASK-35: Mock stores for bots tests
Status: DONE
Files: services/bots/internal/handler/mock_stores_test.go

## TASK-36: Bot handler tests
Status: DONE
Files: services/bots/internal/handler/bot_handler_test.go
Notes: go test ./internal/handler/... -v passed with 8 handler scenarios covering auth, permission, validation, read, and delete flows.

## TASK-37: CHECKPOINT
Status: DONE
Files: services/bots/internal/handler/mock_stores_test.go, services/bots/internal/handler/bot_handler_test.go
Notes: go test ./... -v passed for services/bots.

## TASK-38: Integrations service go.mod
Status: DONE
Files: services/integrations/go.mod, services/integrations/go.sum

## TASK-39: Integrations model package
Status: DONE
Files: services/integrations/internal/model/models.go, services/integrations/go.mod, services/integrations/go.sum

## TASK-40: Connector store
Status: DONE
Files: services/integrations/internal/store/connector_store.go

## TASK-41: Route store
Status: DONE
Files: services/integrations/internal/store/route_store.go

## TASK-42: Delivery store
Status: DONE
Files: services/integrations/internal/store/delivery_store.go

## TASK-43: CHECKPOINT
Status: DONE
Files: services/integrations/go.mod, services/integrations/go.sum, services/integrations/internal/model/models.go, services/integrations/internal/store/connector_store.go, services/integrations/internal/store/route_store.go, services/integrations/internal/store/delivery_store.go
Notes: services/integrations go build/vet passed. Local toolchain and shared pkg replacement again resolved the module to go 1.25.0 with pgx/v5 v5.9.1 instead of the plan's stated go 1.24 baseline.

## TASK-44: Integration service layer
Status: DONE
Files: services/integrations/go.mod, services/integrations/go.sum, services/integrations/internal/model/models.go, services/integrations/internal/store/connector_store.go, services/integrations/internal/client/messaging_client.go, services/integrations/internal/service/integration_service.go
Notes: go build ./internal/service/... passed. As with bots, the plan stores only a SHA-256 digest of the webhook secret, so inbound signature verification uses the persisted digest as the HMAC key because the raw secret is intentionally not recoverable.

## TASK-45: Delivery retry worker
Status: DONE
Files: services/integrations/internal/service/delivery_worker.go
Notes: go build ./internal/service/... passed. The worker retries pending/failed deliveries every 10 seconds, uses the service-layer payload envelope for re-send/edit, and moves exhausted deliveries to dead_letter.

## TASK-46: Connector management handler
Status: DONE
Files: services/integrations/go.mod, services/integrations/go.sum, services/integrations/internal/handler/connector_handler.go
Notes: go build ./internal/handler/... passed. Management endpoints are wired with pkg/response, validator-based input checks, and X-User-Role permission guards for SysManageIntegrations.

## TASK-47: Inbound webhook handler
Status: DONE
Files: services/integrations/internal/handler/webhook_handler.go
Notes: go build ./internal/handler/... passed. Public inbound webhooks now validate raw JSON bodies, apply Redis-backed 60 req/min per-connector throttling, require a fresh timestamp when a signature header is present, and pass the raw payload into the service layer for HMAC verification and routing.

## TASK-48: Delivery log handler
Status: DONE
Files: services/integrations/internal/handler/connector_handler.go, services/integrations/internal/handler/delivery_handler.go, services/integrations/internal/store/delivery_store.go
Notes: go build ./internal/handler/... passed. Delivery listing now supports server-side status filtering through a private store extension while keeping the public DeliveryStore interface from the plan intact.

## TASK-49: Integrations cmd/main.go - full wiring
Status: DONE
Files: services/integrations/go.mod, services/integrations/go.sum, services/integrations/cmd/main.go
Notes: go build ./cmd/... passed. integrations now wires pgx, Redis, NATS, messaging client, management API under /api/v1, public inbound webhooks under /api/v1/webhooks/in/:connectorId, and the background delivery retry worker.

## TASK-50: CHECKPOINT
Status: DONE
Files: services/integrations/go.mod, services/integrations/go.sum, services/integrations/cmd/main.go, services/integrations/internal/model/models.go, services/integrations/internal/store/connector_store.go, services/integrations/internal/store/delivery_store.go, services/integrations/internal/client/messaging_client.go, services/integrations/internal/service/integration_service.go, services/integrations/internal/service/delivery_worker.go, services/integrations/internal/handler/connector_handler.go, services/integrations/internal/handler/webhook_handler.go, services/integrations/internal/handler/delivery_handler.go
Notes: services/integrations go build/vet passed. Module resolution still follows the local toolchain/pkg replacement reality rather than the plan baseline: go.mod stayed on go 1.25.0 and nats/redis/pgx resolved to newer compatible versions.
