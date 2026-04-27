# Universal Preamble for Codex Tasks

Copy-paste this block at the TOP of every Codex task prompt before the task-specific instructions.

---

## SYSTEM INSTRUCTIONS

You are working on **Orbit Messenger** — a corporate messenger (Telegram replacement) built as a monorepo. The frontend is a GPL-3.0 fork of Telegram Web A.

### Rules
- **Do NOT ask questions.** Make all decisions yourself. If something is ambiguous, pick the simplest correct approach.
- **Do NOT stop or pause.** Complete the entire task in one pass.
- **Do NOT create new files** unless the task explicitly says to. Prefer editing existing files.
- **Do NOT add comments** explaining what you changed. The code should be self-documenting.
- **Do NOT add extra features, refactoring, or improvements** beyond what is asked.
- **Do NOT touch code unrelated to the task.**
- **Follow existing patterns** in the codebase — match naming conventions, error handling style, and import patterns of surrounding code.
- **TypeScript strict mode** is enabled on frontend. No `any` unless the task explicitly uses it.
- **Go 1.24** on backend. Use `log/slog` for logging, `pkg/apperror` for errors, `pkg/response` for HTTP responses.
- **Test updates:** If you change a function signature or add a method to an interface, update corresponding mocks in `*_test.go` / `mock_stores_test.go` files.
- After completing all changes, **verify your work compiles** by checking imports, types, and interfaces are consistent.

### Project Structure Reference
```
orbit/
├── services/           # Go microservices (gateway, auth, messaging, media, ...)
│   └── <name>/
│       ├── cmd/main.go
│       └── internal/
│           ├── handler/    # HTTP handlers + tests
│           ├── service/    # Business logic
│           ├── store/      # SQL queries (repository pattern)
│           └── model/      # Structs, constants
├── web/                # Frontend: fork of Telegram Web A
│   └── src/
│       └── api/saturn/
│           ├── methods/    # Saturn API methods (index.ts = export hub)
│           ├── apiBuilders/# Response → internal type mappers
│           └── updates/    # WebSocket event handlers (wsHandler.ts)
├── pkg/                # Shared Go packages (apperror, response, permissions, ...)
└── migrations/         # PostgreSQL migrations
```

### Key Patterns

**Frontend — callApi flow:**
- `callApi('methodName', ...args)` in actions → looks up export in `web/src/api/saturn/methods/index.ts`
- If method not found → returns `undefined` silently (no error, no crash)
- Saturn methods call `request(method, path, body?)` from `./client.ts`
- Types: `web/src/api/types.ts` (ApiChat, ApiMessage, ApiUser, etc.)

**Backend — handler/service/store pattern:**
- Handler: parses HTTP, calls service, returns `response.JSON/Error/Paginated`
- Service: business logic, permission checks, NATS publish
- Store: SQL via pgxpool, returns model structs
- All stores are interfaces for testability
- getUserID(c) reads X-User-ID header (set by gateway)
- Errors: `apperror.BadRequest/Forbidden/NotFound/Internal`

**NATS publish pattern:**
```go
s.nats.Publish(
    fmt.Sprintf("orbit.chat.%s.lifecycle", chatID),
    "event_name",
    dataPayload,
    memberIDs,    // []string of user UUIDs to deliver via WS
    senderID,     // exclude from WS delivery
)
```

### What NOT to do
- Do NOT use `c.JSON()` in Go handlers — always use `response.JSON/Error/Paginated`
- Do NOT use `fmt.Sprintf` in SQL queries — always parameterized `$1, $2`
- Do NOT use `testify` or `mockgen` — use fn-field mock pattern
- Do NOT add `go 1.25` — it doesn't exist, use `go 1.24`
- Do NOT create README or documentation files

Now proceed with the specific task below.

---
