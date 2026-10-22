import { emptyDraft, newItem, newTier } from './model';
import { LIMITS, validateDraft, validateSlug } from './validation';
import type { Draft } from './validation';

function valid(): Draft {
  const d = emptyDraft();
  d.title = 'Тема';
  d.items = [newItem('a'), newItem('b'), newItem('c')];
  return d;
}

describe('validateDraft', () => {
  it('accepts a valid draft', () => {
    expect(validateDraft(valid())).toEqual({});
  });

  it('checks the title', () => {
    const d = valid();
    d.title = '  ';
    expect(validateDraft(d).title).toBeTruthy();
    d.title = 'я'.repeat(LIMITS.maxTitle + 1);
    expect(validateDraft(d).title).toBeTruthy();
    d.title = 'я'.repeat(LIMITS.maxTitle);
    expect(validateDraft(d).title).toBeUndefined();
  });

  it('counts code points, not UTF-16 units', () => {
    const d = valid();
    d.title = '😀'.repeat(LIMITS.maxTitle);
    expect(validateDraft(d).title).toBeUndefined();
  });

  it('checks description length', () => {
    const d = valid();
    d.description = 'x'.repeat(LIMITS.maxDescription + 1);
    expect(validateDraft(d).description).toBeTruthy();
  });

  it('checks item count and labels', () => {
    const d = valid();
    d.items = [newItem('a'), newItem('b')];
    expect(validateDraft(d).items).toBeTruthy();
    d.items = Array.from({ length: LIMITS.maxItems + 1 }, () => newItem('x'));
    expect(validateDraft(d).items).toBeTruthy();
    d.items = [newItem(''), newItem('x'.repeat(LIMITS.maxItemLabel + 1)), newItem('ok')];
    const e = validateDraft(d);
    expect(e['items.0.label']).toBeTruthy();
    expect(e['items.1.label']).toBeTruthy();
    expect(e['items.2.label']).toBeUndefined();
  });

  it('checks image urls', () => {
    const d = valid();
    d.items[0]!.imageUrl = 'ftp://x/y.png';
    d.items[1]!.imageUrl = 'not a url';
    d.items[2]!.imageUrl = 'https://cdn.example.com/' + 'a'.repeat(LIMITS.maxImageUrl);
    const e = validateDraft(d);
    expect(e['items.0.image_url']).toBeTruthy();
    expect(e['items.1.image_url']).toBeTruthy();
    expect(e['items.2.image_url']).toContain('2048');
    d.items[0]!.imageUrl = 'https://cdn.example.com/a.png';
    d.items[1]!.imageUrl = '';
    d.items[2]!.imageUrl = '   ';
    expect(validateDraft(d)).toEqual({});
  });

  it('checks tiers', () => {
    const d = valid();
    d.tiers = [newTier('S', '#ffffff')];
    expect(validateDraft(d).tiers).toBeTruthy();
    d.tiers = Array.from({ length: LIMITS.maxTiers + 1 }, () => newTier('x', '#ffffff'));
    expect(validateDraft(d).tiers).toBeTruthy();
    d.tiers = [
      newTier('', '#fff'),
      newTier('x'.repeat(LIMITS.maxTierName + 1), '#GGGGGG'),
      newTier('A', '#AbCdEf'),
    ];
    const e = validateDraft(d);
    expect(e['tiers.0.name']).toBeTruthy();
    expect(e['tiers.0.color']).toBeTruthy();
    expect(e['tiers.1.name']).toBeTruthy();
    expect(e['tiers.1.color']).toBeTruthy();
    expect(e['tiers.2.name']).toBeUndefined();
    expect(e['tiers.2.color']).toBeUndefined();
  });

  it('requires a slug only for public lists', () => {
    const d = valid();
    d.slug = '';
    expect(validateDraft(d).slug).toBeUndefined();
    d.visibility = 'public';
    expect(validateDraft(d).slug).toBeTruthy();
    d.slug = 'my-list_1';
    expect(validateDraft(d).slug).toBeUndefined();
  });
});

describe('validateSlug', () => {
  it('checks length and alphabet', () => {
    expect(validateSlug('ab')).toBeTruthy();
    expect(validateSlug('a'.repeat(65))).toBeTruthy();
    expect(validateSlug('bad slug')).toBeTruthy();
    expect(validateSlug('абв')).toBeTruthy();
    expect(validateSlug('abc')).toBeUndefined();
  });
});
