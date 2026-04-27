# docs/canon — Canonical Facts for AI Agents

Reading order:
1. **state.json** — machine-readable snapshot (phase, last migration, go versions, removed features)
2. **current-state.md** — human summary of what is actually built and running
3. **divergences.md** — where TZ/PHASES.md differ from reality
4. **architecture.md** — services, ports, shared packages
5. **conventions.md** — code style, SQL patterns, git rules
6. **adr/** — Architecture Decision Records
   - [001 — No End-to-End Encryption](adr/001-no-e2e-encryption.md) — AES-256-GCM at-rest only; Phase 7 Signal Protocol rolled back (mig 053).
   - [002 — No Broadcast Channels](adr/002-no-channels.md) — Channels removed in mig 035; DM + groups cover corporate use cases.
   - [003 — Gateway on Go 1.25](adr/003-gateway-go-1.25.md) — Gateway pinned to 1.25 for embedded `nats-server/v2`; rest stays on 1.24.
   - [004 — RBAC: 4 System Roles + Chat Bitmask](adr/004-rbac-bitmask.md) — superadmin/compliance/admin/member; bits in `pkg/permissions/system.go`.
   - [005 — Single NATS Stream "ORBIT"](adr/005-single-nats-stream.md) — One JetStream, 24h retention, subjects by domain.
