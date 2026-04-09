# Slot 19 Audit Report

## Status: COMPLETED

## Slot
- ID: 19
- Name: logging-pii
- Scope: `services/` only
- Focus: `slog`, `log`, `fmt.Println`, client-facing error paths, PII in logs, structured logging consistency, trace/request IDs, log levels, panic recovery

## References
- Commit: `82669bd35a1568f24eff710b1dd0074342f12dff`
- Instructions read: `CLAUDE.md`
- Phase context read: `PHASES.md` (current phase header + process section)

## File Checklist
- [x] Broad candidate enumeration across production `services/**/*.go` for `slog`, `log`, `fmt.Print*`, `response.Error`, `apperror`
- [x] `services/calls/cmd/main.go`
- [x] `services/messaging/cmd/main.go`
- [x] `services/media/cmd/main.go`
- [x] `services/gateway/cmd/main.go`
- [x] `services/gateway/internal/middleware/logging.go`
- [x] `services/gateway/internal/handler/proxy.go`
- [x] `services/gateway/internal/middleware/jwt.go`
- [x] `services/gateway/internal/ws/handler.go`
- [x] `services/gateway/internal/push/dispatcher.go`
- [x] `services/auth/cmd/main.go`
- [x] `services/auth/internal/service/auth_service.go`
- [x] `services/media/internal/handler/upload_handler.go`
- [x] `services/media/internal/handler/media_handler.go`
- [x] `services/media/internal/service/media_service.go`
- [x] `services/messaging/internal/service/sticker_import.go`
- [x] `services/messaging/internal/tenor/client.go`
- [x] `services/calls/internal/handler/sfu_handler.go`
- [x] `services/calls/internal/service/call_service.go`

## Findings

### High / Critical
#### HIGH: Raw PostgreSQL DSN is written to startup logs in two services
- Files:
  - `services/calls/cmd/main.go:30-31`
  - `services/messaging/cmd/main.go:34-35`
- Evidence:

```go
dbDSN, dbPassword, dbRawPassword := config.DatabaseDSN()
slog.Info("database config", "dsn", dbDSN, "password_len", len(dbPassword))
```

- Why this is a bug:
  - `config.DatabaseDSN()` feeds `pgxpool.ParseConfig(dbDSN)` immediately afterwards, and the surrounding comments explicitly describe Saturn / `DATABASE_URL` password handling. That means `dbDSN` is the real connection string, not a scrubbed display value.
  - In PostgreSQL DSNs the password is commonly embedded in the URL. Writing it to central JSON logs leaks database credentials to anyone with log access and to every downstream log sink.
  - This is a direct secret exposure, not just a style problem.

#### HIGH: Media service logs raw NATS URL, unlike gateway which already redacts it
- File:
  - `services/media/cmd/main.go:99`
- Verification context:
  - `services/media/cmd/main.go:99`

```go
slog.Info("NATS connected", "url", natsURL)
```

  - `services/gateway/cmd/main.go:35` and `services/gateway/cmd/main.go:64`

```go
slog.Info("resolved NATS URL", "url", redactURL(natsURL))
slog.Info("NATS connected", "url", redactURL(natsURL))
```

- Why this is a bug:
  - Same codebase already treats NATS URLs as sensitive enough to redact in gateway, which confirms the expected threat model.
  - NATS URLs commonly embed username/password or token material. Logging the raw value from media leaks broker credentials and internal topology into logs.
  - This is a significant secret exposure on a production startup path.

### Medium
- None verified after pass 2.

### Low Bucket
- `services/gateway/internal/middleware/logging.go:15-16` generates a request ID only on the response (`c.Set("X-Request-ID", ...)`). Proxy paths in `services/gateway/internal/handler/proxy.go:142-145`, `:226-229`, `:245-248`, `:258-261`, `:280-283` build upstream requests without forwarding that ID, so trace correlation stops at gateway.
- `services/gateway/internal/handler/proxy.go:123-147` and `:226-285` log full upstream URLs on proxy failures, including query strings. That can spill user-supplied search terms or other request parameters into logs.
- `services/gateway/internal/push/dispatcher.go:303` logs raw web-push subscription endpoints on delivery failure. Those URLs are per-device identifiers and should be treated as sensitive.
- `services/messaging/internal/tenor/client.go:177` logs raw third-party response bodies for Tenor failures. Not obviously exploitable, but it increases uncontrolled data ingress into logs.
- No panic recovery middleware found in the runtime Fiber services reviewed (`services/auth/cmd/main.go`, `services/gateway/cmd/main.go`, `services/messaging/cmd/main.go`, `services/media/cmd/main.go`, `services/calls/cmd/main.go`).
- Logging is structurally inconsistent across services: gateway request logs have `request_id`, but many other service logs omit a stable `event` field and correlation metadata altogether.

## Pass 1 Notes
- Enumerated all production Go files in `services/` hitting any of:
  - `slog`
  - `log`
  - `fmt.Print*`
  - `response.Error`
  - `apperror`
- Narrowed suspicious areas to:
  - startup/config logging in `cmd/main.go`
  - gateway request / proxy / push logging
  - auth token / logout logging
  - media and messaging external-integration logging
  - direct error-to-client paths in handlers / WS handlers

## Pass 2 Verification
- Re-read exact startup logging call sites in:
  - `services/calls/cmd/main.go`
  - `services/messaging/cmd/main.go`
  - `services/media/cmd/main.go`
- Cross-checked the NATS handling against `services/gateway/cmd/main.go`, where the same URL is explicitly redacted before logging.
- Re-read gateway logging / proxy code to confirm request ID is response-only and not propagated upstream.
- Re-read push and third-party integration logging to separate low-noise hygiene issues from real credential leaks.
