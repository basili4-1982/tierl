import {
  boardReducer,
  canSubmit,
  findContainer,
  initBoard,
  MIN_PLACED,
  placedCount,
  POOL,
  requiredCount,
  resolveDropTarget,
  toPlacements,
} from './placement';

const items = ['a', 'b', 'c', 'd'];
const tiers = ['t1', 't2'];

describe('initBoard', () => {
  it('puts everything in the pool without placements', () => {
    expect(initBoard(items, tiers)).toEqual({ pool: items, tiers: { t1: [], t2: [] } });
  });

  it('restores placements and ignores unknown or duplicate entries', () => {
    const s = initBoard(items, tiers, [
      { item_id: 'a', tier_id: 't2' },
      { item_id: 'a', tier_id: 't1' },
      { item_id: 'zzz', tier_id: 't1' },
      { item_id: 'b', tier_id: 'nope' },
    ]);
    expect(s).toEqual({ pool: ['b', 'c', 'd'], tiers: { t1: [], t2: ['a'] } });
  });
});

describe('boardReducer', () => {
  const start = initBoard(items, tiers);

  it('moves from pool to tier and back', () => {
    const s1 = boardReducer(start, { type: 'move', itemId: 'b', to: 't1' });
    expect(s1.pool).toEqual(['a', 'c', 'd']);
    expect(s1.tiers.t1).toEqual(['b']);
    const s2 = boardReducer(s1, { type: 'move', itemId: 'b', to: POOL });
    expect(s2.pool).toEqual(['a', 'c', 'd', 'b']);
    expect(s2.tiers.t1).toEqual([]);
  });

  it('moves between tiers without mutating the previous state', () => {
    const s1 = boardReducer(start, { type: 'move', itemId: 'a', to: 't1' });
    const snapshot = JSON.stringify(s1);
    const s2 = boardReducer(s1, { type: 'move', itemId: 'a', to: 't2' });
    expect(s2.tiers).toEqual({ t1: [], t2: ['a'] });
    expect(JSON.stringify(s1)).toBe(snapshot);
  });

  it('ignores no-op, unknown item and unknown target', () => {
    expect(boardReducer(start, { type: 'move', itemId: 'a', to: POOL })).toBe(start);
    expect(boardReducer(start, { type: 'move', itemId: 'x', to: 't1' })).toBe(start);
    expect(boardReducer(start, { type: 'move', itemId: 'a', to: 'nope' })).toBe(start);
  });

  it('resets from a new set of placements', () => {
    const s = boardReducer(start, {
      type: 'reset',
      itemIds: ['a', 'b'],
      tierIds: ['t1'],
      placements: [{ item_id: 'a', tier_id: 't1' }],
    });
    expect(s).toEqual({ pool: ['b'], tiers: { t1: ['a'] } });
  });
});

describe('selectors', () => {
  const s = boardReducer(
    boardReducer(initBoard(items, tiers), { type: 'move', itemId: 'a', to: 't1' }),
    { type: 'move', itemId: 'c', to: 't2' },
  );

  it('finds containers', () => {
    expect(findContainer(s, 'a')).toBe('t1');
    expect(findContainer(s, 'b')).toBe(POOL);
    expect(findContainer(s, 'x')).toBeNull();
  });

  it('counts and serialises placements', () => {
    expect(placedCount(s)).toBe(2);
    expect(toPlacements(s)).toEqual([
      { item_id: 'a', tier_id: 't1' },
      { item_id: 'c', tier_id: 't2' },
    ]);
  });

  it('applies the minimal completeness rule', () => {
    expect(MIN_PLACED).toBe(3);
    expect(canSubmit(s, 4)).toBe(false);
    const s3 = boardReducer(s, { type: 'move', itemId: 'b', to: 't2' });
    expect(canSubmit(s3, 4)).toBe(true);
    expect(requiredCount(2)).toBe(2);
    expect(requiredCount(10)).toBe(3);
    expect(canSubmit(initBoard(['a', 'b'], tiers), 2)).toBe(false);
  });

  it('resolves drop targets', () => {
    expect(resolveDropTarget(s, null)).toBeNull();
    expect(resolveDropTarget(s, POOL)).toBe(POOL);
    expect(resolveDropTarget(s, 't2')).toBe('t2');
    expect(resolveDropTarget(s, 'a')).toBe('t1');
    expect(resolveDropTarget(s, 'unknown')).toBeNull();
  });
});
