import type { components } from '../../api/schema';
import type { Draft, DraftItem, DraftTier } from './validation';

type Tierlist = components['schemas']['Tierlist'];
type TierlistInput = components['schemas']['TierlistInput'];

let counter = 0;
export const newKey = () => `k${++counter}`;

export const DEFAULT_TIERS: ReadonlyArray<{ name: string; color: string }> = [
  { name: 'S', color: '#ff7f7f' },
  { name: 'A', color: '#ffbf7f' },
  { name: 'B', color: '#ffdf7f' },
  { name: 'C', color: '#bfff7f' },
  { name: 'D', color: '#7fffff' },
  { name: 'F', color: '#bf7fff' },
];

export const newItem = (label = '', imageUrl = ''): DraftItem => ({
  key: newKey(),
  label,
  imageUrl,
});
export const newTier = (name = '', color = '#cccccc'): DraftTier => ({
  key: newKey(),
  name,
  color,
});

export function emptyDraft(): Draft {
  return {
    title: '',
    description: '',
    visibility: 'unlisted',
    slug: '',
    items: [newItem(), newItem(), newItem()],
    tiers: DEFAULT_TIERS.map((t) => newTier(t.name, t.color)),
  };
}

export function draftFromTierlist(t: Tierlist): Draft {
  return {
    title: t.title,
    description: t.description,
    visibility: t.visibility,
    slug: t.slug,
    items: [...t.items]
      .sort((a, b) => a.position - b.position)
      .map((i) => newItem(i.label, i.image_url ?? '')),
    tiers: [...t.tiers]
      .sort((a, b) => a.position - b.position)
      .map((x) => newTier(x.name, x.color)),
  };
}

export function toStructure(d: Draft): Pick<TierlistInput, 'items' | 'tiers'> {
  return {
    items: d.items.map((i) => {
      const url = i.imageUrl.trim();
      return url === '' ? { label: i.label.trim() } : { label: i.label.trim(), image_url: url };
    }),
    tiers: d.tiers.map((t) => ({ name: t.name.trim(), color: t.color })),
  };
}

/** Moves the element at `from` by `delta` positions; out-of-range moves are ignored. */
export function move<T>(list: T[], from: number, delta: number): T[] {
  const to = from + delta;
  if (to < 0 || to >= list.length) return list;
  const next = [...list];
  next.splice(to, 0, ...next.splice(from, 1));
  return next;
}
