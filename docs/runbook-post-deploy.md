# Orbit post-deploy verification runbook

Checklist to run after every `git push origin main` that triggers a Saturn
auto-deploy. Goal: confirm prod is healthy before walking away.

## 1. Health endpoints

Hit these immediately after Saturn shows green:

```bash
GATEWAY="<GATEWAY_URL>"   # TODO: replace with prod gateway URL

# Gateway liveness + readiness
curl -sf "${GATEWAY}/health/live"   && echo "OK: live"
curl -sf "${GATEWAY}/health/ready"  && echo "OK: ready"
```

### Per-service health (via gateway proxy)

All services are behind the gateway. If any service exposes a direct health
endpoint, check it too:

| Service | Endpoint | Expected |
|---------|----------|----------|
| gateway | `${GATEWAY}/health/live` | `200` |
| gateway | `${GATEWAY}/health/ready` | `200` |
| auth | `${GATEWAY}/api/v1/auth/health` | `200` — TODO: verify route |
| messaging | `${GATEWAY}/api/v1/messaging/health` | `200` — TODO: verify route |
| media | `${GATEWAY}/api/v1/media/health` | `200` — TODO: verify route |
| calls | `${GATEWAY}/api/v1/calls/health` | `200` — TODO: verify route |
| ai | `${GATEWAY}/api/v1/ai/health` | `200` — TODO: verify route |
| bots | `${GATEWAY}/api/v1/bots/health` | `200` — TODO: verify route |
| integrations | `${GATEWAY}/api/v1/integrations/health` | `200` — TODO: verify route |

**When to panic**: if gateway `/health/ready` returns non-200 for > 1 min after
deploy completed — something is wrong, don't wait.

## 2. UI smoke checklist (~2–3 min)

Open the web app in a browser:

- [ ] Page loads without blank screen or JS errors (check console)
- [ ] Login with test user → lands on chat list
- [ ] Open a test chat → messages load
- [ ] Send a message → appears in chat, no spinner stuck
- [ ] Check WebSocket: DevTools → Network → WS → filter `/api/v1/ws` → should
      show `101 Switching Protocols` and frames flowing
- [ ] Hit `${GATEWAY}/api/v1/ai/health` in browser — should NOT return `503`
      (AI service stub should return 200)

If any of these fail → go to [runbook-rollback.md](runbook-rollback.md).

## 3. Metrics & observability

### Where to look

| What | Where |
|------|-------|
| Service logs | Saturn dashboard → select service → Logs tab |
| Local dev logs | `docker compose logs -f --tail=100` |
| Dashboards | TODO: link to Grafana/monitoring dashboards when available |

### Red flags in metrics

| Metric | Threshold | Action |
|--------|-----------|--------|
| HTTP error rate (5xx) | > 1% over 5 min window | Investigate immediately |
| API latency p95 | > 2× baseline (pre-deploy) | Investigate, likely perf regression |
| Memory usage | > 80% of container limit | Watch closely, may OOM soon |
| WebSocket disconnects | Spike > 10× normal | Check gateway logs |
| DB connection pool | Exhausted / waiting queries | Check for missing connection releases |

### Log patterns to grep for

```bash
# On Saturn or local, look for these in the last 10 min:
# panic / fatal — service crashed
# "connection refused" — downstream service not reachable
# "deadline exceeded" — timeouts
# "duplicate key" — migration or data issue
```

## 4. Rollback trigger criteria

Switch to [runbook-rollback.md](runbook-rollback.md) if ANY of these:

- [ ] Health endpoint returns non-200 for > 2 min after deploy
- [ ] HTTP 5xx error rate > 5% sustained over 5 min
- [ ] Users report they cannot login or send messages
- [ ] WebSocket refuses to connect (gateway down or broken upgrade)
- [ ] Any service is crash-looping (repeated restarts in Saturn dashboard)

Do **not** wait for "it might fix itself" — rollback is cheap (~7 min total),
broken prod is expensive.

## 5. Sign-off

Deploy is considered complete when:

- [ ] All health endpoints return 200
- [ ] UI smoke checklist passed
- [ ] Metrics stable for 10 min post-deploy (no error spikes)
- [ ] Posted in `#deploys` Slack channel:
      ```
      ✅ Deploy <short-sha> — <commit summary>
      Smoke: passed | Metrics: stable | Rollback: not needed
      ```

If anything required a rollback, post a brief incident summary instead:
```
🔴 Deploy <short-sha> rolled back — <reason>
Reverted in <revert-sha>. Investigating root cause.
```
