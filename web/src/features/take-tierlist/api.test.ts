import { http, HttpResponse } from 'msw';
import { server } from '../../mocks/server';
import { ApiError } from '../../api/client';
import { sampleSubmission } from './handlers';
import { fetchMySubmission, fetchTierlist, saveSubmission } from './api';

const placements = [{ item_id: 'i', tier_id: 't' }];

describe('take-tierlist api', () => {
  it('loads a tierlist and rejects with ApiError on 404', async () => {
    expect((await fetchTierlist('languages')).slug).toBe('languages');
    await expect(fetchTierlist('nope')).rejects.toMatchObject({ status: 404 });
  });

  it('treats 404 of /mine as no answer and other errors as failures', async () => {
    expect(await fetchMySubmission('languages')).toBeNull();
    server.use(
      http.get('/api/v1/tierlists/:slug/submissions/mine', () =>
        HttpResponse.json(sampleSubmission),
      ),
    );
    expect((await fetchMySubmission('languages'))?.id).toBe(sampleSubmission.id);
    server.use(
      http.get(
        '/api/v1/tierlists/:slug/submissions/mine',
        () => new HttpResponse(null, { status: 500 }),
      ),
    );
    await expect(fetchMySubmission('languages')).rejects.toBeInstanceOf(ApiError);
  });

  it('sends the CSRF header and the placements', async () => {
    let seen: { csrf: string | null; body: unknown; method: string } | undefined;
    server.use(
      http.post('/api/v1/tierlists/:slug/submissions', async ({ request }) => {
        seen = {
          csrf: request.headers.get('X-CSRF-Token'),
          body: await request.json(),
          method: request.method,
        };
        return HttpResponse.json(sampleSubmission, { status: 201 });
      }),
    );
    await saveSubmission('languages', placements, false);
    expect(seen).toEqual({ csrf: 'test-csrf-token', body: { placements }, method: 'POST' });
  });

  it('uses PUT when a previous answer exists', async () => {
    let method = '';
    server.use(
      http.put('/api/v1/tierlists/:slug/submissions/mine', ({ request }) => {
        method = request.method;
        return HttpResponse.json(sampleSubmission);
      }),
    );
    await saveSubmission('languages', placements, true);
    expect(method).toBe('PUT');
  });

  it('falls back to PUT on already_submitted and rethrows other errors', async () => {
    server.use(
      http.post('/api/v1/tierlists/:slug/submissions', () =>
        HttpResponse.json({ code: 'already_submitted', message: 'x' }, { status: 409 }),
      ),
    );
    expect((await saveSubmission('languages', placements, false)).id).toBe(sampleSubmission.id);
    server.use(
      http.post('/api/v1/tierlists/:slug/submissions', () =>
        HttpResponse.json({ code: 'tierlist_closed', message: 'x' }, { status: 409 }),
      ),
    );
    await expect(saveSubmission('languages', placements, false)).rejects.toMatchObject({
      status: 409,
    });
  });
});
