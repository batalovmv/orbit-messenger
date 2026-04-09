# Slot 17: Docker / Deploy Audit

## Status: COMPLETED

## Scope

- `docker-compose.yml`
- `.saturn.yml`
- `.env.example`
- `services/ai/Dockerfile`
- `services/auth/Dockerfile`
- `services/bots/Dockerfile`
- `services/calls/Dockerfile`
- `services/gateway/Dockerfile`
- `services/integrations/Dockerfile`
- `services/media/Dockerfile`
- `services/meilisearch/Dockerfile`
- `services/messaging/Dockerfile`
- `web/Dockerfile`

## Checklist

- [x] Read `CLAUDE.md`
- [x] Pass 1: inspect compose, saturn config, env example
- [x] Pass 1: inspect all scoped Dockerfiles
- [x] Pass 2: verify each candidate finding against exact lines/config
- [x] Report HIGH/CRITICAL individually
- [x] Report MEDIUM only if confidence >= 0.9
- [x] Bucket LOW issues

## Findings

### HIGH

1. Public `coturn` relay falls back to known static credentials when env is missing
   - Evidence: `docker-compose.yml:83-96` exposes `3478/tcp`, `3478/udp`, and `49152-49200/udp` on all interfaces, while the server itself is configured with `--user=${TURN_USER:-orbit}:${TURN_PASSWORD:-orbit}`. The same fallback is mirrored into the `calls` service config at `docker-compose.yml:197-212`, where `TURN_USER` and `TURN_PASSWORD` also default to `orbit`. `.env.example:62-72` defines the intended env vars, but compose does not require them.
   - Impact: if `TURN_PASSWORD` is omitted or the stack is launched with a partial env file, the deployment comes up with a publicly reachable TURN relay authenticated by the predictable `orbit:orbit` credential pair. Anyone on the internet who knows the defaults can use the relay directly and burn bandwidth/ports without owning an Orbit account. That is a real unauthenticated abuse/DoS surface, not just a dev-only misconfiguration.
   - Why this clears the severity gate: exposure is remote, requires no prior auth, and directly grants relay resources on the public TURN endpoint.
   - Fix direction: make `TURN_USER` and `TURN_PASSWORD` mandatory with `:?`, or keep coturn unbound/non-public in default compose and only publish it in an explicitly production-hardened profile.

## Low Bucket

- `docker-compose.yml:83-228` has healthchecks only for infra services; `gateway`, `auth`, `messaging`, `media`, `calls`, `coturn`, and `web` have none, and `gateway` waits for several backends with `condition: service_started` rather than `service_healthy`. That is mostly deploy flakiness rather than a clear security bug.
- `docker-compose.yml` uses only the default compose network, so infra and app containers are not segmented into public/internal tiers. Fine for local dev, but it reduces blast-radius isolation if any one container is compromised.
- `docker-compose.yml:65-73`, `docker-compose.yml:185-189`, and `.env.example:48-55` still ship convenience MinIO defaults (`minioadmin`) through fallback expansion. They are loopback-bound in compose, so I am not escalating them individually, but they should stay strictly dev-only.
- `web/Dockerfile:16-19` leaves the final Nginx image on base-image defaults and adds no explicit non-root hardening.
- `services/gateway/Dockerfile:1-16`, `services/auth/Dockerfile:1-16`, `services/messaging/Dockerfile:1-16`, `services/media/Dockerfile:1-17`, and `services/calls/Dockerfile:1-16` are clean multi-stage builds, but they miss BuildKit cache mounts for `go mod` / `go build`, so rebuild cost is higher than necessary.
- `web/Dockerfile:4-14` installs `git` and `bash` into the builder and creates a synthetic git commit during image build. That increases layer weight and hurts reproducibility.
- `services/ai/Dockerfile:3-7`, `services/bots/Dockerfile:3-7`, and `services/integrations/Dockerfile:3-7` use a much broader `COPY . .` pattern than the other services, which is suboptimal for cache efficiency and can make the build context larger than needed.
- `.saturn.yml:4-73` declares ports for every component but contains no explicit healthcheck/readiness metadata, so operational correctness depends entirely on platform defaults.

## Notes

- Pass 2 completed. I only found one issue that confidently clears the requested HIGH/CRITICAL threshold.
- I did not report medium-severity items individually because none cleared the requested confidence bar.
