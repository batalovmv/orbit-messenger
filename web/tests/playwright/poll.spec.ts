import { type Browser, expect, test } from '@playwright/test';

import { FAKE_WEBSOCKET_SCRIPT } from './fixtures/fake-websocket';
import { MockServerState } from './fixtures/mock-state';
import { BASE_URL, setupRoutes, waitForReady } from './fixtures/setup';

async function createPollPage(browser: Browser) {
  const state = new MockServerState();
  const pollMessage = state.buildPollMessage(state.alice.id, {
    question: 'Quarterly planning',
    answers: ['Approve', 'Discuss later'],
    totalVoters: 0,
  });
  state.seedMessage(pollMessage);

  const context = await browser.newContext();
  await context.addInitScript(FAKE_WEBSOCKET_SCRIPT);
  setupRoutes(context, state, state.alice.id);

  const page = await context.newPage();
  state.allPages.push(page);

  await page.goto(`${BASE_URL}/#${state.chatId}`);
  await waitForReady(page);

  return { context, page, state, pollMessage };
}

test.describe('Poll messaging', () => {
  test('renders open poll state from history', async ({ browser }) => {
    const { context, page } = await createPollPage(browser);

    try {
      const poll = page.locator('.Poll:visible').filter({ hasText: 'Quarterly planning' }).first();
      await expect(poll).toBeVisible({ timeout: 5000 });
      await expect(poll.locator('.poll-question')).toContainText('Quarterly planning');
      await expect(poll.locator('.Radio')).toHaveCount(2);
      await expect(poll.locator('.poll-voters-count')).toContainText('No votes');
    } finally {
      await context.close();
    }
  });

  test('keeps poll content after partial poll_closed update', async ({ browser }) => {
    const { context, page, state, pollMessage } = await createPollPage(browser);
    let dialogText: string | undefined;

    page.on('dialog', async (dialog) => {
      dialogText = dialog.message();
      await dialog.dismiss();
    });

    try {
      const poll = page.locator('.Poll:visible').filter({ hasText: 'Quarterly planning' }).first();
      await expect(poll).toBeVisible({ timeout: 5000 });

      await state.pushWsToPage(page, {
        type: 'poll_closed',
        data: {
          poll_id: pollMessage.poll!.id,
        },
      });

      await expect.poll(async () => page.evaluate((pollId) => {
        const global = (window as any).getGlobal?.();
        return global?.messages?.pollById?.[pollId]?.summary?.closed ?? undefined;
      }, pollMessage.poll!.id)).toBe(true);

      await expect(poll).toHaveClass(/is-closed/, { timeout: 5000 });
      await expect(poll.locator('.poll-results')).toBeVisible({ timeout: 5000 });
      await expect(poll.locator('.PollOption')).toHaveCount(2);
      await expect(poll.locator('.poll-option-text').first()).toContainText('Approve');
      await expect(poll.locator('.poll-option-text').nth(1)).toContainText('Discuss later');

      await page.waitForTimeout(500);
      expect(dialogText).toBeUndefined();
    } finally {
      await context.close();
    }
  });
});
