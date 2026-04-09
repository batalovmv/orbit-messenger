# Slot 10: Stub Services Audit

## Status: COMPLETED

## Scope

- Commit: `82669bd35a1568f24eff710b1dd0074342f12dff`
- Paths: `services/ai/`, `services/bots/`, `services/integrations/`
- Focus: stub readiness for production, exposed endpoints, health checks, env validation, dead routes, placeholders returning `200` instead of `501`, startup stability, security issues in placeholder code

## Checklist

- [x] Read `CLAUDE.md`
- [x] Read `PHASES.md`
- [x] Pass 1: inspect all scoped files
- [x] Pass 2: verify each candidate finding against exact code paths
- [x] Report HIGH/CRITICAL individually
- [x] Report MEDIUM only if confidence >= 0.9
- [x] Bucket LOW issues

## File Checklist

- [x] `services/ai/Dockerfile`
- [x] `services/ai/go.mod`
- [x] `services/ai/cmd/main.go`
- [x] `services/ai/internal/handler/.gitkeep`
- [x] `services/ai/internal/service/.gitkeep`
- [x] `services/bots/Dockerfile`
- [x] `services/bots/go.mod`
- [x] `services/bots/cmd/main.go`
- [x] `services/bots/internal/handler/.gitkeep`
- [x] `services/bots/internal/service/.gitkeep`
- [x] `services/integrations/Dockerfile`
- [x] `services/integrations/go.mod`
- [x] `services/integrations/cmd/main.go`
- [x] `services/integrations/internal/handler/.gitkeep`
- [x] `services/integrations/internal/service/.gitkeep`

## Pass 1 Notes

- All three scoped services are pure stubs: one `cmd/main.go`, empty `internal/{handler,service}` placeholders, no business routes, no tests.
- Each binary only registers `/health` and then starts `http.ListenAndServe` on a `PORT` env with service-specific fallback (`8085` / `8086` / `8087`).
- No placeholder route currently returns `200` for unimplemented functionality. Outside `/health`, the services rely on the default mux and therefore fall through to `404`.

## Pass 2 Verification

- `go test ./...` succeeds in `services/ai`, `services/bots`, and `services/integrations` with `?[no test files]`.
- Runtime smoke with explicit ports confirmed:
- `services/ai`: `/health` -> `200 {"status":"ok","service":"orbit-ai"}`, `/api/v1/summarize` -> `404`
- `services/bots`: `/health` -> `200 {"status":"ok","service":"orbit-bots"}`, `/api/v1/bots` -> `404`
- `services/integrations`: `/health` -> `200 {"status":"ok","service":"orbit-integrations"}`, `/api/v1/webhooks` -> `404`

## Findings

No HIGH/CRITICAL findings in scoped files after pass 2 verification.

### MEDIUM: Health checks report "ok" even when the service surface is completely unimplemented

- Confidence: 0.98
- Files:
- `services/ai/cmd/main.go:13-25`
- `services/bots/cmd/main.go:13-25`
- `services/integrations/cmd/main.go:13-25`
- Why this matters: each stub unconditionally returns `200 {"status":"ok"}` from `/health` and then starts serving, but there are no feature handlers and no readiness gate. The runtime smoke confirmed the mismatch directly: `/health` returns `200`, while the expected product routes (`/api/v1/summarize`, `/api/v1/bots`, `/api/v1/webhooks`) immediately return `404`.
- Impact: if these containers are deployed and monitored only through `/health`, Saturn/orchestration will keep them marked healthy and traffic can reach phase-incomplete stubs instead of failing closed. For pending Phase 6-8 services this is exactly the failure mode the scope asked to catch: stubs can sit in prod looking healthy while the actual surface is absent.

### MEDIUM: All three stubs use the default `net/http` server with zero timeouts

- Confidence: 0.93
- Files:
- `services/ai/cmd/main.go:19-25`
- `services/bots/cmd/main.go:19-25`
- `services/integrations/cmd/main.go:19-25`
- Why this matters: the code uses `http.HandleFunc` plus `http.ListenAndServe(":"+port, nil)`, which means the process runs behind the default `http.Server` with no `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, or `IdleTimeout`.
- Impact: if any of these ports are reachable outside a strictly private network, a slow-client/slowloris style connection flood can pin goroutines and sockets on the only open endpoint (`/health`) and degrade or stall the process. I am keeping this at MEDIUM instead of HIGH because the actual exposure model is outside this scope, but the placeholder code is not production-hardened.

## Low Bucket

- No env validation beyond `PORT`; the stubs will start successfully even if future required secrets/config for AI, bots, or integrations are completely absent.
- `internal/handler` and `internal/service` are empty `.gitkeep` placeholders, so the services do not yet follow the repo's expected `handler/service/store` structure.
- Dockerfiles expose `${PORT:-8080}` while the binaries default to `8085`, `8086`, and `8087`; this is metadata drift that can confuse operators and health/port assumptions.
- There are no service-specific tests yet; current verification is compile smoke only.

## Notes

- Verified startup/route behavior with local smoke runs on ports `18085`, `18086`, and `18087`.
