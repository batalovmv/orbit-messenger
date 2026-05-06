# Saturn JetStream observability — Prometheus wiring

> Created 2026-05-05. Locally wired via `monitoring` profile in
> `docker-compose.yml`; this runbook activates the same on Saturn.

---

## Architecture

```
nats-js (port 8222 — JSON /varz, /jsz)
        │
        ▼
nats-exporter (natsio/prometheus-nats-exporter, port 7777 — /metrics)
        │
        ▼
Saturn managed Prometheus (scrapes /metrics)
        │
        ▼
Alert rules (orbit.nats group): JetStreamConsumerLag, JetStreamConsumerAckPending
```

NATS server itself does NOT speak Prometheus — port 8222 is JSON only.
The exporter bridges that to OpenMetrics, which Saturn's Prom can scrape.

---

## Activation on Saturn

### 1. Add `nats-exporter` resource

Saturn dashboard → Architecture → Add Service:
- Name: `orbit-nats-exporter`
- Image: `natsio/prometheus-nats-exporter:0.17.3`
- Port: `7777`
- Command / args:
  ```
  -port=7777 -jsz=all -varz -connz -subz http://nats-js:8222
  ```
- Healthcheck: HTTP GET `/metrics` on port 7777 (returns `# HELP ...`)

No env vars needed — the exporter is fully argument-driven.

### 2. Tell Saturn Prom to scrape it

If Saturn exposes scrape-config UI: add target `orbit-nats-exporter:7777`,
metrics path `/metrics`, job name `nats`.

If Saturn does NOT (current state — scrape config is internal-only),
file a Saturn FR or use their support channel: ask them to add
`nats` job pointing at `orbit-nats-exporter:7777`.

### 3. Load alert rules

The `orbit.nats` group in `monitoring/prometheus/rules/orbit.yml` is
already in the repo. If Saturn Prom auto-loads from a fixed rules path,
ensure it picks up that file. Otherwise paste the group into Saturn's
rule UI.

---

## Verification

After activation:

```bash
# 1. Exporter is exposing metrics
curl -s http://orbit-nats-exporter:7777/metrics | grep jetstream_consumer_num_pending

# Expect lines like:
# jetstream_consumer_num_pending{stream_name="ORBIT",consumer_name="bots-worker",...} 0
```

```promql
# 2. Prom is scraping
up{job="nats"} == 1

# 3. Alert evaluates without error
ALERTS{alertname="JetStreamConsumerLag"}
```

To force-fire the alert (smoke):

```bash
# On the bots service or any JS subscriber, pause the handler for 5 min
# under non-zero traffic. num_pending climbs, alert fires after for: 5m.
# Don't leave this on prod — fire on staging.
```

---

## Metric reference

| Metric | Meaning | Healthy |
|---|---|---|
| `jetstream_consumer_num_pending` | Messages in stream not yet delivered to this consumer | < 100, drains in seconds |
| `jetstream_consumer_num_ack_pending` | Messages delivered, awaiting ack | < 10 normally |
| `jetstream_consumer_num_redelivered` | Redeliveries (handler panic / ack timeout) | 0 ideally |
| `jetstream_stream_state_messages` | Total messages currently in stream | bounded by retention (24h) |
| `jetstream_stream_state_bytes` | Total bytes in stream | bounded by `MaxBytes` if set |

Common label cardinality: `stream_name`, `consumer_name`, `account`.

---

## Local equivalent

```bash
docker compose --profile monitoring up -d nats-exporter prometheus
curl -s http://localhost:9090/api/v1/targets | jq '.data.activeTargets[] | select(.labels.job=="nats")'
```

`prometheus_data` retains 7d locally — enough for noise-tuning the
threshold before wider rollout.
