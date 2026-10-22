import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  TouchSensor,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type Announcements,
  type DragEndEvent,
  type DragStartEvent,
  type ScreenReaderInstructions,
} from '@dnd-kit/core';
import { useState } from 'react';
import type { components } from '../../api/schema';
import { readableTextColor } from './colors';
import { zoneCoordinateGetter } from './keyboard';
import { POOL, resolveDropTarget, type BoardAction, type BoardState } from './placement';

type Tier = components['schemas']['Tier'];
type Item = components['schemas']['Item'];

interface BoardProps {
  tiers: Tier[];
  items: Item[];
  state: BoardState;
  dispatch: (action: BoardAction) => void;
  disabled?: boolean;
}

const instructions: ScreenReaderInstructions = {
  draggable:
    'Чтобы взять элемент, нажмите пробел или Enter. Стрелки переключают тиры и пул, пробел или Enter кладёт элемент, Escape отменяет.',
};

export function Board({ tiers, items, state, dispatch, disabled }: BoardProps) {
  const [activeId, setActiveId] = useState<string | null>(null);
  const itemById = new Map(items.map((i) => [i.id, i]));
  const zoneName = (id: string | null | undefined) =>
    id === POOL ? 'Пул' : (tiers.find((t) => t.id === id)?.name ?? 'вне зоны');
  const labelOf = (id: string | number) => itemById.get(String(id))?.label ?? String(id);
  const zoneOf = (id: string | number | undefined) =>
    id === undefined ? null : resolveDropTarget(state, String(id));

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(TouchSensor, { activationConstraint: { delay: 150, tolerance: 8 } }),
    useSensor(KeyboardSensor, { coordinateGetter: zoneCoordinateGetter }),
  );

  const announcements: Announcements = {
    onDragStart: ({ active }) => `Взят элемент «${labelOf(active.id)}».`,
    onDragOver: ({ active, over }) =>
      over
        ? `«${labelOf(active.id)}» над зоной «${zoneName(zoneOf(over.id))}».`
        : `«${labelOf(active.id)}» вне зон.`,
    onDragEnd: ({ active, over }) =>
      over
        ? `«${labelOf(active.id)}» помещён в зону «${zoneName(zoneOf(over.id))}».`
        : `«${labelOf(active.id)}» возвращён на место.`,
    onDragCancel: ({ active }) => `Перемещение «${labelOf(active.id)}» отменено.`,
  };

  function onDragStart(e: DragStartEvent) {
    setActiveId(String(e.active.id));
  }
  function onDragEnd(e: DragEndEvent) {
    setActiveId(null);
    const to = resolveDropTarget(state, e.over ? String(e.over.id) : null);
    if (to !== null) dispatch({ type: 'move', itemId: String(e.active.id), to });
  }

  const moveTargets = [
    ...tiers.map((t) => ({ id: t.id, name: t.name })),
    { id: POOL, name: 'Пул' },
  ];
  const renderItem = (id: string) => {
    const item = itemById.get(id);
    if (!item) return null;
    return (
      <ItemChip
        key={id}
        item={item}
        zone={zoneOf(id) ?? POOL}
        targets={moveTargets}
        onMove={(to) => dispatch({ type: 'move', itemId: id, to })}
        disabled={disabled}
      />
    );
  };

  const active = activeId ? itemById.get(activeId) : undefined;
  return (
    <DndContext
      sensors={sensors}
      accessibility={{ announcements, screenReaderInstructions: instructions }}
      onDragStart={onDragStart}
      onDragEnd={onDragEnd}
      onDragCancel={() => setActiveId(null)}
    >
      <div className="take-board">
        {[...tiers]
          .sort((a, b) => a.position - b.position)
          .map((tier) => (
            <Zone
              key={tier.id}
              id={tier.id}
              title={tier.name}
              color={tier.color}
              count={state.tiers[tier.id]?.length ?? 0}
            >
              {(state.tiers[tier.id] ?? []).map(renderItem)}
            </Zone>
          ))}
        <Zone id={POOL} title="Не размещено" count={state.pool.length} pool>
          {state.pool.map(renderItem)}
        </Zone>
      </div>
      <DragOverlay>
        {active ? <div className="take-chip take-chip--overlay">{active.label}</div> : null}
      </DragOverlay>
    </DndContext>
  );
}

interface ZoneProps {
  id: string;
  title: string;
  color?: string;
  count: number;
  pool?: boolean;
  children: React.ReactNode;
}

function Zone({ id, title, color, count, pool, children }: ZoneProps) {
  const { setNodeRef, isOver } = useDroppable({ id });
  return (
    <section
      className={`take-zone${pool ? ' take-zone--pool' : ''}${isOver ? ' is-over' : ''}`}
      aria-label={`${title}, элементов: ${count}`}
    >
      <h2
        className="take-zone__title"
        style={color ? { background: color, color: readableTextColor(color) } : undefined}
      >
        {title}
      </h2>
      <ul ref={setNodeRef} className="take-zone__items">
        {children}
        {count === 0 && <li className="take-zone__empty">Перетащите сюда</li>}
      </ul>
    </section>
  );
}

interface ItemChipProps {
  item: Item;
  zone: string;
  targets: { id: string; name: string }[];
  onMove: (to: string) => void;
  disabled?: boolean;
}

function ItemChip({ item, zone, targets, onMove, disabled }: ItemChipProps) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id: item.id,
    disabled,
  });
  return (
    <li className={`take-chip${isDragging ? ' is-dragging' : ''}`}>
      <div ref={setNodeRef} className="take-chip__handle" {...attributes} {...listeners}>
        {item.image_url ? <img src={item.image_url} alt="" className="take-chip__img" /> : null}
        <span>{item.label}</span>
      </div>
      <select
        className="take-chip__move"
        aria-label={`Переместить «${item.label}»`}
        value={zone}
        disabled={disabled}
        onChange={(e) => onMove(e.target.value)}
      >
        {targets.map((t) => (
          <option key={t.id} value={t.id}>
            {t.name}
          </option>
        ))}
      </select>
    </li>
  );
}
