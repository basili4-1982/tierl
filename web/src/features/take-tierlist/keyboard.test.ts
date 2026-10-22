import type { KeyboardCoordinateGetter } from '@dnd-kit/core';
import { zoneCoordinateGetter } from './keyboard';

type Args = Parameters<KeyboardCoordinateGetter>[1];

const rect = (top: number) => ({
  top,
  left: 0,
  width: 300,
  height: 100,
  right: 300,
  bottom: top + 100,
});

function run(code: string, dragTop: number | null) {
  const zones = [0, 100, 200].map((top, i) => ({ id: `z${i}`, rect: rect(top) }));
  const preventDefault = vi.fn();
  const args = {
    context: {
      collisionRect: dragTop === null ? null : { ...rect(dragTop), height: 20 },
      droppableRects: new Map(zones.map((z) => [z.id, z.rect])),
      droppableContainers: { getEnabled: () => zones },
    },
  } as unknown as Args;
  const out = zoneCoordinateGetter({ code, preventDefault } as unknown as KeyboardEvent, args);
  return { out, preventDefault };
}

describe('zoneCoordinateGetter', () => {
  it('ignores other keys', () => {
    const { out, preventDefault } = run('KeyA', 10);
    expect(out).toBeUndefined();
    expect(preventDefault).not.toHaveBeenCalled();
  });

  it('does nothing without a drag rect', () => {
    expect(run('ArrowDown', null).out).toBeUndefined();
  });

  it('jumps to the next and previous zone', () => {
    expect(run('ArrowDown', 10).out).toEqual({ x: 8, y: 108 });
    expect(run('ArrowRight', 10).out).toEqual({ x: 8, y: 108 });
    expect(run('ArrowUp', 110).out).toEqual({ x: 8, y: 8 });
  });

  it('wraps around at the ends', () => {
    expect(run('ArrowDown', 210).out).toEqual({ x: 8, y: 8 });
    expect(run('ArrowUp', 10).out).toEqual({ x: 8, y: 208 });
  });

  it('returns nothing when there are no zones', () => {
    const args = {
      context: {
        collisionRect: rect(0),
        droppableRects: new Map(),
        droppableContainers: { getEnabled: () => [] },
      },
    } as unknown as Args;
    const ev = { code: 'ArrowDown', preventDefault: vi.fn() } as unknown as KeyboardEvent;
    expect(zoneCoordinateGetter(ev, args)).toBeUndefined();
  });
});
