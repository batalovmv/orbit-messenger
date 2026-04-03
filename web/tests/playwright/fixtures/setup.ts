import { type BrowserContext, type Page, test as base } from '@playwright/test';

import { FAKE_WEBSOCKET_SCRIPT } from './fake-websocket';
import {
  buildWsMessageDeleted,
  buildWsMessageUpdated,
  buildWsNewMessage,
  MockServerState,
} from './mock-state';

export const BASE_URL = 'http://localhost:1235';

// ---- Route handler setup ----

export function setupRoutes(context: BrowserContext, state: MockServerState, currentUserId: string) {
  const currentUser = currentUserId === state.alice.id ? state.alice : state.bob;

  // 1. Fail-fast catch-all — registered FIRST = lowest priority (LIFO)
  context.route('**/api/v1/**', async (route) => {
    const method = route.request().method();
    const url = route.request().url();
    // Allow OPTIONS (CORS preflight) silently
    if (method === 'OPTIONS') {
      await route.fulfill({ status: 204 });
      return;
    }
    const msg = `Unhandled API: ${method} ${url}`;
    // eslint-disable-next-line no-console
    console.error(`[MOCK FAIL-FAST] ${msg}`);
    await route.fulfill({
      status: 500,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'unhandled_route', message: msg, status: 500 }),
    });
  });

  // 2. Specific handlers — registered AFTER = higher priority

  // Auth
  context.route('**/api/v1/auth/refresh', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        access_token: `test-token-${currentUserId}`,
        expires_in: 86400,
        user: currentUser,
      }),
    });
  });

  context.route('**/api/v1/auth/me', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify(currentUser),
    });
  });

  context.route('**/api/v1/auth/sessions', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        sessions: [{
          id: `session-${currentUserId}`,
          hash: `session-${currentUserId}`,
          app_name: 'Orbit Web',
          app_version: 'test',
          platform: 'web',
          device_model: 'Playwright',
          system_version: 'test',
          ip: '127.0.0.1',
          country: 'Test',
          region: 'Local',
          created_at: currentUser.created_at,
          last_active_at: currentUser.updated_at,
          is_current: true,
          can_accept_calls: true,
          can_accept_secret_chats: false,
        }],
      }),
    });
  });

  // Users
  context.route('**/api/v1/users/me/settings/appearance', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        user_id: currentUserId,
        theme: 'dark',
        language: 'en',
        font_size: 16,
        send_by_enter: true,
        created_at: currentUser.created_at,
        updated_at: currentUser.updated_at,
      }),
    });
  });

  context.route('**/api/v1/users?q=*&limit=*', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        users: [state.alice, state.bob],
      }),
    });
  });

  context.route('**/api/v1/users/*', async (route) => {
    const url = route.request().url();
    const userId = url.match(/\/users\/([^/?]+)/)?.[1];
    const user = userId === state.alice.id ? state.alice : userId === state.bob.id ? state.bob : currentUser;

    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify(user),
    });
  });

  // Chats list (idempotent — may be called multiple times during bootstrap)
  context.route('**/api/v1/chats?*', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: [state.buildChatListItem(currentUserId)],
        has_more: false,
      }),
    });
  });

  context.route(`**/api/v1/chats/${state.chatId}`, async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify(state.buildChatListItem(currentUserId)),
      });
    } else {
      await route.fallback();
    }
  });

  // Chat members
  context.route(`**/api/v1/chats/${state.chatId}/members*`, async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: state.buildMembers(), has_more: false }),
    });
  });

  context.route(`**/api/v1/chats/${state.chatId}/available-reactions`, async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        chat_id: state.chatId,
        mode: 'all',
      }),
    });
  });

  context.route(`**/api/v1/chats/${state.chatId}/messages/scheduled`, async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify([]),
    });
  });

  // Messages - GET (history)
  context.route(`**/api/v1/chats/${state.chatId}/messages*`, async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: state.getMessagesDesc(),
          has_more: false,
        }),
      });
      return;
    }

    // POST - send message
    if (route.request().method() === 'POST') {
      let body: { content?: string; entities?: unknown[] };
      try {
        body = route.request().postDataJSON();
      } catch {
        body = { content: '' };
      }

      const msg = state.addMessage(currentUserId, body.content || '');

      // Respond to sender first
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify(msg),
      });

      // Then broadcast WS to ALL pages (mirrors real gateway)
      await state.broadcastWs(buildWsNewMessage(msg));
      return;
    }

    await route.fallback();
  });

  context.route('**/api/v1/messages/*', async (route) => {
    const url = route.request().url();
    const uuidMatch = url.match(/\/messages\/([0-9a-f-]{36})/i);
    if (!uuidMatch) {
      await route.fallback();
      return;
    }
    const uuid = uuidMatch[1];

    if (route.request().method() === 'GET') {
      const message = state.findByUuid(uuid);
      if (!message) {
        await route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'not_found' }),
        });
        return;
      }

      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify(message),
      });
      return;
    }

    await route.fallback();
  });

  // Mark read
  context.route(`**/api/v1/chats/${state.chatId}/read`, async (route) => {
    if (route.request().method() === 'PATCH') {
      state.lastReadByUserId[currentUserId] = state.messages[state.messages.length - 1]?.id || '';
      await route.fulfill({ status: 204, body: '' });
      return;
    }
    await route.fallback();
  });

  // Delete message: DELETE /messages/:uuid
  context.route('**/api/v1/messages/*', async (route) => {
    const url = route.request().url();
    const uuidMatch = url.match(/\/messages\/([0-9a-f-]{36})/i);
    if (!uuidMatch) {
      await route.fallback();
      return;
    }
    const uuid = uuidMatch[1];

    if (route.request().method() === 'DELETE') {
      const deleted = state.deleteMessage(uuid);
      if (!deleted) {
        await route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'not_found' }),
        });
        return;
      }
      await route.fulfill({ status: 204, body: '' });
      // Broadcast delete to all pages
      await state.broadcastWs(buildWsMessageDeleted(state.chatId, deleted.sequence_number));
      return;
    }

    if (route.request().method() === 'PATCH') {
      let body: { content?: string; entities?: unknown[] };
      try {
        body = route.request().postDataJSON();
      } catch {
        body = {};
      }

      const updated = state.editMessage(uuid, body.content || '');
      if (!updated) {
        await route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'not_found' }),
        });
        return;
      }
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify(updated),
      });
      // Broadcast edit to all pages
      await state.broadcastWs(buildWsMessageUpdated(updated));
      return;
    }

    await route.fallback();
  });

  // Stickers
  context.route('**/api/v1/stickers/featured*', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify([]),
    });
  });

  context.route('**/api/v1/stickers/installed*', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify([]),
    });
  });

  context.route('**/api/v1/stickers/search*', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify([]),
    });
  });

  // Pinned messages
  context.route(`**/api/v1/chats/${state.chatId}/pinned*`, async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ messages: [] }),
    });
  });

  // Link preview
  context.route('**/api/v1/messages/link-preview*', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ preview: undefined }),
    });
  });

  // Sync/fetchDifference — no-op stub
  context.route('**/api/v1/sync*', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: [], has_more: false }),
    });
  });
}

