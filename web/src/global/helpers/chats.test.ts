import { getAllowedAttachmentOptions } from './chats';

describe('chat attachment helpers', () => {
  it('keeps checklist attachment disabled for Orbit chats', () => {
    const options = getAllowedAttachmentOptions({
      id: '-100123',
      isMonoforum: false,
      type: 'group',
    } as any);

    expect(options.canAttachPolls).toBe(true);
    expect(options.canAttachToDoLists).toBe(false);
  });
});
