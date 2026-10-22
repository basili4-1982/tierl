import { http, HttpResponse } from 'msw';
import type { components } from '../../api/schema';

type Tierlist = components['schemas']['Tierlist'];
type Submission = components['schemas']['Submission'];
type ApiError = components['schemas']['Error'];

const id = (n: number) => `00000000-0000-4000-8000-${String(n).padStart(12, '0')}`;

export const sampleTierlist: Tierlist = {
  id: id(1),
  slug: 'languages',
  title: 'Языки программирования',
  description: 'Расставьте языки по тирам.',
  visibility: 'public',
  status: 'open',
  tiers: [
    { id: id(101), name: 'S', color: '#ff7f7f', position: 0 },
    { id: id(102), name: 'A', color: '#ffbf7f', position: 1 },
    { id: id(103), name: 'B', color: '#7fbfff', position: 2 },
  ],
  items: ['Go', 'Rust', 'Python', 'Java'].map((label, i) => ({
    id: id(201 + i),
    label,
    image_url: null,
    position: i,
  })),
  author: { id: id(9), display_name: 'Автор' },
  is_owner: false,
  submissions_count: 0,
  created_at: '2026-10-01T00:00:00Z',
  closed_at: null,
};

export const sampleSubmission: Submission = {
  id: id(301),
  tierlist_slug: 'languages',
  placements: [
    { item_id: id(201), tier_id: id(101) },
    { item_id: id(202), tier_id: id(102) },
    { item_id: id(203), tier_id: id(103) },
  ],
  created_at: '2026-10-02T00:00:00Z',
  updated_at: '2026-10-02T00:00:00Z',
};

const notFound = (): Response =>
  HttpResponse.json<ApiError>({ code: 'not_found', message: 'Не найдено' }, { status: 404 });

// Mocks follow api/openapi.yaml (tierlists, submissions). Slugs: "languages" is
// open, "old-poll" is closed, anything else is 404.
export const takeTierlistHandlers = [
  http.get('/api/v1/tierlists/:slug', ({ params }) => {
    if (params.slug === 'languages') return HttpResponse.json(sampleTierlist);
    if (params.slug === 'old-poll')
      return HttpResponse.json({ ...sampleTierlist, slug: 'old-poll', status: 'closed' });
    return notFound();
  }),
  http.get('/api/v1/tierlists/:slug/submissions/mine', () => notFound()),
  http.post('/api/v1/tierlists/:slug/submissions', () =>
    HttpResponse.json(sampleSubmission, { status: 201 }),
  ),
  http.put('/api/v1/tierlists/:slug/submissions/mine', () => HttpResponse.json(sampleSubmission)),
];
