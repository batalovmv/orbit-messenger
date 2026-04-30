import { getAllowedAttachmentOptions } from './chats';

describe('chat attachment helpers', () => {
  it('keeps checklist attachment disabled while polls wait for group permissions', () => {
    const chat = {
      id: '-100123',
      isMonoforum: false,
      type: 'group',
    } as any;

    const unresolvedOptions = getAllowedAttachmentOptions(chat);
    const resolvedOptions = getAllowedAttachmentOptions(chat, {} as any);

    expect(unresolvedOptions.canAttachPolls).toBe(false);
    expect(unresolvedOptions.canAttachToDoLists).toBe(false);
    expect(resolvedOptions.canAttachPolls).toBe(true);
    expect(resolvedOptions.canAttachToDoLists).toBe(false);
  });
});
