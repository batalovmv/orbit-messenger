# Sprint 1 — Dependency audit report

Date: 2026-04-21

## Go services

All 8 services share the same set of **stdlib vulnerabilities** (crypto/x509, crypto/tls, net/url, os) — these are fixed in Go 1.26.1 / 1.26.2 but **cannot be patched via go.mod**; they require upgrading the Go toolchain itself. The `go` directive is pinned at 1.24.0 per project policy, so these are noted but not actionable here.

### services/ai
- govulncheck: **7 symbol-level** (all stdlib) + 1 package-level + 4 module-level
- Stdlib (not patchable via deps): GO-2026-4947, GO-2026-4946, GO-2026-4870, GO-2026-4866, GO-2026-4601, GO-2026-4600, GO-2026-4599
- No third-party library vulns requiring action

### services/auth
- govulncheck: **8 symbol-level** (all stdlib) + 8 module-level
- Stdlib: GO-2026-4947, GO-2026-4946, GO-2026-4870, GO-2026-4866, GO-2026-4602, GO-2026-4601, GO-2026-4600, GO-2026-4599
- Module-level: golang.org/x/crypto@v0.31.0 → **v0.45.0** (GO-2025-4135, GO-2025-4134, GO-2025-4116, GO-2025-3487) — **patched**

### services/bots
- govulncheck: **7 symbol-level** (all stdlib) + 1 package-level + 4 module-level
- Stdlib: GO-2026-4947, GO-2026-4946, GO-2026-4870, GO-2026-4866, GO-2026-4601, GO-2026-4600, GO-2026-4599
- No third-party library vulns requiring action (x/crypto already at v0.49.0)

### services/calls
- govulncheck: **7 symbol-level** (all stdlib) + 1 package-level + 10 module-level
- Stdlib: GO-2026-4947, GO-2026-4946, GO-2026-4870, GO-2026-4866, GO-2026-4601, GO-2026-4600, GO-2026-4599
- Module-level patched:
  - golang.org/x/crypto@v0.41.0 → **v0.45.0** (GO-2025-4135, GO-2025-4134, GO-2025-4116)
  - golang.org/x/net@v0.43.0 → **v0.47.0** (GO-2026-4441, GO-2026-4440)
  - github.com/pion/interceptor@v0.1.37 → **v0.1.39** (GO-2025-3748 — RTP padding DoS)

### services/gateway
- govulncheck: **7 symbol-level** (all stdlib) + 1 package-level + 9 module-level
- Stdlib: GO-2026-4947, GO-2026-4946, GO-2026-4870, GO-2026-4866, GO-2026-4601, GO-2026-4600, GO-2026-4599
- Module-level patched:
  - golang.org/x/crypto@v0.41.0 → **v0.45.0**
  - golang.org/x/net@v0.43.0 → **v0.47.0**

### services/integrations
- govulncheck: **7 symbol-level** (all stdlib) + 1 package-level + 4 module-level
- Stdlib: GO-2026-4947, GO-2026-4946, GO-2026-4870, GO-2026-4866, GO-2026-4601, GO-2026-4600, GO-2026-4599
- No third-party library vulns requiring action

### services/media
- govulncheck: **7 symbol-level** (all stdlib) + 1 package-level + 8 module-level
- Stdlib: GO-2026-4947, GO-2026-4946, GO-2026-4870, GO-2026-4866, GO-2026-4601, GO-2026-4600, GO-2026-4599
- Module-level patched:
  - golang.org/x/crypto@v0.31.0 → **v0.45.0** (GO-2025-4135, GO-2025-4134, GO-2025-4116, GO-2025-3487)

### services/messaging
- govulncheck: **7 symbol-level** (all stdlib) + 3 package-level + 7 module-level
- Stdlib: GO-2026-4947, GO-2026-4946, GO-2026-4870, GO-2026-4866, GO-2026-4601, GO-2026-4600, GO-2026-4599
- Module-level patched:
  - golang.org/x/crypto@v0.41.0 → **v0.45.0**
  - golang.org/x/net@v0.43.0 → **v0.47.0** (GO-2026-4441, GO-2026-4440)

## Frontend (web/)

- npm audit (--production): **clean** — 0 high, 0 critical, 0 vulnerabilities total
- 38 prod dependencies scanned

## Patches applied

### services/auth
- golang.org/x/crypto: v0.31.0 → v0.45.0 (GO-2025-4135, GO-2025-4134, GO-2025-4116, GO-2025-3487)

### services/calls
- golang.org/x/crypto: v0.41.0 → v0.45.0 (GO-2025-4135, GO-2025-4134, GO-2025-4116)
- golang.org/x/net: v0.43.0 → v0.47.0 (GO-2026-4441, GO-2026-4440)
- github.com/pion/interceptor: v0.1.37 → v0.1.39 (GO-2025-3748)

### services/gateway
- golang.org/x/crypto: v0.41.0 → v0.45.0
- golang.org/x/net: v0.43.0 → v0.47.0

### services/media
- golang.org/x/crypto: v0.31.0 → v0.45.0 (GO-2025-4135, GO-2025-4134, GO-2025-4116, GO-2025-3487)

### services/messaging
- golang.org/x/crypto: v0.41.0 → v0.45.0
- golang.org/x/net: v0.43.0 → v0.47.0

## Open questions / requires review

1. **Go stdlib vulnerabilities (7 CVEs across all services)**: GO-2026-4947, GO-2026-4946, GO-2026-4870, GO-2026-4866, GO-2026-4601, GO-2026-4600, GO-2026-4599 — all in crypto/x509, crypto/tls, net/url. Fixed in Go 1.26.1/1.26.2, but `go` directive is pinned at 1.24.0. **Requires Go toolchain upgrade** (separate task).

2. **Stdlib module-only vulns** (not called by code): GO-2026-4869 (archive/tar), GO-2026-4865 (html/template XSS), GO-2026-4864 (os Root.Chmod, linux-only), GO-2026-4603 (html/template). Same root cause — needs Go toolchain bump.

3. **auth: GO-2026-4602** (os.ReadDir FileInfo escape) — symbol-level hit via migrator.Run. Fixed in Go 1.26.1. Toolchain upgrade required.

## Build verification

- services/auth: `go build ./...` → ✅ ok
- services/calls: `go build ./...` → ✅ ok
- services/gateway: `go build ./...` → ✅ ok
- services/media: `go build ./...` → ✅ ok
- services/messaging: `go build ./...` → ✅ ok
- services/ai: no dep changes — skipped
- services/bots: no dep changes — skipped
- services/integrations: no dep changes — skipped
