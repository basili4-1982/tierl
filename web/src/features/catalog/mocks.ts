import { http, HttpResponse } from 'msw';
import type { components } from '../../api/schema';

type TierlistSummary = components['schemas']['TierlistSummary'];

const author = { id: '00000000-0000-4000-8000-000000000009', display_name: 'Anna' };

export const sampleCatalogItem: TierlistSummary = {
  id: '00000000-0000-4000-8000-000000000001',
  slug: 'languages',
  title: 'Языки программирования',
  description: 'Расставьте языки по тирам.',
  visibility: 'public',
  status: 'open',
  items_count: 4,
  submissions_count: 42,
  cover_url: null,
  author,
  created_at: '2026-10-22T09:00:00Z',
};

export const sampleUnlistedItem: TierlistSummary = {
  ...sampleCatalogItem,
  id: '00000000-0000-4000-8000-000000000002',
  slug: 'xK3pQ9vTz2Lm',
  title: 'Секретный опрос',
  visibility: 'unlisted',
};

export const catalogHandlers = [
  http.get('/api/v1/catalog', () =>
    HttpResponse.json({ items: [sampleCatalogItem], next_cursor: null }),
  ),
];
