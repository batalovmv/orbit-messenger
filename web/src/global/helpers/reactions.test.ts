import { getReactionKey, isSameReaction } from './reactions';

describe('reaction helpers', () => {
  it('normalizes emoji variation selectors in reaction keys', () => {
    expect(getReactionKey({
      type: 'emoji',
      emoticon: '❤',
    })).toBe(getReactionKey({
      type: 'emoji',
      emoticon: '❤️',
    }));
  });

  it('treats emoji reactions with and without variation selectors as equal', () => {
    expect(isSameReaction({
      type: 'emoji',
      emoticon: '❤',
    }, {
      type: 'emoji',
      emoticon: '❤️',
    })).toBe(true);
  });
});
