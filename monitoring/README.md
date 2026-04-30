# Monitoring (local dev)

Stack: **Prometheus** scrapes every Orbit service's `/metrics` endpoint, **Grafana** renders dashboards.

This is **optional local-dev tooling**. It is disabled in the default Docker Compose stack
so routine app/PWA testing does not spend ports and resources on dashboards.

## Run

```bash
docker compose --profile monitoring up -d prometheus grafana alertmanager
```

- Grafana: http://localhost:3001 (anonymous Viewer access — no login needed for read-only)
- Prometheus: http://localhost:9090

Dashboards auto-load on container start from `monitoring/grafana/dashboards/*.json`.

## Dashboards

| Dashboard | UID | What |
|---|---|---|
| **Orbit — SLO Overview** | `orbit-slo-overview` | RPS, error rate (5xx), p95/p99 latency vs SLO targets, WebSocket connection count, top routes by p95 |
| **Orbit — Service Health** | `orbit-service-health` | Goroutines, heap, CPU, GC pause, FDs, RSS — per service |

Both dashboards have a `service` template variable (multi-select) for filtering.

## SLO targets (per `CLAUDE.md`)

- Message delivery p99 < 100ms
- API response p95 < 200ms
- WebSocket connections 500 concurrent/instance
- Frontend TTI < 3s

## Adding a dashboard

1. Drop a `.json` file into `monitoring/grafana/dashboards/`. Grafana picks it up within 30s (provisioning poll interval).
2. Set `"uid": "orbit-<name>"` so the dashboard URL is stable.
3. Reference the Prometheus datasource as `{ "type": "prometheus", "uid": "prometheus" }`.

## Adding a metric

Use the shared `pkg/metrics` Registry — every service already constructs one in `main.go`:

```go
// in cmd/main.go (already exists)
mreg := metrics.New("myservice")
app.Use(mreg.HTTPMiddleware())
app.Get("/metrics", mreg.Handler())

// add a custom metric
myCounter := mreg.Counter("orbit_foo_total", "Things happened", "label")
myCounter.WithLabelValues("bar").Inc()
```

`HTTPMiddleware()` already exports the standard RED metrics:

- `orbit_http_requests_total{service,method,route,status}` (Counter)
- `orbit_http_request_duration_seconds{service,method,route,status}` (Histogram)

Plus Go runtime + process metrics from the default Prometheus collectors.
