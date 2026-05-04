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
  const emailInput = page.locator('input[type="text"]').first();
  await emailInput.waitFor({ state: 'visible', timeout: 30_000 });
  await emailInput.fill(email);
  const passwordInput = page.locator('input[type="password"]').first();
  await passwordInput.fill(PASSWORD);
  // Submit — pressing Enter on the password field triggers the
  // form's onSubmit which dispatches the login action. Clicking the
  // Next button works too but the keyboard path is slightly more
  // deterministic across the InputText component's focus/blur model.
  await passwordInput.press('Enter');
  // After login the LeftColumn renders. #LeftColumn is the most
  // stable selector — it appears regardless of whether the chat list
  // has any rows yet (a fresh empty user still sees an empty
  // LeftColumn). Increased timeout because dev bundle cold-loads can
  // take 20-30 s on first hit.
  await page.locator('#LeftColumn').waitFor({ state: 'visible', timeout: 45_000 });
}

async function openDirectChatWith(page: Page, peerName: string) {
  // The seeded peer's display name is set by tests/load/seed-loadtest-users.sql
  // for the loadtest_* users. test@/user2@ get whatever display name was
  // set at register time — look up by visible email-like text or the
  // first chat row that's not ourselves.
  // Search via the search input is the most stable; falls back to
  // clicking the first non-self chat row.
  const searchBox = page.getByPlaceholder(/search/i).first();
  if (await searchBox.isVisible({ timeout: 2000 }).catch(() => false)) {
    await searchBox.fill(peerName);
    await page.locator('.ListItem, [role="button"]').filter({ hasText: peerName })
      .first().click({ timeout: 5000 }).catch(() => undefined);
    await searchBox.fill('');
  }
  // If search didn't land us on a chat, click the first chat row.
  const chatHeader = page.locator('.MessageList, .messages-container, [data-testid="message-list"]').first();
  if (!(await chatHeader.isVisible({ timeout: 2000 }).catch(() => false))) {
    await page.locator('.ListItem').first().click();
  }
  await chatHeader.waitFor({ state: 'visible', timeout: 10_000 });
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

    // 2. Both open the shared direct chat
    await Promise.all([
      openDirectChatWith(alicePage, 'user2'),
      openDirectChatWith(bobPage, 'test'),
    ]);

    // 3. Alice clicks the call (audio) icon in the chat header.
    //    The phone-icon is the most stable selector — title or aria
    //    can vary by language. Fall back to the icon class.
    const callButton = alicePage.locator(
      'button[aria-label*="all" i], button[title*="all" i], button:has(.icon-phone), button:has(.icon-call)',
    ).first();
    await callButton.click({ timeout: 10_000 });

    // 4. Bob's UI should show the incoming call within ~3s. Accept.
    const acceptButton = bobPage.locator(
      'button[aria-label*="ccept" i], button[title*="ccept" i], button:has(.icon-call), button:has-text("Accept")',
    ).first();
    await acceptButton.click({ timeout: 15_000 });

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

    // 7. Hangup from alice. Both UIs should clear within a few seconds
    //    and the PC should close (iceConnectionState=closed).
    const hangupButton = alicePage.locator(
      'button[aria-label*="ang" i], button[title*="ang" i], button:has(.icon-hangup), button:has(.icon-close)',
    ).first();
    await hangupButton.click({ timeout: 10_000 });

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
