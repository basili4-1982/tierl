import type { KeyboardCoordinateGetter } from '@dnd-kit/core';

const PREV = new Set(['ArrowUp', 'ArrowLeft']);
const NEXT = new Set(['ArrowDown', 'ArrowRight']);

/**
 * Keyboard dragging jumps between zones (tiers, then the pool) instead of
 * moving by pixels: arrows select the previous/next zone in layout order.
 */
export const zoneCoordinateGetter: KeyboardCoordinateGetter = (event, { context }) => {
  const forward = NEXT.has(event.code);
  if (!forward && !PREV.has(event.code)) return undefined;
  event.preventDefault();
  const { droppableRects, droppableContainers, collisionRect } = context;
  if (!collisionRect) return undefined;
  const zones = droppableContainers
    .getEnabled()
    .map((c) => ({ rect: droppableRects.get(c.id) }))
    .filter((z): z is { rect: NonNullable<typeof z.rect> } => Boolean(z.rect))
    .sort((a, b) => a.rect.top - b.rect.top || a.rect.left - b.rect.left);
  const inside = (r: { top: number; left: number; width: number; height: number }) =>
    collisionRect.top + collisionRect.height / 2 >= r.top &&
    collisionRect.top + collisionRect.height / 2 <= r.top + r.height;
  const index = zones.findIndex((z) => inside(z.rect));
  const target = zones[forward ? index + 1 : index - 1] ?? zones[forward ? 0 : zones.length - 1];
  if (!target) return undefined;
  // Coordinates are the new top-left corner of the dragged element.
  return { x: target.rect.left + 8, y: target.rect.top + 8 };
};
