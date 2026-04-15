import 'fake-indexeddb/auto';

import {
  addToIndex,
  clearAllClientSearchIndex,
  clearChatIndex,
  removeFromIndex,
  resetClientIndexForTests,
  searchClient,
  tokenize,
} from './client-index';

describe('tokenize', () => {
  it('splits on non-letter characters and lowercases', () => {
    expect(tokenize('Hello, World!')).toEqual(['hello', 'world']);
  });

  it('handles Cyrillic and punctuation', () => {
    expect(tokenize('Привет, Мир!')).toEqual(['привет', 'мир']);
  });

  it('drops single-character tokens to cut noise', () => {
    expect(tokenize('a bb ccc')).toEqual(['bb', 'ccc']);
  });

  it('returns an empty array for empty input', () => {
    expect(tokenize('')).toEqual([]);
  });
});

describe('client search index', () => {
  beforeEach(async () => {
    // Each test starts with a clean store so we don't leak state
    // between specs (fake-indexeddb persists across test runs).
    resetClientIndexForTests();
    try {
      await clearAllClientSearchIndex();
    } catch {
      // First run — no DB yet.
    }
  });

  it('adds a message and finds it by single-word query', async () => {
    await addToIndex('chat-1', 10, 'Hello from Alice');
    const results = await searchClient('chat-1', 'alice');
    expect(results).toEqual([10]);
  });

  it('intersects posting lists for multi-word AND queries', async () => {
    await addToIndex('chat-1', 1, 'alpha beta gamma');
    await addToIndex('chat-1', 2, 'alpha omega');
    await addToIndex('chat-1', 3, 'beta delta');
    await addToIndex('chat-1', 4, 'alpha beta');

    expect(await searchClient('chat-1', 'alpha beta')).toEqual([1, 4]);
    expect(await searchClient('chat-1', 'alpha')).toEqual([1, 2, 4]);
    expect(await searchClient('chat-1', 'alpha zeta')).toEqual([]);
  });

  it('scopes search to a chatId — cross-chat pollution is impossible', async () => {
    await addToIndex('chat-1', 1, 'shared word');
    await addToIndex('chat-2', 2, 'shared word');
    expect(await searchClient('chat-1', 'shared')).toEqual([1]);
    expect(await searchClient('chat-2', 'shared')).toEqual([2]);
  });

  it('is idempotent on repeat add for the same message id', async () => {
    await addToIndex('chat-1', 5, 'repeat repeat');
    await addToIndex('chat-1', 5, 'repeat repeat');
    await addToIndex('chat-1', 5, 'repeat repeat');
    expect(await searchClient('chat-1', 'repeat')).toEqual([5]);
  });

  it('removes a message from all posting lists', async () => {
    await addToIndex('chat-1', 1, 'alpha beta');
    await addToIndex('chat-1', 2, 'alpha gamma');
    await removeFromIndex('chat-1', 1);
    expect(await searchClient('chat-1', 'alpha')).toEqual([2]);
    expect(await searchClient('chat-1', 'beta')).toEqual([]);
  });

  it('clearChatIndex drops every posting list for the given chat', async () => {
    await addToIndex('chat-1', 1, 'alpha');
    await addToIndex('chat-2', 2, 'alpha');
    await clearChatIndex('chat-1');
    expect(await searchClient('chat-1', 'alpha')).toEqual([]);
    expect(await searchClient('chat-2', 'alpha')).toEqual([2]);
  });

  it('searchClient returns [] on empty query', async () => {
    await addToIndex('chat-1', 1, 'hello');
    expect(await searchClient('chat-1', '   ')).toEqual([]);
  });
});
