import { defineConfig, devices } from '@playwright/test';

// Calls e2e harness — drives REAL Chromium against the live dev stack
// at http://localhost:3000. Separate from web/tests/playwright which
// runs against a mocked build with stubbed WS — calls need real
// gateway, real NATS, real coturn.
//
// Pre-req: docker compose up -d (full stack) AND `cd web && npm run dev`
// (or have the web container serving on :3000). The harness assumes
// the seeded `test@orbit.local` and `user2@orbit.local` users exist
// (see tests/load/seed-loadtest-users.sql for the password hash) and
// that they share a `direct` chat. If the chat doesn't exist the test
// will skip with a clear message — operator runs `Create DM` from the
// app first or seeds via SQL.

export default defineConfig({
  testDir: '.',
  testMatch: /.*\.spec\.ts$/,
  // WebRTC handshakes can take 5-15 s on a cold stack; pad generously.
  timeout: 90_000,
  expect: { timeout: 15_000 },
  retries: process.env.CI ? 1 : 0,
  workers: 1, // serialise — both browsers share the same backend state
  reporter: [['list'], ['html', { outputFolder: 'playwright-report', open: 'never' }]],
  use: {
    baseURL: process.env.ORBIT_URL || 'http://localhost:3000',
    trace: 'retain-on-failure',
    video: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chromium-fake-media',
      use: {
        ...devices['Desktop Chrome'],
        // Permissions for getUserMedia without a browser prompt.
        permissions: ['microphone', 'camera'],
        launchOptions: {
          args: [
            // Auto-grant fake camera/mic without UI prompt.
            '--use-fake-ui-for-media-stream',
            // Synthesise a green-square + 1kHz tone instead of using
            // real hardware. Required for headless and for repeatable
            // RTCStats — the fake stream emits a deterministic frame
            // pattern that we can probe in the test.
            '--use-fake-device-for-media-stream',
            // Some Chromium versions still gate autoplay even with the
            // flags above; this removes the user-gesture requirement
            // so the SDK's playback step does not stall waiting for a
            // click that the test cannot deliver.
            '--autoplay-policy=no-user-gesture-required',
            // Disable network conditions overrides that would muddy
            // ICE candidate timing.
            '--disable-features=PrivacySandboxAdsApis',
          ],
        },
      },
    },
  ],
});
