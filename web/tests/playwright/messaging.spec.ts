import { expect, test } from './fixtures/setup';

// Helper: type a message and send via Enter
async function sendMessage(page: import('@playwright/test').Page, text: string) {
  await page.locator('#editable-message-text').click();
  await page.keyboard.type(text);
  await page.keyboard.press('Enter');
}

// Helper: find a message by text content
function messageByText(page: import('@playwright/test').Page, text: string) {
  return page.locator('.Message').filter({ hasText: text });
}

// Helper: unique text per test
function uniqueText(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

test.describe('Messaging between two users', () => {
  test('1. Optimistic send: message appears with pending, then confirmed', async ({ alicePage, bobPage }) => {
    const text = uniqueText('optimistic');

    // Block POST so we can observe pending state
    let resolvePost!: () => void;
    const postBlocked = new Promise<void>((r) => {
      resolvePost = r;
    });

    await alicePage.route('**/api/v1/chats/*/messages', async (route) => {
      if (route.request().method() === 'POST') {
        // Wait for test to check pending state before responding
        await postBlocked;
        // Delegate to context-level handler which mutates state + broadcasts WS
        await route.fallback();
      } else {
        await route.fallback();
      }
    });

    await sendMessage(alicePage, text);

    // Assert: message appears immediately with pending icon
    const msg = messageByText(alicePage, text);
    await expect(msg).toBeVisible({ timeout: 3000 });
    await expect(msg.locator('.icon-message-pending')).toBeVisible({ timeout: 2000 });

    // Release the POST
    resolvePost();

    // Assert: pending icon gone, no failed icon
    await expect(msg.locator('.icon-message-pending')).not.toBeVisible({ timeout: 5000 });
    await expect(msg.locator('.icon-message-failed')).not.toBeVisible();

    // Assert: exactly ONE message with this text (dedup verified)
    await expect(messageByText(alicePage, text)).toHaveCount(1);
  });

  test('2. Receiver sees incoming message', async ({ alicePage, bobPage }) => {
    const text = uniqueText('receiver');

    await sendMessage(alicePage, text);

    // Wait for message to appear on Alice (via HTTP response)
    await expect(messageByText(alicePage, text)).toBeVisible({ timeout: 5000 });

    // Bob should see the message via WS broadcast (auto-pushed by mock server)
    const bobMsg = messageByText(bobPage, text);
    await expect(bobMsg).toBeVisible({ timeout: 5000 });

    // Alice's message should be "own"
    const aliceMsg = messageByText(alicePage, text);
    await expect(aliceMsg).toHaveClass(/\bown\b/);

    // Bob's message should NOT be "own"
    await expect(bobMsg).not.toHaveClass(/\bown\b/);
  });

  test('3. Bidirectional: both see correct own/other styling', async ({ alicePage, bobPage }) => {
    const aliceText = uniqueText('from-alice');
    const bobText = uniqueText('from-bob');

    // Alice sends
    await sendMessage(alicePage, aliceText);
    await expect(messageByText(alicePage, aliceText)).toBeVisible({ timeout: 5000 });
    await expect(messageByText(bobPage, aliceText)).toBeVisible({ timeout: 5000 });

    // Bob sends
    await sendMessage(bobPage, bobText);
    await expect(messageByText(bobPage, bobText)).toBeVisible({ timeout: 5000 });
    await expect(messageByText(alicePage, bobText)).toBeVisible({ timeout: 5000 });

    // Check own/other classes
    await expect(messageByText(alicePage, aliceText)).toHaveClass(/\bown\b/);
    await expect(messageByText(alicePage, bobText)).not.toHaveClass(/\bown\b/);
    await expect(messageByText(bobPage, bobText)).toHaveClass(/\bown\b/);
    await expect(messageByText(bobPage, aliceText)).not.toHaveClass(/\bown\b/);
  });

  test('4. Delete syncs between users', async ({ alicePage, bobPage }) => {
    const text = uniqueText('delete-me');

    // Alice sends message
    await sendMessage(alicePage, text);
    await expect(messageByText(alicePage, text)).toBeVisible({ timeout: 5000 });
    await expect(messageByText(bobPage, text)).toBeVisible({ timeout: 5000 });

    // Alice right-clicks on message
    const aliceMsg = messageByText(alicePage, text);
    await aliceMsg.click({ button: 'right' });

    // Click Delete in context menu
    const deleteItem = alicePage.locator('.MessageContextMenu .MenuItem').filter({
      has: alicePage.locator('.icon-delete'),
    });
    await deleteItem.waitFor({ state: 'visible', timeout: 3000 });
    await deleteItem.click();

    // Confirm in delete modal — find the danger/confirm button in any open modal
    const confirmBtn = alicePage.locator('.Modal button.confirm-dialog-button').first();
    await confirmBtn.waitFor({ state: 'visible', timeout: 5000 });
    await confirmBtn.click();

    // Assert: message disappears from Alice
    await expect(messageByText(alicePage, text)).toHaveCount(0, { timeout: 5000 });

    // Assert: message disappears from Bob (via WS message_deleted broadcast)
    await expect(messageByText(bobPage, text)).toHaveCount(0, { timeout: 5000 });
  });

  test('5. Edit syncs between users', async ({ alicePage, bobPage }) => {
    const originalText = uniqueText('original');
    const editedText = uniqueText('edited');

    // Alice sends
    await sendMessage(alicePage, originalText);
    await expect(messageByText(alicePage, originalText)).toBeVisible({ timeout: 5000 });
    await expect(messageByText(bobPage, originalText)).toBeVisible({ timeout: 5000 });

    // Alice right-clicks to edit
    const aliceMsg = messageByText(alicePage, originalText);
    await aliceMsg.click({ button: 'right' });

    // Click Edit in context menu
    const editItem = alicePage.locator('.MessageContextMenu .MenuItem').filter({
      has: alicePage.locator('.icon-edit'),
    });
    await editItem.waitFor({ state: 'visible', timeout: 3000 });
    await editItem.click();

    // Clear existing text and type new text
    await alicePage.locator('#editable-message-text').click();
    await alicePage.keyboard.press('Control+A');
    await alicePage.keyboard.type(editedText);
    await alicePage.keyboard.press('Enter');

    // Assert: Alice sees edited text (original text should be replaced)
    await expect(messageByText(alicePage, editedText)).toBeVisible({ timeout: 10000 });

    // Assert: Bob sees edited text (via WS message_updated broadcast)
    await expect(messageByText(bobPage, editedText)).toBeVisible({ timeout: 10000 });
  });

  test('6. Failed send shows error icon', async ({ alicePage }) => {
    const text = uniqueText('will-fail');

    // Override POST to return 500
    await alicePage.route('**/api/v1/chats/*/messages', async (route) => {
      if (route.request().method() === 'POST') {
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'server_error', message: 'Internal error', status: 500 }),
        });
      } else {
        await route.fallback();
      }
    });

    await sendMessage(alicePage, text);

    // Assert: message appears with failed icon
    const msg = messageByText(alicePage, text);
    await expect(msg).toBeVisible({ timeout: 5000 });
    await expect(msg.locator('.icon-message-failed')).toBeVisible({ timeout: 5000 });
  });

  test('7. Message ordering is preserved on both sides', async ({ alicePage, bobPage }) => {
    const texts = [uniqueText('first'), uniqueText('second'), uniqueText('third')];

    // Send 3 messages sequentially
    for (const text of texts) {
      await sendMessage(alicePage, text);
      // Wait for each to appear before sending next
      await expect(messageByText(alicePage, text)).toBeVisible({ timeout: 5000 });
    }

    // Wait for all to appear on Bob
    for (const text of texts) {
      await expect(messageByText(bobPage, text)).toBeVisible({ timeout: 5000 });
    }

    // Verify order on Alice: all 3 messages should exist in DOM order
    const aliceMessages = alicePage.locator('.Message').filter({ hasText: /first|second|third/ });
    const aliceTexts = await aliceMessages.allTextContents();
    const aliceOrder = texts.filter((t) => aliceTexts.some((at) => at.includes(t)));
    expect(aliceOrder).toEqual(texts);

    // Verify order on Bob
    const bobMessages = bobPage.locator('.Message').filter({ hasText: /first|second|third/ });
    const bobTexts = await bobMessages.allTextContents();
    const bobOrder = texts.filter((t) => bobTexts.some((bt) => bt.includes(t)));
    expect(bobOrder).toEqual(texts);
  });
});
