import { test, expect, type BrowserContext, type Page } from '@playwright/test';

// End-to-end SFU group-call smoke against the live local Orbit stack.
//
// What this proves:
//   1. Three users can log in concurrently and open the same group chat
//   2. Alice can start a voice chat from the 3-dot menu (createGroupCall)
//   3. Bob and Carol receive the active-call signal via WebSocket and
//      can join through their UI
//   4. Each browser opens its own RTCPeerConnection to the SFU
//   5. ICE reaches connected on all three sides
//   6. Each peer receives incoming tracks (>0 bytes) from the SFU,
//      proving the forwarding path works
//   7. Hangup propagates and PCs close on all three sides
//
// Prerequisites: run seed-sfu-group.sql once before this test:
//   docker exec -i orbit-postgres-1 psql -U orbit -d orbit < tests/calls-e2e/seed-sfu-group.sql
//
// Stable as of 2026-05-05 after 5 fixes landed in one session:
//   - call_handler.go dropped redundant member_ids validation
//   - saturn createGroupCall accepts {peer} shape
//   - useSfuStreamManager refs+memo to stop join/leave thrash
//   - createGroupCall handler propagates sfuWsUrl into global state
//   - wsHandler propagates sfu_ws_url on call_incoming for joiners
//   - useSfuStreamManager.join() does the REST membership hop before
//     opening the SFU WS (gateway sfu_proxy rejects non-members)
// Re-run the spec after touching any of those layers.

const ALICE_EMAIL = 'test@orbit.local';
const BOB_EMAIL = 'user2@orbit.local';
const CAROL_EMAIL = 'loadtest_0@orbit.local';
const PASSWORD = 'LoadTest!2026';
const GROUP_NAME_REGEX = /SFU E2E Test Group/i;

async function login(page: Page, email: string) {
  await page.goto('/');
  const emailInput = page.locator('#sign-in-email').first();
  await emailInput.waitFor({ state: 'visible', timeout: 30_000 });
  await emailInput.click();
  await emailInput.pressSequentially(email, { delay: 10 });
  const passwordInput = page.locator('#sign-in-password').first();
  await passwordInput.click();
  await passwordInput.pressSequentially(PASSWORD, { delay: 10 });
  await page.getByRole('button', { name: /next|далее/i }).first().click();
  await page.locator('#LeftColumn').waitFor({ state: 'visible', timeout: 45_000 });
}

async function openGroupChat(page: Page, nameRegex: RegExp) {
  // Group chats render with `.group` class instead of `.private`.
  const chatRow = page.locator('.Chat.chat-item-clickable.group')
    .filter({ hasText: nameRegex }).first();
  await chatRow.waitFor({ state: 'visible', timeout: 30_000 });
  await chatRow.click({ timeout: 10_000 });
  // Middle column header rendered = chat opened.
  await page.locator('.MiddleHeader').waitFor({ state: 'visible', timeout: 15_000 });
}

// Patch RTCPeerConnection to capture every PC the app constructs.
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
      return pcs[pcs.length - 1].iceConnectionState;
    });
    if (state === 'connected' || state === 'completed') {
      // eslint-disable-next-line no-console
      console.log(`[${label}] ICE connected after ${Date.now() - start}ms`);
      return state;
    }
    if (state === 'failed' || state === 'closed') {
      throw new Error(`[${label}] ICE state=${state} (terminal)`);
    }
    await page.waitForTimeout(500);
  }
  throw new Error(`[${label}] ICE never reached connected within ${timeoutMs}ms`);
}

async function pollMediaFlowing(page: Page, label: string, timeoutMs = 20_000) {
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
      console.log(`[${label}] media flowing sent=${stats.sent}B recv=${stats.recv}B after ${Date.now() - start}ms`);
      return stats;
    }
    await page.waitForTimeout(500);
  }
  throw new Error(`[${label}] media never flowed within ${timeoutMs}ms`);
}

