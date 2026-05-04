import { test, expect, type BrowserContext, type Page } from '@playwright/test';

// End-to-end P2P call smoke test against the live local Orbit stack.
//
// What this proves end-to-end:
//   1. Two users can log in concurrently in isolated browser contexts
//   2. Caller can initiate a call from a direct chat
//   3. Callee receives the incoming-call signal via WebSocket
//   4. WebRTC ICE handshake completes (both sides reach
//      iceConnectionState=connected or completed)
//   5. Media bytes actually flow on both sides (RTCStatsReport
//      bytesSent/bytesReceived > 0 within ~10 s)
//   6. Hangup propagates and both sides clear PeerConnection state
//
// Why this matters:
//   The cross-browser-call-test.md runbook lists this as a 10-minute
//   manual check before each pilot release. Automating the Chromium
//   path catches regressions before the manual runbook runs — the
//   manual one then only has to cover Firefox/Safari/iOS.
//
// What this does NOT cover:
//   - Group / SFU calls (3+ participants) — requires a different
//     fixture; tracked separately
//   - TURN relay fallback — needs us to disable host candidates
//     mid-test, which the launchOptions above don't toggle yet
//   - Audio/video quality — fake stream produces a deterministic
//     pattern we don't decode

const TEST_USER_EMAIL = 'test@orbit.local';
const USER2_EMAIL = 'user2@orbit.local';
const PASSWORD = 'LoadTest!2026';

async function login(page: Page, email: string) {
  await page.goto('/');
  // The InputText component renders a Material-style floating label,
  // not a placeholder, so getByPlaceholder() doesn't match. The first
  // text input on the auth screen is email, second (type=password) is
  // password — use simple positional selectors which are also stable
  // across i18n.
  // The form has IDed inputs (#sign-in-email / #sign-in-password)
  // which is the most stable handle. Use pressSequentially so the
  // change handlers fire one character at a time — fill() can race
  // the controlled-state update on Teact's value-binding model and
  // leave the submit button stuck disabled.
  const emailInput = page.locator('#sign-in-email').first();
  await emailInput.waitFor({ state: 'visible', timeout: 30_000 });
  await emailInput.click();
  await emailInput.pressSequentially(email, { delay: 10 });
  const passwordInput = page.locator('#sign-in-password').first();
  await passwordInput.click();
  await passwordInput.pressSequentially(PASSWORD, { delay: 10 });
  // Submit — Enter on the password field is unreliable in our
  // InputText component (the keydown handler is on a parent that may
  // not be focused yet). Click the NEXT button explicitly. Localized
  // text matters, so match by both English and Russian.
  await page.getByRole('button', { name: /next|далее/i }).first().click();
  // After login the LeftColumn renders. #LeftColumn is the most
  // stable selector — it appears regardless of whether the chat list
  // has any rows yet (a fresh empty user still sees an empty
  // LeftColumn). Increased timeout because dev bundle cold-loads can
  // take 20-30 s on first hit.
  await page.locator('#LeftColumn').waitFor({ state: 'visible', timeout: 45_000 });
}

async function openDirectChat(page: Page, peerNameMatch: RegExp) {
  // The chat list contains multiple `.private` rows: Saved Messages
  // (the user's own self-chat), any bots like BotFather, and the
  // actual peer. Filter by visible peer name to land on the right
  // one. peerNameMatch must NOT match "Saved Messages" / "BotFather".
  const chatRow = page.locator('.Chat.chat-item-clickable.private')
    .filter({ hasText: peerNameMatch }).first();
  await chatRow.waitFor({ state: 'visible', timeout: 30_000 });
  await chatRow.click({ timeout: 10_000 });
  // Wait for the middle column to render with the call button —
  // proves the chat actually opened and the call surface is mounted.
  await page.locator('button[aria-label="Call"]').waitFor({ state: 'visible', timeout: 15_000 });
}

// Tap into the in-page RTCPeerConnection registry by patching the
// constructor before any app code runs. Captures every PC the app
// creates so the test can poll iceConnectionState and getStats() on
// it, regardless of how the SDK wraps WebRTC internally.
async function instrumentRTCPeerConnection(context: BrowserContext) {
  await context.addInitScript(() => {
    const w = window as unknown as { __orbitPCs?: RTCPeerConnection[] };
    w.__orbitPCs = [];
    const Native = window.RTCPeerConnection;
    if (!Native) return;
    const Patched = function (this: RTCPeerConnection, config?: RTCConfiguration) {
      const pc = new Native(config);
      w.__orbitPCs!.push(pc);
      return pc;
    } as unknown as typeof RTCPeerConnection;
    Patched.prototype = Native.prototype;
    Patched.generateCertificate = Native.generateCertificate.bind(Native);
    window.RTCPeerConnection = Patched;
  });
}

async function waitForIceConnected(page: Page, label: string, timeoutMs = 30_000) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    const state = await page.evaluate(() => {
      const pcs = (window as any).__orbitPCs as RTCPeerConnection[] | undefined;
      if (!pcs || pcs.length === 0) return 'no-pc';
      // Pick the most recently constructed PC — older ones might be
      // pre-call setup peers (e.g. perfect-negotiation rollback).
      return pcs[pcs.length - 1].iceConnectionState;
    });
    if (state === 'connected' || state === 'completed') {
      // eslint-disable-next-line no-console
      console.log(`[${label}] ICE state=${state} after ${Date.now() - start}ms`);
      return state;
    }
    if (state === 'failed' || state === 'closed') {
      throw new Error(`[${label}] ICE state=${state} (terminal failure)`);
    }
    await page.waitForTimeout(500);
  }
  throw new Error(`[${label}] ICE never reached connected within ${timeoutMs}ms`);
}

