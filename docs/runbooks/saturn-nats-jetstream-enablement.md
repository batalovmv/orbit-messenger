# Saturn NATS — switch to JetStream-enabled image

> **Created 2026-05-04** after the prod log audit caught two startup
> WARNs in gateway:
>
> ```
> JetStream ORBIT stream not created, falling back to core NATS
> nats: JetStream subscribe failed, falling back to core NATS
> ```
>
> Saturn pulls `nats:latest` with default args — no `-js`, no token
> auth, no monitoring port. Gateway falls back to core NATS, which
> means **no 24h replay, no Nats-Msg-Id dedup, no persistence**. The
> first reconnect storm after a gateway redeploy will silently drop
> in-flight messages.
>
> The fix is a custom Dockerfile (`deploy/nats/Dockerfile`) that
> baselines the right config. This runbook walks through the Saturn
> UI flip and verification.

---

## 1. Pre-flight

- `deploy/nats/Dockerfile` and `deploy/nats/nats.conf` exist on `main`
- `NATS_TOKEN` env var is already set on the orbit-nats service
  (Saturn → orbit-nats → Variables)
- Pick a low-traffic window. **Estimated outage: 15-30 s** for the NATS
  recreate. Gateway will reconnect automatically; existing WS sessions
  on clients ride through.

---

## 2. Flip the source on Saturn UI

1. Architecture → double-click `orbit-nats` → Settings tab
2. Section **Source**: change from
   - **Docker Image** `nats:latest`

   to:
   - **Dockerfile** with path `deploy/nats/Dockerfile`
3. Section **Networking → Application Type**: set to a non-HTTP type if
   the dropdown allows ("TCP service" or similar). NATS speaks the NATS
   protocol on 4222, not HTTP. If the only choice is Web/HTTP, leave
   it — Saturn just gets the routing wrong but the gateway talks to
   the container directly via `nats:4222`, which keeps working.
4. Section **Port Configuration**: expose `4222, 8222`. The current
   `80` value is wrong (NATS doesn't speak HTTP on 80) but harmless —
   gateway uses internal networking on 4222.
5. Click **Save Settings**.
6. Click **Deploy Now** at the top of the panel.

---

## 3. Verify

After Deploy completes (1-2 min):

### 3.1 NATS itself is up with JetStream

In a browser tab, open the orbit-nats public URL appending `/varz`:

```
https://new-tg-fgbo1i.saturn.ac/varz
```

(adjust the subdomain — yours is the URL shown in the orbit-nats
panel header). Look for:

```json
"jetstream": {
  "config": {
    "max_memory": 268435456,
    "max_storage": 2147483648,
    "store_dir": "/data/jetstream/jetstream"
  }
},
"auth_required": true
```

If `jetstream.config` is null → the Dockerfile didn't take. Check
deploy logs.

### 3.2 Gateway sees the stream

Saturn → Logs → gateway, filter to all levels. After the next gateway
restart (Saturn restarts dependants automatically when NATS redeploys)
you should see:

```
INFO  NATS connected
INFO  JetStream ORBIT stream created  max_age=24h
INFO  nats: JetStream durable subscriber started  durable=gateway-ws subject=orbit.>
```

The pre-fix WARNs (`JetStream ORBIT stream not created, falling back to
core NATS`) must NOT appear.

### 3.3 Smoke test

1. Open the web app, send any message. It should arrive on a second
   connected client within ~200 ms.
2. Force-reconnect a client (toggle WS by going offline/online). The
   missed messages from the disconnect window should replay through
   JetStream — they would not have under core NATS fallback.

---

## 4. Rollback

If something goes wrong (Dockerfile build fails, gateway can't
connect, JetStream init loops):

1. Settings → Source → revert to **Docker Image** `nats:2-alpine`
2. Save Settings → Deploy Now

This drops back to the pre-fix state (no JetStream) but keeps the
service up. Investigate the Dockerfile or config in dev, push a fix,
flip the source again.

---

## 5. Notes

- **Volume**: the Dockerfile declares `/data` as a VOLUME so Saturn's
  managed storage attaches automatically. Stream data persists across
  container restarts.
- **Local dev**: `docker compose up -d --build nats` rebuilds from the
  same Dockerfile, so dev and Saturn stay byte-for-byte identical.
- **Monitoring**: `pg_stat_archiver`-style alerts for JetStream
  health are not yet wired. Future: scrape `/jsz` from postgres-exporter
  pattern and alert on `streams.consumers.delivered.consumer_seq`
  stalls. Out of scope for this runbook.
