// 150-user load profile for the local Orbit stack.
//
// What it does:
//   1. setup(): registers (or reuses) 150 unique users against /api/v1/auth/register
//      and logs each one in to obtain access tokens. Token list passed to VUs.
//   2. default(): per VU, picks "its own" user, runs a realistic loop:
//        GET /api/v1/auth/me  → JWT validation hot path (Postgres + Redis JWT cache)
//        GET /api/v1/chats    → proxy to messaging
//        sleep 1-3s
//      VU stays on its own JWT for the whole run, so Redis JWT cache hits.
//
// Why this shape:
//   - 150 logins concurrently would all hit auth_sensitive rate-limit (5/min).
//     Setup-time logins are sequential and only run once, so they pass.
//   - The hot loop hits the api group (600/min/user) which is well above the
//     ~30-60 req/min/user we generate.
//   - Token reuse simulates a typical day where users authenticate once and
//     keep using the same access JWT until refresh.
//
// How to run (from repo root):
//
//   docker run --rm -i --network orbit_default \
//     -v "$(pwd)/tests/load:/scripts" \
//     -e BASE_URL=http://gateway:8080 \
//     grafana/k6:0.50.0 run /scripts/k6-150-users.js
//
// Tunables via env:
//   BASE_URL    default http://gateway:8080  (use http://host.docker.internal:8080
//               from outside the docker-compose network)
//   USERS       default 150
//   DURATION    default 60s
//   REGISTER_PASSWORD  default LoadTest!2026  (must match initial setup or
//                                               existing users will fail to log in)
import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Trend, Rate, Counter } from 'k6/metrics';
import { SharedArray } from 'k6/data';

// Pre-minted JWTs (see tests/load/mint-tokens.go). One token per loadtest user.
// Avoids the 5/min/IP rate limit on /auth/login that would otherwise serialise
// the 150-VU setup phase across 30 minutes.
const TOKENS = new SharedArray('tokens', () => JSON.parse(open('./tokens.json')));

const BASE_URL = __ENV.BASE_URL || 'http://gateway:8080';
// Setup logins go DIRECT to the auth service (bypasses gateway's
// auth_sensitive rate limit which keys by IP and would 429 us at 5/min when
// all 150 setup-time logins originate from the k6 container's bridge IP).
// On Saturn the equivalent is the internal auth URL — same idea.
const AUTH_DIRECT_URL = __ENV.AUTH_DIRECT_URL || 'http://auth:8081';
const USERS = parseInt(__ENV.USERS || '150', 10);
const DURATION = __ENV.DURATION || '60s';
const PASSWORD = __ENV.REGISTER_PASSWORD || 'LoadTest!2026';

// Custom metrics — show up in the summary.
const meTrend = new Trend('me_duration', true);
const chatsTrend = new Trend('chats_duration', true);
const errorRate = new Rate('errors');
const meSuccess = new Counter('me_success');
const chatsSuccess = new Counter('chats_success');

export const options = {
  scenarios: {
    constant_150: {
      executor: 'constant-vus',
      vus: USERS,
      duration: DURATION,
      gracefulStop: '5s',
    },
  },
  thresholds: {
    // Realistic SLOs for a 150-user pilot tenant.
    'http_req_duration{expected_response:true}': ['p(95)<400', 'p(99)<800'],
    'errors': ['rate<0.01'],
    'me_duration': ['p(95)<300'],
    'chats_duration': ['p(95)<400'],
    'http_req_failed': ['rate<0.01'],
  },
  // Saturn pilot tenant has 8 backend services on shared box; not the right
  // place to test extreme tail. We only care about steady-state behaviour.
  noConnectionReuse: false,
  insecureSkipTLSVerify: true,
}

export function setup() {
  // Sanity check the tokens file once.
  const ok = TOKENS.filter(t => t && t.token).length;
  console.log(`[setup] ${ok}/${TOKENS.length} pre-minted tokens loaded; BASE_URL=${BASE_URL}`);
  if (ok < USERS) {
    throw new Error(`Need ${USERS} tokens, found ${ok}. Run \`go run tests/load/mint-tokens.go > tests/load/tokens.json\` first.`);
  }
  return {};
}

export default function () {
  const idx = (__VU - 1) % TOKENS.length;
  const u = TOKENS[idx];
  if (!u) {
    sleep(1);
    return;
  }
  const headers = {
    'Authorization': `Bearer ${u.token}`,
    'Content-Type': 'application/json',
  };

  // We deliberately don't hit /api/v1/auth/me here:
  //   * /auth/me is rate-limited 60/min/IP via authSessionRateLimit.
  //     A k6 container with 150 VUs sends from one bridge IP, so all 150
  //     users collapse into one bucket and start failing.
  //   * On prod that bucket is keyed by client real IP (X-Forwarded-For),
  //     but only if TRUSTED_PROXIES is set on the gateway. Verify on Saturn
  //     before assuming /me scales — see audits/load-2026-04-27.md.
  // For load profiling we exercise the apiGroup endpoints (600/min/user),
  // which represent the realistic post-login hot path.
  group('apiGroup hot path', () => {
    const chatsRes = http.get(`${BASE_URL}/api/v1/chats`, {
      headers, tags: { name: 'list_chats' },
    });
    chatsTrend.add(chatsRes.timings.duration);
    const chatsOk = check(chatsRes, {
      'chats 200': (r) => r.status === 200,
    });
    if (chatsOk) chatsSuccess.add(1);
    errorRate.add(!chatsOk);

    // /users/me/notification-priority lives in apiGroup (NOT auth proxy).
    // Per-user rate-limited at 600/min, hits Postgres.
    const npRes = http.get(`${BASE_URL}/api/v1/users/me/notification-priority`, {
      headers, tags: { name: 'notif_priority' },
    });
    meTrend.add(npRes.timings.duration);
    const npOk = check(npRes, {
      'notif-priority 200': (r) => r.status === 200,
    });
    if (npOk) meSuccess.add(1);
    errorRate.add(!npOk);
  });

  // Realistic think-time: 1-3s between bursts.
  sleep(1 + Math.random() * 2);
}

export function teardown() {
  console.log(`[teardown] load test complete`);
}
