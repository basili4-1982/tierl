import type { components } from '../../api/schema';
import { readableText } from './color';
import { draftFromTierlist, emptyDraft, move, toStructure, newItem } from './model';

describe('model', () => {
  it('starts with default S..F tiers and three items', () => {
    const d = emptyDraft();
    expect(d.tiers.map((t) => t.name)).toEqual(['S', 'A', 'B', 'C', 'D', 'F']);
    expect(d.items).toHaveLength(3);
    expect(d.visibility).toBe('unlisted');
  });

  it('moves elements and ignores out-of-range moves', () => {
    expect(move([1, 2, 3], 0, 1)).toEqual([2, 1, 3]);
    expect(move([1, 2, 3], 2, -1)).toEqual([1, 3, 2]);
    const l = [1, 2];
    expect(move(l, 0, -1)).toBe(l);
    expect(move(l, 1, 1)).toBe(l);
  });

  it('sorts a loaded tierlist by position', () => {
    const t = {
      title: 't',
      description: '',
      visibility: 'public',
      slug: 's',
      tiers: [
        { id: '2', name: 'B', color: '#000000', position: 1 },
        { id: '1', name: 'A', color: '#ffffff', position: 0 },
      ],
      items: [
        { id: '2', label: 'y', image_url: null, position: 1 },
        { id: '1', label: 'x', image_url: 'https://e.x/i.png', position: 0 },
      ],
    } as components['schemas']['Tierlist'];
    const d = draftFromTierlist(t);
    expect(d.tiers.map((x) => x.name)).toEqual(['A', 'B']);
    expect(d.items.map((x) => [x.label, x.imageUrl])).toEqual([
      ['x', 'https://e.x/i.png'],
      ['y', ''],
    ]);
  });

  it('builds the API structure and trims, omitting empty image urls', () => {
    const d = emptyDraft();
    d.items = [newItem(' a ', ' https://e.x/a.png '), newItem('b', '')];
    const s = toStructure(d);
    expect(s.items).toEqual([{ label: 'a', image_url: 'https://e.x/a.png' }, { label: 'b' }]);
    expect(s.tiers?.[0]).toEqual({ name: 'S', color: '#ff7f7f' });
  });

  it('picks a readable text colour', () => {
    expect(readableText('#ffffff')).toBe('#000000');
    expect(readableText('#000000')).toBe('#ffffff');
    expect(readableText('bad')).toBe('#000000');
  });
});