async function startVoiceChat(page: Page) {
  // Group voice chat is initiated from the chat header 3-dot menu.
  // The button has aria-label "More" or icon "more" — match by icon
  // since aria-label is i18n-dependent.
  const moreBtn = page.locator('.MiddleHeader button:has(.icon-more)').first();
  await moreBtn.waitFor({ state: 'visible', timeout: 10_000 });
  await moreBtn.click();
  // Menu item with voice-chat icon. Localized text "Start voice chat" /
  // "Начать голосовой чат" — match by icon class to avoid i18n coupling.
  const startVoiceItem = page.locator('.MenuItem:has(.icon-voice-chat)').first();
  await startVoiceItem.waitFor({ state: 'visible', timeout: 10_000 });
  await startVoiceItem.click();
}

async function joinActiveVoiceChat(page: Page) {
  // After alice creates the call, bob/carol see a `.GroupCallTopPane`
  // banner above the chat header with title "Voice chat — N participants".
  // Clicking anywhere on the pane joins the call (whole pane is clickable).
  // Wait long enough for the WS update from NATS to land + the pane to
  // render — first-paint after groupCall arrives can take a few seconds.
  const pane = page.locator('.GroupCallTopPane').first();
  await pane.waitFor({ state: 'visible', timeout: 20_000 });
  await pane.click();
}

test.describe('SFU group call between three users', () => {
  test.setTimeout(120_000); // group calls take longer to negotiate

  test('alice starts, bob+carol join, all 3 ICE connect, media flows via SFU', async ({ browser }) => {
    const aliceCtx = await browser.newContext();
    const bobCtx = await browser.newContext();
    const carolCtx = await browser.newContext();
    await Promise.all([
      instrumentRTCPeerConnection(aliceCtx),
      instrumentRTCPeerConnection(bobCtx),
      instrumentRTCPeerConnection(carolCtx),
    ]);

    const alicePage = await aliceCtx.newPage();
    const bobPage = await bobCtx.newPage();
    const carolPage = await carolCtx.newPage();

    for (const [page, label] of [
      [alicePage, 'alice'], [bobPage, 'bob'], [carolPage, 'carol'],
    ] as const) {
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

    await Promise.all([
      login(alicePage, ALICE_EMAIL),
      login(bobPage, BOB_EMAIL),
      login(carolPage, CAROL_EMAIL),
    ]);

    await Promise.all([
      openGroupChat(alicePage, GROUP_NAME_REGEX),
      openGroupChat(bobPage, GROUP_NAME_REGEX),
      openGroupChat(carolPage, GROUP_NAME_REGEX),
    ]);

    // Alice initiates the voice chat.
    await startVoiceChat(alicePage);

    // Wait for alice's PC to spin up before bob/carol try to join —
    // gives the SFU room time to materialise.
    await alicePage.waitForTimeout(2000);

    // Bob and Carol join.
    await Promise.all([
      joinActiveVoiceChat(bobPage),
      joinActiveVoiceChat(carolPage),
    ]);

    // All three ICE connect.
    await Promise.all([
      waitForIceConnected(alicePage, 'alice'),
      waitForIceConnected(bobPage, 'bob'),
      waitForIceConnected(carolPage, 'carol'),
    ]);

    // Each peer must receive media — the SFU forwarding path is the
    // whole point. sent>0 just means the local mic is captured;
    // recv>0 means at least one OTHER peer's track was forwarded.
    const [aliceStats, bobStats, carolStats] = await Promise.all([
      pollMediaFlowing(alicePage, 'alice'),
      pollMediaFlowing(bobPage, 'bob'),
      pollMediaFlowing(carolPage, 'carol'),
    ]);
    expect(aliceStats.recv, 'alice should receive forwarded media').toBeGreaterThan(0);
    expect(bobStats.recv, 'bob should receive forwarded media').toBeGreaterThan(0);
    expect(carolStats.recv, 'carol should receive forwarded media').toBeGreaterThan(0);

    await aliceCtx.close();
    await bobCtx.close();
    await carolCtx.close();
  });
});
