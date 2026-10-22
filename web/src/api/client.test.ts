import { http, HttpResponse } from 'msw';
import { server } from '../mocks/server';
import { ApiError, getCsrfToken } from './client';

describe('getCsrfToken', () => {
  it('returns the token from the mocked contract', async () => {
    await expect(getCsrfToken()).resolves.toBe('test-csrf-token');
  });

  it('throws ApiError with the contract error body', async () => {
    server.use(
      http.get('/api/v1/csrf', () =>
        HttpResponse.json({ code: 'rate_limited', message: 'slow down' }, { status: 429 }),
      ),
    );
    await expect(getCsrfToken()).rejects.toMatchObject({
      name: 'ApiError',
      status: 429,
      message: 'slow down',
    });
  });

  it('falls back to a status message when the body is not JSON', async () => {
    server.use(http.get('/api/v1/csrf', () => new HttpResponse('boom', { status: 500 })));
    const err = await getCsrfToken().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).message).toBe('HTTP 500');
  });
});
