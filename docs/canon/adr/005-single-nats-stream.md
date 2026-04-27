# ADR 005 — Single NATS JetStream "ORBIT", Subjects by Domain

**Status:** ACCEPTED.

## Context

Phase 8D in the TZ called for **four** NATS JetStream streams, one per
domain: `events`, `messages`, `presence`, `calls`. The reasoning was
isolation — different retention, different consumer groups, blast-radius
containment if one stream's storage filled up.

In practice for a 150-user deployment:
- All four domains have the same operational SLA (24h retention is fine
  for everything; we're not archiving via JetStream).
- Cross-domain consumers (e.g. gateway needs both messages and presence)
  end up subscribing to multiple streams and reconciling order — a
  needless source of bugs.
- Four streams = four consumer-config blobs, four monitoring dashboards,
  four "is JetStream healthy?" checks. None of which earn their keep at
  this scale.
- Subject hierarchies already give us per-domain filtering inside one
  stream without paying the multi-stream cost.

## Decision

**One JetStream stream named `ORBIT`**, 24h retention, file storage.
Domain separation happens via subject prefixes:

- `orbit.msg.<chat_id>.<event>` — message create/edit/delete, reactions
- `orbit.presence.<user_id>` — online/offline/typing
- `orbit.call.<chat_id>.<event>` — call lifecycle (ring, accept, end)
- `orbit.audit.<actor_id>` — audit-log fanout
- `orbit.bot.<bot_id>.<event>` — bot delivery, integration routing

Consumers filter by subject pattern; no consumer needs to span multiple
streams.

## Consequences

- Single retention policy: 24h for everything. If a domain ever needs
  longer retention, prefer a dedicated downstream sink (Postgres,
  ClickHouse) over splitting the stream.
- Operationally simpler: one stream to size, monitor, back up, recreate.
  Disk-pressure incidents have one knob to turn.
- Subject naming is now **load-bearing** — renames break in-flight
  consumers. Treat `orbit.<domain>.*` as a public contract; document
  changes in `architecture.md`.
- If a single domain ever generates enough volume to monopolise the
  stream's IOPS budget, revisit and split — but only with measured data,
  not anticipation.
- `divergences.md` row about "4 streams" is the canonical pointer to this
  ADR; keep them in sync.
