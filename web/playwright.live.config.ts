import { devices, type PlaywrightTestConfig } from '@playwright/test';

const config: PlaywrightTestConfig = {
  testDir: 'tests/playwright-live',
  timeout: process.env.CI ? 5 * 60 * 1000 : 2 * 60 * 1000,
  expect: {
    timeout: 15 * 1000,
  },
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  fullyParallel: false,
  use: {
    baseURL: process.env.PLAYWRIGHT_LIVE_BASE_URL || 'http://localhost:3000/',
    video: 'retain-on-failure',
    trace: 'on-first-retry',
    serviceWorkers: 'block',
  },
  reporter: [['html', { outputFolder: 'playwright-report-live' }]],
  projects: [
    {
      name: 'chromium-live',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
};

export default config;