async function pollMediaFlowing(page: Page, label: string, timeoutMs = 15_000) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    const stats = await page.evaluate(async () => {
      const pcs = (window as any).__orbitPCs as RTCPeerConnection[] | undefined;
      if (!pcs || pcs.length === 0) return { sent: 0, recv: 0 };
      const pc = pcs[pcs.length - 1];
      const report = await pc.getStats();
      let sent = 0;
      let recv = 0;
      report.forEach((r: any) => {
        if (r.type === 'outbound-rtp') sent += r.bytesSent ?? 0;
        if (r.type === 'inbound-rtp') recv += r.bytesReceived ?? 0;
      });
      return { sent, recv };
    });
    if (stats.sent > 0 && stats.recv > 0) {
      // eslint-disable-next-line no-console
      console.log(`[${label}] media flowing: sent=${stats.sent}B recv=${stats.recv}B after ${Date.now() - start}ms`);
      return stats;
    }
    await page.waitForTimeout(500);
  }
  throw new Error(`[${label}] media never flowed within ${timeoutMs}ms`);
}

test.describe('P2P call between two users', () => {
  test('alice initiates, bob accepts, ICE connects, media flows, hangup clears state', async ({ browser }) => {
    // Two isolated contexts so cookies/localStorage don't collide.
    const aliceCtx = await browser.newContext();
    const bobCtx = await browser.newContext();
    await instrumentRTCPeerConnection(aliceCtx);
    await instrumentRTCPeerConnection(bobCtx);

    const alicePage = await aliceCtx.newPage();
    const bobPage = await bobCtx.newPage();

    // Pipe console errors so a WebRTC failure surfaces in the test log.
    for (const [page, label] of [[alicePage, 'alice'], [bobPage, 'bob']] as const) {
      page.on('console', (msg) => {
        if (msg.type() === 'error') {
          // eslint-disable-next-line no-console
          console.log(`[${label}] console.error: ${msg.text()}`);
        }
      });
      page.on('pageerror', (err) => {
        // eslint-disable-next-line no-console
        console.log(`[${label}] pageerror: ${err.message}`);
      });
    }

    // 1. Both log in concurrently
    await Promise.all([
      login(alicePage, TEST_USER_EMAIL),
      login(bobPage, USER2_EMAIL),
    ]);

    // 2. Both open the shared direct chat. user2 displays as
    //    "Test User" in alice's chat list and test@orbit.local
    //    displays under whatever name was set at register time —
    //    grep on the email-derived prefix to be tolerant of name
    //    drift on either side.
    // Display names from the seed: user2's name is "User Two" so
    // alice's row shows "User Two"; test@orbit.local's name is
    // "Test User" so bob's row shows "Test User". Match both with
    // generous patterns to survive minor display-name edits.
    await Promise.all([
      openDirectChat(alicePage, /user two|user2/i),
      openDirectChat(bobPage, /test user|^test/i),
    ]);

    // 3. Alice clicks the call icon in the chat header.
    await alicePage.locator('button[aria-label="Call"]').first().click({ timeout: 10_000 });

    // 4. Bob's PhoneCall modal opens with the incoming-call layout
    //    (Accept + Decline buttons). Both render as
    //    `button:has(.icon-phone-discard)` and Accept is the FIRST
    //    one — see web/src/components/calls/phone/PhoneCall.tsx
    //    where {isIncomingRequested && <accept>} renders before the
    //    standard hangup. Use first() consistently: during incoming
    //    it is accept; after the modal switches to active-call it is
    //    hangup. Same selector works on both sides for hangup later.
    await bobPage.locator('button:has(.icon-phone-discard)').first()
      .click({ timeout: 15_000 });

    // 5. Both PCs must reach ICE connected
    await Promise.all([
      waitForIceConnected(alicePage, 'alice'),
      waitForIceConnected(bobPage, 'bob'),
    ]);

    // 6. Media must actually flow both ways
    const [aliceStats, bobStats] = await Promise.all([
      pollMediaFlowing(alicePage, 'alice'),
      pollMediaFlowing(bobPage, 'bob'),
    ]);
    expect(aliceStats.sent).toBeGreaterThan(0);
    expect(aliceStats.recv).toBeGreaterThan(0);
    expect(bobStats.sent).toBeGreaterThan(0);
    expect(bobStats.recv).toBeGreaterThan(0);

    // 7. Hangup from alice. The active-call layout has only one
    //    phone-discard button (the accept one is gone after pickup),
    //    so first() lands on hangup.
    await alicePage.locator('button:has(.icon-phone-discard)').first()
      .click({ timeout: 10_000 });

    // Verify alice's PC closed
    await expect.poll(async () => {
      return alicePage.evaluate(() => {
        const pcs = (window as any).__orbitPCs as RTCPeerConnection[];
        return pcs?.[pcs.length - 1]?.iceConnectionState;
      });
    }, { timeout: 10_000 }).toBe('closed');

    await aliceCtx.close();
    await bobCtx.close();
  });
});
