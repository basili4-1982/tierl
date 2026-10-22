// Pure state logic of the take-tierlist board. No React, no DOM.

export const POOL = 'pool';
/** SPEC section 4: minimal answer completeness. */
export const MIN_PLACED = 3;

export type Container = typeof POOL | string;

export interface Placement {
  item_id: string;
  tier_id: string;
}

export interface BoardState {
  /** Unplaced item ids in display order. */
  pool: string[];
  /** Item ids per tier id, in drop order. */
  tiers: Record<string, string[]>;
}

export type BoardAction =
  | { type: 'move'; itemId: string; to: Container }
  | { type: 'reset'; itemIds: string[]; tierIds: string[]; placements?: Placement[] };

export function initBoard(
  itemIds: string[],
  tierIds: string[],
  placements: Placement[] = [],
): BoardState {
  const tiers: Record<string, string[]> = Object.fromEntries(tierIds.map((id) => [id, []]));
  const placed = new Set<string>();
  const known = new Set(itemIds);
  for (const p of placements) {
    const bucket = tiers[p.tier_id];
    if (bucket && known.has(p.item_id) && !placed.has(p.item_id)) {
      bucket.push(p.item_id);
      placed.add(p.item_id);
    }
  }
  return { pool: itemIds.filter((id) => !placed.has(id)), tiers };
}

export function findContainer(state: BoardState, itemId: string): Container | null {
  if (state.pool.includes(itemId)) return POOL;
  for (const [tierId, ids] of Object.entries(state.tiers)) {
    if (ids.includes(itemId)) return tierId;
  }
  return null;
}

export function boardReducer(state: BoardState, action: BoardAction): BoardState {
  switch (action.type) {
    case 'reset':
      return initBoard(action.itemIds, action.tierIds, action.placements);
    case 'move': {
      const from = findContainer(state, action.itemId);
      if (from === null || from === action.to) return state;
      if (action.to !== POOL && !(action.to in state.tiers)) return state;
      const { itemId, to } = action;
      const without = (ids: string[]) => ids.filter((id) => id !== itemId);
      const tiers = Object.fromEntries(
        Object.entries(state.tiers).map(([tierId, ids]) => [
          tierId,
          tierId === from ? without(ids) : tierId === to ? [...ids, itemId] : ids,
        ]),
      );
      return {
        pool:
          from === POOL ? without(state.pool) : to === POOL ? [...state.pool, itemId] : state.pool,
        tiers,
      };
    }
  }
}

export function placedCount(state: BoardState): number {
  return Object.values(state.tiers).reduce((n, ids) => n + ids.length, 0);
}

/** Needed placements: at most MIN_PLACED, fewer when the survey has fewer items. */
export function requiredCount(totalItems: number): number {
  return Math.min(MIN_PLACED, totalItems);
}

export function canSubmit(state: BoardState, totalItems: number): boolean {
  return placedCount(state) >= requiredCount(totalItems);
}

export function toPlacements(state: BoardState): Placement[] {
  return Object.entries(state.tiers).flatMap(([tierId, ids]) =>
    ids.map((itemId) => ({ item_id: itemId, tier_id: tierId })),
  );
}

/** Maps a dnd-kit droppable/draggable id (container or item) to a container. */
export function resolveDropTarget(state: BoardState, overId: string | null): Container | null {
  if (overId === null) return null;
  if (overId === POOL || overId in state.tiers) return overId;
  return findContainer(state, overId);
}
