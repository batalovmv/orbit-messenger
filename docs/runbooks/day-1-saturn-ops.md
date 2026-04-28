# Day 1 — Saturn ops day

> Created 2026-04-28 alongside the gateway push-metrics + Prometheus rules PR.
>
> This runbook is the operator checklist for the four Day 1 items the assistant cannot do remotely. Pair it with the dispatcher metrics + alert rules that ship in the same PR.
>
> Run order matters. Each section ends with an explicit ack line — copy that line into the launch tracker once you have evidence for every box.

---

## 1. WAL/PITR enablement

Follow `saturn-wal-pitr-enablement.md` end-to-end. Section 1 (verification) is read-only. Move to Section 2 only if `archive_mode=off` or `archived_count=0`.

**Pre-flight:** Saturn shell + `INTERNAL_SECRET` for `aws --endpoint-url` calls.

```bash
ssh saturn  # or open Coolify → Application → Console
cd /path/to/orbit/compose
```

Run Section 1 read-only block, capture output:

- [ ] `archive_mode` value: ____
- [ ] `archive_command` value: ____
- [ ] `archived_count`: ____
- [ ] `last_archived_time`: ____
- [ ] `failed_count`: ____
- [ ] R2 `s3://${R2_BACKUP_BUCKET}/postgres/` last entry timestamp: ____
- [ ] R2 `s3://${R2_BACKUP_WAL_BUCKET:-orbit-backup-wal}/wal-g/` listing populated? (Y/N) ____

Decision matrix from `saturn-wal-pitr-enablement.md` Section 1. If activation needed, follow Section 2 verbatim. If activation not needed, skip to Section 4 (drill — but the drill itself is Day 2 work; just confirm telemetry is live here).

After activation — **proof set**:

- [ ] `archive_mode=on` after `docker restart orbit-postgres-1`
- [ ] `archived_count` increased after `pg_switch_wal()`
- [ ] `failed_count` stayed at 0
- [ ] First `wal-g backup-push` produced exactly one `base_<lsn>` row in `wal-g backup-list`
- [ ] R2 `wal-g/` prefix has at least one new object created in the last 5 min

**Ack:** WAL is flowing into R2 and a base backup exists. Record values + timestamps in the pilot tracker.

---

## 2. TRUSTED_PROXIES on Saturn

The warning at `services/gateway/cmd/main.go:147` reads:

> `TRUSTED_PROXIES not set — X-Forwarded-For will be ignored, c.IP() returns raw connection IP`

When the gateway sees Coolify's ingress as the only client IP, every per-IP rate limit (auth, sensitive endpoints, invite validation) collapses into a single bucket. Setting `TRUSTED_PROXIES` to the upstream subnet restores per-real-client buckets.

### 2.1 Find the upstream subnet

```bash
docker network inspect $(docker network ls --filter name=orbit -q) \
  --format '{{range .IPAM.Config}}{{.Subnet}}{{"\n"}}{{end}}'
```

This is the bridge subnet that Coolify's ingress connects from (e.g. `172.18.0.0/16`). On Coolify v4 the Caddy ingress lives on the same bridge, so a single subnet entry covers it.

Record the subnet here: ____

### 2.2 Set the env

If the project's compose env file is the `.env` next to `docker-compose.yml`:

```bash
grep -E '^TRUSTED_PROXIES=' .env || echo "TRUSTED_PROXIES=<subnet-from-2.1>" >> .env
docker compose up -d gateway
```

If Coolify manages env vars through its UI, add `TRUSTED_PROXIES` there and trigger a gateway redeploy from the Coolify UI.

Belt-and-braces value if you can't isolate the subnet:

```
TRUSTED_PROXIES=10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,127.0.0.1/8
```

This trusts only RFC1918 + loopback — i.e. nothing the public internet can directly connect from. Safe default.

### 2.3 Confirm the warning is gone

```bash
docker logs --since 2m orbit-gateway-1 2>&1 | grep -i 'TRUSTED_PROXIES'
```

Empty output = the gateway came up without firing the warning.

Sanity-check rate limiting: hit `/api/v1/auth/login` from two different real client IPs and confirm each gets its own bucket (different `X-RateLimit-Remaining` headers / 429 thresholds).

**Ack:** warning gone, `c.IP()` returns the real upstream client IP, per-IP rate limit buckets are individual again.

---

