import { computeSafetyNumber } from './safety-numbers';

describe('computeSafetyNumber', () => {
  const aliceUserId = '00000000-0000-0000-0000-000000000001';
  const bobUserId = '00000000-0000-0000-0000-000000000002';
  const aliceKey = new Uint8Array(32).fill(0xaa);
  const bobKey = new Uint8Array(32).fill(0xbb);

  it('returns 60 digits in 5 groups of 12 separated by single spaces', () => {
    const sn = computeSafetyNumber(aliceUserId, aliceKey, bobUserId, bobKey);
    const groups = sn.split(' ');
    expect(groups).toHaveLength(5);
    for (const group of groups) {
      expect(group).toHaveLength(12);
      expect(group).toMatch(/^\d{12}$/);
    }
    expect(sn.replace(/ /g, '')).toHaveLength(60);
  });

  it('is symmetric (order of (userA, userB) does not matter)', () => {
    const ab = computeSafetyNumber(aliceUserId, aliceKey, bobUserId, bobKey);
    const ba = computeSafetyNumber(bobUserId, bobKey, aliceUserId, aliceKey);
    expect(ab).toBe(ba);
  });

  it('is deterministic for the same inputs', () => {
    const first = computeSafetyNumber(aliceUserId, aliceKey, bobUserId, bobKey);
    const second = computeSafetyNumber(aliceUserId, aliceKey, bobUserId, bobKey);
    expect(first).toBe(second);
  });

  it('changes when either identity key changes', () => {
    const base = computeSafetyNumber(aliceUserId, aliceKey, bobUserId, bobKey);

    const aliceKey2 = new Uint8Array(aliceKey);
    aliceKey2[0] ^= 0xff;
    const changed = computeSafetyNumber(aliceUserId, aliceKey2, bobUserId, bobKey);
    expect(changed).not.toBe(base);

    const bobKey2 = new Uint8Array(bobKey);
    bobKey2[31] ^= 0x01;
    const changed2 = computeSafetyNumber(aliceUserId, aliceKey, bobUserId, bobKey2);
    expect(changed2).not.toBe(base);
  });

  it('changes when user ids change', () => {
    const base = computeSafetyNumber(aliceUserId, aliceKey, bobUserId, bobKey);
    const otherBob = '00000000-0000-0000-0000-000000000003';
    const changed = computeSafetyNumber(aliceUserId, aliceKey, otherBob, bobKey);
    expect(changed).not.toBe(base);
  });
});
