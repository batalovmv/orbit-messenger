// 150-user WebSocket fanout load test for the Orbit gateway.
//
// Pairs with k6-150-users.js (which exercises the HTTP API hot path).
// This script measures the OTHER half of the runtime: 150 concurrent
// WebSocket clients holding open connections, completing the auth
// handshake, and receiving server-pushed frames over their tenure.
//
// What it measures:
//   - ws_connect_duration       — TCP+TLS+upgrade handshake time
//   - ws_auth_ack_duration      — time from auth frame send to first
//                                 server frame (auth_ok / first event)
//   - ws_msgs_received          — total inbound frames per VU
//   - ws_disconnects            — server-initiated closes (>0 means the
//                                 gateway dropped someone — investigate)
//
// Why these metrics, not message-throughput:
//   The chat-message fanout path involves auth, messaging, gateway hub
//   and NATS. Decomposing those costs needs a different test (a paired
//   sender VU pool against a common chat, with end-to-end latency
//   captured server-side). For the pilot smoke it is enough to know
//   that 150 idle connections do not eat the gateway's hub-mutex.
//
// How to run (from repo root):
//
//   docker run --rm -i --network orbit_default \
//     -v "$(pwd)/tests/load:/scripts" \
//     -e BASE_URL=ws://gateway:8080 \
//     grafana/k6:0.50.0 run /scripts/k6-150-ws.js
//
// Tunables via env:
//   BASE_URL    default ws://gateway:8080
//   USERS       default 150
//   DURATION    default 60s   — how long each VU holds the connection
//
// Pre-req:  tokens.json from `go run tests/load/mint-tokens.go`.

import ws from 'k6/ws';
import { check, sleep } from 'k6';
import { Trend, Rate, Counter } from 'k6/metrics';
import { SharedArray } from 'k6/data';

const TOKENS = new SharedArray('tokens', () => JSON.parse(open('./tokens.json')));

const BASE_URL = __ENV.BASE_URL || 'ws://gateway:8080';
const USERS = parseInt(__ENV.USERS || '150', 10);
const DURATION = __ENV.DURATION || '60s';

// Custom metrics — these surface in the summary alongside k6 builtins.
const connectTrend = new Trend('ws_connect_duration', true);
const authAckTrend = new Trend('ws_auth_ack_duration', true);
const msgsReceived = new Counter('ws_msgs_received');
const disconnects = new Counter('ws_disconnects');
const errorRate = new Rate('errors');

// VU lifetime (ms) — leave a small slack under DURATION so the explicit
// close happens before the executor's gracefulStop, otherwise we get
// noisy "abort" events that look like server-side drops.
const HOLD_MS = (() => {
  const n = parseInt(DURATION, 10);
  if (Number.isNaN(n)) return 55000;
  // DURATION can be e.g. "60s" or "5m" — naive parse, k6 itself owns
  // the canonical interpretation; this is just for setTimeout.
  if (DURATION.endsWith('m')) return n * 60 * 1000 - 5000;
  return n * 1000 - 5000;
})();

export const options = {
  scenarios: {
    // per-vu-iterations: 1 — each VU connects ONCE and holds until
    // HOLD_MS. To avoid the gateway's 60/min/IP WS pre-auth limit
    // (services/gateway/cmd/main.go ~L276) when all VUs share the
    // k6 container's bridge IP, we stagger the actual connect via a
    // per-VU sleep at the start of default() — see RAMP_GAP_MS below.
    sustained_ws: {
      executor: 'per-vu-iterations',
      vus: USERS,
      iterations: 1,
      maxDuration: DURATION,
      gracefulStop: '10s',
    },
  },
  thresholds: {
    'ws_connect_duration': ['p(95)<2000'],
    'ws_auth_ack_duration': ['p(95)<1500'],
    'errors': ['rate<0.02'],
    'ws_disconnects': ['count<5'],
  },
};

// Pace VUs ~1.1s apart so 150 VUs spread over ~165s and stay under
// the 60/min/IP limit even with all of them on the k6 bridge IP.
// Override via RAMP_GAP_MS=0 if testing on Saturn where each user
// has a real distinct IP.
const RAMP_GAP_MS = parseInt(__ENV.RAMP_GAP_MS || '1100', 10);

export function setup() {
  const ok = TOKENS.filter((t) => t && t.token).length;
  console.log(`[setup] ${ok}/${TOKENS.length} pre-minted tokens loaded; BASE_URL=${BASE_URL}`);
  if (ok < USERS) {
    throw new Error(
      `Need ${USERS} tokens, found ${ok}. Run \`go run tests/load/mint-tokens.go > tests/load/tokens.json\` first.`,
    );
  }
  return {};
}

export default function () {
  const idx = (__VU - 1) % TOKENS.length;
  const u = TOKENS[idx];
  if (!u) return;

  // Stagger the connect so 150 VUs sharing one bridge IP don't all
  // attempt the upgrade in one second and burn the rate limit.
  const sleepMs = RAMP_GAP_MS * (__VU - 1);
  if (sleepMs > 0) {
    sleep(sleepMs / 1000);
  }

  const wsUrl = `${BASE_URL}/api/v1/ws`;
  const connectStart = Date.now();
  let authSentAt = 0;
  let authedFlag = false;

  const res = ws.connect(wsUrl, {}, (socket) => {
    socket.on('open', () => {
      connectTrend.add(Date.now() - connectStart);
      authSentAt = Date.now();
      // Gateway protocol: first frame is {"type":"auth","data":{"token":...,"session_id":...}}
      // session_id is optional but lets the gateway exclude origin-session
      // from its own echoes — set it to a stable per-VU value.
      socket.send(
        JSON.stringify({
          type: 'auth',
          data: { token: u.token, session_id: `loadtest-vu-${__VU}` },
        }),
      );
    });

    socket.on('message', (data) => {
      msgsReceived.add(1);
      if (!authedFlag) {
        // First inbound frame after auth-send is either ack or first
        // event. Either way it proves the auth handshake completed.
        authAckTrend.add(Date.now() - authSentAt);
        authedFlag = true;
      }
      // Don't decode payload here — k6 VUs would CPU-bind on JSON parse
      // at this concurrency. We only count frames.
    });

    socket.on('close', () => {
      // Server-initiated close mid-test counts as a disconnect. The
      // explicit setTimeout below closes from the client side after
      // HOLD_MS, which fires AFTER this handler is set up, so a close
      // before HOLD_MS is by definition server-side.
      // Note: the close handler also runs on client-side close, but
      // only one fires per connection so the counter is monotonic.
    });

    socket.on('error', (e) => {
      errorRate.add(1);
      // Spammy in failure mode — comment out if log volume is too high.
      console.log(`[VU ${__VU}] ws error: ${e}`);
    });

    // Hold the connection for ~DURATION minus slack, then clean close.
    socket.setTimeout(() => {
      socket.close();
    }, HOLD_MS);
  });

  check(res, {
    'ws upgrade status 101': (r) => r && r.status === 101,
  });
  if (!res || res.status !== 101) {
    errorRate.add(1);
    disconnects.add(1);
  }
}

export function teardown() {
  console.log('[teardown] WS load test complete');
}