## 3. iPhone real-device push test

Follow `iphone-push-test.md` literally. Use the pilot URL (HTTPS), not localhost.

**Pre-flight:**
- iPhone running iOS 16.4 or later (web push requires it).
- The `iphone-test@…` account is pre-created and added to a 2-person DM with a desktop sender.
- The PWA has been added to home screen at least once.

Walk through the 7 stages from the runbook and check each:

- [ ] Stage 1 — service worker registered (Safari → Develop → iPhone → Application → Service Workers)
- [ ] Stage 2 — notification permission granted in iOS Settings → Orbit
- [ ] Stage 3 — `pushManager.subscribe()` succeeded (verified in console)
- [ ] Stage 4 — `push_subscriptions` row exists (psql query in the runbook)
- [ ] Stage 5 — desktop sender posts message → gateway log line `push: dispatched`
- [ ] Stage 6 — banner appears within ~5 s on locked phone
- [ ] Stage 7 — tap opens Orbit at the right DM, counter clears

Record:
- iOS version: ____
- Device model: ____
- Result of each stage (PASS/FAIL): ____

**With this PR landed**, after a successful test you should also see `orbit_push_attempts_total{result="ok"}` increment by exactly one in Prometheus:

```bash
docker exec orbit-prometheus-1 sh -c \
  'wget -qO- "http://localhost:9090/api/v1/query?query=orbit_push_attempts_total"' \
  | python3 -m json.tool
```

If Saturn does not have a Prometheus deployment, scrape the gateway directly with the internal token from a shell on the gateway host:

```bash
curl -sH "X-Internal-Token: $INTERNAL_SECRET" http://gateway:8080/metrics | grep orbit_push_attempts_total
```

**Ack:** banner fired, tap routed to the chat, gateway emitted exactly one `orbit_push_attempts_total{result="ok"}` increment.

---

## 4. Alert wiring smoke test

This PR ships:

1. Fixed Prometheus rules — `orbit_http_requests_total`, `orbit_http_request_duration_seconds_bucket`, `orbit_ws_active_connections` (the previous unprefixed names matched no series, so HighErrorRate / HighLatency / HighWSConnections never fired). The dead `HighDBConnections` rule was dropped — bring it back when a postgres_exporter sidecar is added.
2. New rules — `WSAbnormalDrops`, `PushDeliveryFailureRate`.
3. New metric — `orbit_push_attempts_total{result="ok|fail|stale"}` emitted by the gateway push dispatcher.

After deploy:

- [ ] `docker exec orbit-prometheus-1 promtool check rules /etc/prometheus/rules/orbit.yml` returns "SUCCESS: 7 rules found".
- [ ] Hot-reload Prometheus: `curl -X POST http://127.0.0.1:9090/-/reload`. (Or restart the container.)
- [ ] Visit the Prometheus UI → Alerts: all 7 rules show, none firing.
- [ ] Trigger a synthetic push failure (point a test push at `https://nonexistent.invalid/` and watch `orbit_push_attempts_total{result="fail"}` tick up). Verify the alert moves to "Pending" then "Firing" within ~5 min.
- [ ] Confirm Alertmanager forwards the alert to the integrations webhook:
  ```bash
  docker logs --since 5m orbit-integrations-1 | grep alertmanager
  ```
  A POST hitting the `inbound_webhook` endpoint with `preset_id=alertmanager` is the proof.
- [ ] Once verified, silence the synthetic alert via Alertmanager UI before it pages anyone.

**Deferred to Day 7 (Observability):**
- WAL archive lag alert — needs `postgres_exporter` sidecar exposing `pg_stat_archiver_*` metrics.
- NATS consumer lag alert — needs either `nats_exporter` or a gauge in `services/gateway/internal/ws/nats_subscriber.go`.

The Day 1 PR explicitly does not add either. Both are tracked under the Day 7 task list.

**Ack:** Prometheus rule file passes promtool, all 7 rules load, the synthetic push-failure alert reaches the integrations webhook, then is silenced.

---

## Sign-off

When all four sections are acked, paste the four ack lines into the pilot tracker, mark Day 1 complete, and message the on-call channel:

> Day 1 ops complete: WAL/PITR live, TRUSTED_PROXIES set to `<subnet>`, iPhone push end-to-end on iOS `<version>` `<model>`, alert rules loaded and synthetic-fire confirmed.
