# HR Bot — Installation & Usage

Minimal vacation / sick-leave / day-off workflow for a 150-employee corporate
deployment. The HR bot exposes 3 REST endpoints in the `bots` service and
a dedicated `bot_hr_requests` table (migration 062). No command parser, no
automatic message generation — the bot is **registered like any other bot**
via BotFather, and approve/reject flow runs through the Bot API `sendMessage`
with `reply_markup` inline keyboards.

This keeps the server minimal; a thin client (HR manager's device or a
separate daemon) drives the conversational UX.

---

## 1. Provision the bot

1. HR manager opens Orbit → Settings → Bots → **Create bot** (BotFather).
   - username: `hr_bot` (or similar)
   - display name: `HR Assistant`
2. Note the **bot ID** (UUID) from the bot details view.
3. Install the bot into the HR chat:
   - `POST /api/v1/bots/:botID/install` with `chat_id` = HR chat UUID and
     `scopes = ScopePostMessages | ScopeReceiveCallbacks` (bits 1 + 4 = 5).
4. The HR manager who created the bot becomes the **approver** — the endpoint
   `PATCH /hr/requests/:id` returns 403 for any other user.

---

## 2. REST endpoints

All endpoints are internal (gateway proxies `/api/v1/*` with `X-Internal-Token`
auth + `X-User-ID` for the calling user).

### `POST /api/v1/bots/:botID/hr/requests`

Create a pending request. Any authenticated user can call this as long as
the bot is installed in `chat_id` with `ScopePostMessages`.

Body:
```json
{
  "chat_id":      "<chat uuid>",
  "request_type": "vacation | sick_leave | day_off",
  "start_date":   "2026-05-01",
  "end_date":     "2026-05-10",
  "reason":       "optional free text, <=500 chars"
}
```

Response `201 Created`:
```json
{
  "id": "...",
  "bot_id": "...",
  "chat_id": "...",
  "user_id": "...",
  "request_type": "vacation",
  "start_date": "2026-05-01T00:00:00Z",
  "end_date":   "2026-05-10T00:00:00Z",
  "status": "pending",
  "created_at": "..."
}
```

Errors:
- `400` — invalid dates, unknown request_type, reason too long
- `401` — missing X-User-ID
- `403` — bot not installed in chat

### `GET /api/v1/bots/:botID/hr/requests?status=pending`

- Bot owner → all requests for this bot
- Other users → only their own requests

Optional filter: `status` = `pending | approved | rejected`

Response `200 OK`: `{ "requests": [ ... ] }`

### `PATCH /api/v1/bots/:botID/hr/requests/:id`

Approve or reject. Only the **bot owner** (HR manager who created the bot)
may call this.

Body:
```json
{
  "decision": "approve | reject",
  "note": "optional free text, <=500 chars"
}
```

Errors:
- `403` — caller is not the bot owner
- `404` — request not found OR request belongs to a different bot
- `409` — request already decided (idempotency guard)

---

## 3. Conversational UX (recommended flow)

The server is workflow-agnostic — the HR manager's client drives the UX.
Recommended pattern for MST:

1. Employee types `/vacation 2026-05-01 2026-05-10 family` in the HR chat.
2. HR bot (external daemon or manual HR manager action) calls:
   - `POST /hr/requests` with parsed dates
3. HR bot replies in chat via Bot API `sendMessage`:
   - text: `"Request #12 created, awaiting HR"`
   - `reply_markup.inline_keyboard`:
     ```json
     [[
       {"text": "Approve", "callback_data": "hr:approve:<request-id>"},
       {"text": "Reject",  "callback_data": "hr:reject:<request-id>"}
     ]]
     ```
4. HR manager taps "Approve" → callback fires `sendBotCallback` → HR bot
   receives callback → calls `PATCH /hr/requests/:id` with decision.
5. HR bot edits the original message (`editMessageText`) to reflect final
   status and notifies the requester via another `sendMessage`.

All inline-keyboard infrastructure (rendering, callback routing) is already
in place — see commit `7a819bc` (validator) and upstream Telegram Web A
`InlineButtons.tsx` component which is wired into `Message.tsx`.

---

## 4. Schema

See `migrations/062_bot_hr_requests.sql`:

```sql
CREATE TABLE bot_hr_requests (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_id        UUID NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    chat_id       UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    request_type  TEXT NOT NULL CHECK (request_type IN ('vacation','sick_leave','day_off')),
    start_date    DATE NOT NULL,
    end_date      DATE NOT NULL,
    reason        TEXT,
    status        TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending','approved','rejected')),
    approver_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    decision_note TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT bot_hr_requests_dates_ordered CHECK (end_date >= start_date)
);
```

Indexes cover: by-user+status (employee's own list), by-chat+status
(HR chat queue), and a partial index on `status='pending'` for the
approval queue.

---

## 5. Security model (for 150-employee deployment)

- **No separate HR role table.** The bot owner IS the approver. For 1-2 HR
  managers this is sufficient. Scale-out (approval chains, delegation) is
  explicitly out of scope — Orbit's target is 150 employees, not 15,000.
- **Requests are bot-scoped.** `PATCH` validates that the request's `bot_id`
  matches the URL `:botID` — prevents a stranger from approving requests
  across bots they happen to own.
- **Status transitions are atomic.** The `Decide` store method uses a single
  `UPDATE ... WHERE status='pending' RETURNING *`. Concurrent approve/reject
  races result in `ErrHRRequestAlreadyFinal` (409) for the loser.
- **Employee visibility.** `GET /hr/requests` automatically scopes to the
  caller's `user_id` when they are not the bot owner. No leakage across
  employees.
- **Rate limiting.** Inherits the bots-service Redis limiter (30 req/sec
  per authenticated caller).

---

## 6. Out of scope (deliberately)

- Manager hierarchy / approval chains
- Delegation during approver's own vacation
- Calendar integration (ICS export)
- Half-day and partial-day requests
- Balance tracking (remaining vacation days)
- Multi-language templates (RU translations are on the client via `lang()`)

Any of these can be added later as they become real needs — for 150
employees they are premature optimization.
