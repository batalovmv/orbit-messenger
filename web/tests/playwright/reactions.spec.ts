import { buildWsMessageUpdated, buildWsNewMessage } from './fixtures/mock-state';
import { expect, test } from './fixtures/setup';

function messageByText(page: import('@playwright/test').Page, text: string) {
  return page.locator('.Message').filter({ hasText: text });
}

test.describe('Reactions', () => {
  test('keeps rendered reaction badge after full message update without reactions payload', async ({
    alicePage,
    mockState,
  }) => {
    const text = `reaction-${Date.now()}`;
    const message = mockState.buildSaturnMessage(mockState.bob.id, text);
    message.reactions = [{
      emoji: '❤️',
      count: 1,
      user_ids: [mockState.alice.id],
    }];
    mockState.seedMessage(message);

    await mockState.pushWsToPage(alicePage, buildWsNewMessage(message));

    const renderedMessage = messageByText(alicePage, text);
    await expect(renderedMessage).toBeVisible({ timeout: 5000 });

    const reactionBadge = renderedMessage.locator('.message-reaction');
    await expect(reactionBadge).toBeVisible({ timeout: 5000 });
    await expect(reactionBadge.locator('.ReactionStaticEmoji .emoji-fallback')).toHaveText('❤️');
    await expect(reactionBadge.locator('.Avatar')).toHaveCount(1);

    const updatedMessage = {
      ...message,
      is_edited: true,
      edited_at: new Date().toISOString(),
    };
    delete updatedMessage.reactions;

    await mockState.pushWsToPage(alicePage, buildWsMessageUpdated(updatedMessage));

    await expect(renderedMessage).toBeVisible({ timeout: 5000 });
    await expect(reactionBadge).toBeVisible({ timeout: 5000 });
    await expect(reactionBadge.locator('.ReactionStaticEmoji .emoji-fallback')).toHaveText('❤️');
    await expect(reactionBadge.locator('.Avatar')).toHaveCount(1);
  });
});