// ---- Wait for app readiness ----

export async function waitForReady(page: Page) {
  // 1. WS fake is connected
  await page.waitForFunction(() => (window as any).__TEST_WS_READY__ === true, undefined, { timeout: 15000 });

  // 2. Composer is visible (chat is open)
  await page.waitForSelector('#editable-message-text', { timeout: 15000 });
}

// ---- Fixture ----

type MessagingFixtures = {
  alicePage: Page;
  bobPage: Page;
  mockState: MockServerState;
};

export const test = base.extend<MessagingFixtures>({
  // eslint-disable-next-line no-empty-pattern
  mockState: async ({}, use) => {
    const state = new MockServerState();
    await use(state);
  },

  alicePage: async ({ browser, mockState }, use) => {
    const context = await browser.newContext();
    await context.addInitScript(FAKE_WEBSOCKET_SCRIPT);
    setupRoutes(context, mockState, mockState.alice.id);

    const page = await context.newPage();
    mockState.allPages.push(page);

    await page.goto(`${BASE_URL}/#${mockState.chatId}`);
    await waitForReady(page);

    await use(page);
    await context.close();
  },

  bobPage: async ({ browser, mockState }, use) => {
    const context = await browser.newContext();
    await context.addInitScript(FAKE_WEBSOCKET_SCRIPT);
    setupRoutes(context, mockState, mockState.bob.id);

    const page = await context.newPage();
    mockState.allPages.push(page);

    await page.goto(`${BASE_URL}/#${mockState.chatId}`);
    await waitForReady(page);

    await use(page);
    await context.close();
  },
});

export { expect } from '@playwright/test';
