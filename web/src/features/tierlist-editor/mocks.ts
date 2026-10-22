import { http, HttpResponse } from 'msw';
import type { components } from '../../api/schema';

type Tierlist = components['schemas']['Tierlist'];
type Input = components['schemas']['TierlistInput'] & { slug?: string };
type Patch = components['schemas']['TierlistPatch'];

const author = { id: '0b6f0e9e-5c1a-4a53-9a0e-8a4d1d5b7c11', display_name: 'Anna' };

function seed(slug: string, submissions: number, isOwner = true): Tierlist {
  return {
    id: `id-${slug}`,
    slug,
    title: 'Лучшие языки',
    description: 'Расставьте по тирам',
    visibility: 'public',
    status: 'open',
    tiers: [
      { id: 't1', name: 'S', color: '#ff7f7f', position: 0 },
      { id: 't2', name: 'A', color: '#ffbf7f', position: 1 },
    ],
    items: [
      { id: 'i1', label: 'Go', image_url: null, position: 0 },
      { id: 'i2', label: 'Rust', image_url: 'https://cdn.example.com/rust.png', position: 1 },
      { id: 'i3', label: 'Zig', image_url: null, position: 2 },
    ],
    author,
    is_owner: isOwner,
    submissions_count: submissions,
    created_at: '2026-10-22T09:00:00Z',
    closed_at: null,
  };
}

const store = new Map<string, Tierlist>();

/** Restores the in-memory fixtures (call between tests). */
export function resetTierlistStore(): void {
  store.clear();
  for (const t of [seed('fresh-list', 0), seed('locked-list', 3), seed('foreign-list', 0, false)]) {
    store.set(t.slug, t);
  }
}
resetTierlistStore();

const err = (status: number, code: string, message: string, details?: Record<string, unknown>) =>
  HttpResponse.json({ code, message, details }, { status });

function build(slug: string, input: Input | (Patch & Partial<Input>), base?: Tierlist): Tierlist {
  const prev = base ?? seed(slug, 0);
  return {
    ...prev,
    slug,
    title: input.title ?? prev.title,
    description: input.description ?? prev.description,
    visibility: input.visibility ?? prev.visibility,
    tiers: input.tiers
      ? input.tiers.map((t, position) => ({ id: `t${position}`, ...t, position }))
      : prev.tiers,
    items: input.items
      ? input.items.map((i, position) => ({ id: `i${position}`, image_url: null, ...i, position }))
      : prev.items,
  };
}

// Mocks follow api/openapi.yaml (tierlists tag).
export const tierlistEditorHandlers = [
  http.post('/api/v1/tierlists', async ({ request }) => {
    const input = (await request.json()) as Input;
    const slug = input.visibility === 'public' ? (input.slug ?? '') : 'unlisted-random-slug-0001';
    if (store.has(slug)) {
      return err(422, 'validation_failed', 'Проверьте поля формы', {
        fields: { slug: 'Этот адрес уже занят' },
      });
    }
    const created = build(slug, input);
    store.set(slug, created);
    return HttpResponse.json(created, { status: 201 });
  }),
  http.get('/api/v1/tierlists/:slug', ({ params }) => {
    const t = store.get(String(params.slug));
    // Unknown slugs fall through to the next handler (fixtures of other features).
    return t ? HttpResponse.json(t) : undefined;
  }),
  http.patch('/api/v1/tierlists/:slug', async ({ params, request }) => {
    const t = store.get(String(params.slug));
    if (!t) return err(404, 'not_found', 'Не найдено');
    if (!t.is_owner) return err(403, 'forbidden', 'Нет прав');
    const patch = (await request.json()) as Patch;
    if ((patch.items || patch.tiers) && t.submissions_count > 0) {
      return err(409, 'tierlist_locked', 'Опрос уже проходили, структуру менять нельзя');
    }
    const updated = build(t.slug, patch, t);
    store.set(t.slug, updated);
    return HttpResponse.json(updated);
  }),
  http.post('/api/v1/tierlists/:slug/close', ({ params }) => {
    const t = store.get(String(params.slug));
    if (!t) return err(404, 'not_found', 'Не найдено');
    const closed: Tierlist = { ...t, status: 'closed', closed_at: '2026-10-22T10:00:00Z' };
    store.set(t.slug, closed);
    return HttpResponse.json(closed);
  }),
  http.delete('/api/v1/tierlists/:slug', ({ params }) => {
    const slug = String(params.slug);
    if (!store.delete(slug)) return err(404, 'not_found', 'Не найдено');
    return new HttpResponse(null, { status: 204 });
  }),
];
