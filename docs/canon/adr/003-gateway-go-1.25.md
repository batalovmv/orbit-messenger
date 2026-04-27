# ADR 003 — Gateway on Go 1.25, All Other Services on 1.24

**Status:** ACCEPTED.

## Context

Project policy (`docs/canon/conventions.md`, `CLAUDE.md`) pins all Go services
to **1.24** — newer toolchains have repeatedly shipped subtle stdlib behaviour
changes (TLS, `net/http`, GC) that broke production before tests caught them.
The 1.24 floor gives us one toolchain to validate per quarter.

`services/gateway` is the WebSocket fan-out + presence service. Its tests
embed a real NATS server (`nats-server/v2 v2.12.7`) inside `handler_test.go`
to exercise WS-over-NATS routing without a docker dependency. Recent
`nats-server/v2` releases require Go 1.25 to compile (uses `unique` package
+ updated `runtime` APIs). Downgrading the embedded server breaks JetStream
behaviour we rely on in tests.

## Decision

`services/gateway` runs on **Go 1.25**. Every other service stays on **1.24**.
The exception is documented in `services/gateway/go.mod` header comment and
referenced from `docs/canon/divergences.md`.

## Consequences

- `services/gateway/Dockerfile` uses a 1.25 base image; all other service
  Dockerfiles use 1.24. CI matrix runs both toolchains.
- New services default to 1.24. Adding another 1.25 service requires an
  explicit ADR amendment — do not silently bump.
- Shared packages under `pkg/` must compile on 1.24 (lowest common floor).
  Do not use 1.25-only stdlib features in `pkg/*`.
- When the rest of the project eventually moves to 1.25, this ADR becomes
  moot — collapse the divergence and delete the comment in
  `services/gateway/go.mod`.
- If `nats-server/v2` ships a 1.24-compatible release (or we replace the
  embedded server with a docker fixture), revisit immediately and put
  gateway back on 1.24.
