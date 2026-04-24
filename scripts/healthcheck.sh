#!/usr/bin/env bash
# External healthcheck for Orbit Messenger services.
# Polls /health on all 8 services (via gateway proxy).
# On failure, sends an alert to Orbit via the integrations webhook.
#
# Required env vars:
#   ORBIT_BASE_URL          — base URL of the gateway, e.g. https://orbit.mst.corp
#   HEALTHCHECK_WEBHOOK_URL — full URL of the integrations webhook endpoint
#   HEALTHCHECK_SECRET      — Bearer token / API key for the connector
#
# Usage (one-shot):
#   ./scripts/healthcheck.sh
#
# Usage (continuous loop, every 30s):
#   while true; do ./scripts/healthcheck.sh; sleep 30; done
#
# Usage (cron, every minute):
#   * * * * * /path/to/orbit/scripts/healthcheck.sh >> /var/log/orbit-healthcheck.log 2>&1

set -euo pipefail

ORBIT_BASE_URL="${ORBIT_BASE_URL:-http://localhost:8080}"
HEALTHCHECK_WEBHOOK_URL="${HEALTHCHECK_WEBHOOK_URL:-}"
HEALTHCHECK_SECRET="${HEALTHCHECK_SECRET:-}"
TIMEOUT="${HEALTHCHECK_TIMEOUT:-5}"

SERVICES=(
  "gateway:${ORBIT_BASE_URL}/health"
  "auth:${ORBIT_BASE_URL}/api/v1/auth/health"
  "messaging:${ORBIT_BASE_URL}/api/v1/messaging/health"
  "media:${ORBIT_BASE_URL}/api/v1/media/health"
  "calls:${ORBIT_BASE_URL}/api/v1/calls/health"
  "ai:${ORBIT_BASE_URL}/api/v1/ai/health"
  "bots:${ORBIT_BASE_URL}/api/v1/bots/health"
  "integrations:${ORBIT_BASE_URL}/api/v1/integrations/health"
)

FAILED=()

for entry in "${SERVICES[@]}"; do
  name="${entry%%:*}"
  url="${entry#*:}"

  http_code=$(curl -s -o /dev/null -w "%{http_code}" --max-time "${TIMEOUT}" "${url}" 2>/dev/null || echo "000")

  if [[ "${http_code}" != "200" ]]; then
    FAILED+=("${name}(HTTP ${http_code})")
    echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] FAIL ${name} — ${url} returned ${http_code}"
  else
    echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] OK   ${name}"
  fi
done

if [[ ${#FAILED[@]} -eq 0 ]]; then
  echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] All services healthy."
  exit 0
fi

# Build comma-separated list of failed services
FAILED_LIST=$(printf '%s, ' "${FAILED[@]}")
FAILED_LIST="${FAILED_LIST%, }"

echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] Sending alert for: ${FAILED_LIST}"

if [[ -z "${HEALTHCHECK_WEBHOOK_URL}" ]]; then
  echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] WARN: HEALTHCHECK_WEBHOOK_URL not set, skipping webhook." >&2
  exit 1
fi

if [[ -z "${HEALTHCHECK_SECRET}" ]]; then
  echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] WARN: HEALTHCHECK_SECRET not set, skipping webhook." >&2
  exit 1
fi

TIMESTAMP=$(date +%s)

# Build JSON safely via printf — avoids injection from service names
PAYLOAD=$(printf '{"event":"healthcheck.failed","failed_services":"%s","timestamp":%d}' \
  "${FAILED_LIST}" "${TIMESTAMP}")

# Send webhook with Bearer auth; capture HTTP response code
RESPONSE_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
  --max-time "${TIMEOUT}" \
  -X POST \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${HEALTHCHECK_SECRET}" \
  -d "${PAYLOAD}" \
  "${HEALTHCHECK_WEBHOOK_URL}" 2>/dev/null || echo "000")

if [[ "${RESPONSE_CODE}" =~ ^2 ]]; then
  echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] Alert delivered (HTTP ${RESPONSE_CODE})."
else
  echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] WARN: Webhook delivery failed (HTTP ${RESPONSE_CODE})." >&2
fi

exit 1
