import { http, HttpResponse } from 'msw';
import type { components } from '../../api/schema';
import { MOCK_CSRF } from '../auth/mocks';

type TierlistSummary = components['schemas']['TierlistSummary'];
type ApiErr = components['schemas']['Error'];

const author = { id: '0b6f0e9e-5c1a-4a53-9a0e-8a4d1d5b7c11', display_name: 'Anna' };

function seed(
  id: string,
  slug: string,
  title: string,
  status: 'open' | 'closed',
  submissions: number,
): TierlistSummary {
  return {
    id,
    slug,
    title,
    description: 'Расставьте по тирам',
    visibility: 'public',
    status,
    items_count: 3,
    submissions_count: submissions,
    cover_url: null,
    author,
    created_at: '2026-10-22T09:00:00Z',
  };
}

const store = new Map<string, TierlistSummary>();

export function resetProfileMock(): void {
  store.clear();
  for (const t of [
    seed('my-open', 'my-open-list', 'Открытый опрос', 'open', 3),
    seed('my-closed', 'my-closed-list', 'Закрытый опрос', 'closed', 7),
  ]) {
    store.set(t.id, t);
  }
}
resetProfileMock();

const err = (status: number, body: ApiErr) => HttpResponse.json(body, { status });

export const profileHandlers = [
  http.get('/api/v1/me/tierlists', () =>
    HttpResponse.json({ items: Array.from(store.values()), next_cursor: null }),
  ),

  http.patch('/api/v1/tierlists/:id', async ({ params, request }) => {
    const current = store.get(String(params.id));
    if (!current) return undefined;
    const body = (await request.json()) as { status?: string };
    if (body.status !== 'closed' && body.status !== 'public' && body.status !== 'open') {
      return err(400, { code: 'bad_request', message: 'Некорректный статус опроса' });
    }
    const updated: TierlistSummary = {
      ...current,
      status: body.status === 'closed' ? 'closed' : 'open',
    };
    store.set(updated.id, updated);
    return HttpResponse.json(updated);
  }),

  http.delete('/api/v1/tierlists/:id', ({ params }) => {
    const id = String(params.id);
    if (!store.delete(id)) return undefined;
    return new HttpResponse(null, { status: 204 });
  }),

  http.post('/api/v1/me/password', async ({ request }) => {
    if (request.headers.get('X-CSRF-Token') !== MOCK_CSRF) {
      return err(403, { code: 'csrf_failed', message: 'Неверный CSRF-токен' });
    }
    const body = (await request.json()) as { current_password?: string; new_password?: string };
    if (!body.new_password || body.new_password.length < 10) {
      return err(400, {
        code: 'validation_failed',
        message: 'Проверьте поля формы',
        details: { fields: { password: 'Длина пароля от 10 до 128 символов' } },
      });
    }
    return HttpResponse.json({ csrf_token: MOCK_CSRF, revoked_sessions: 1 });
  }),
];
