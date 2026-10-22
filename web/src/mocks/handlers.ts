import { http, HttpResponse } from 'msw';
import type { components } from '../api/schema';

type CsrfToken = components['schemas']['CsrfToken'];

// Mocks follow api/openapi.yaml. Extend as the screens appear.
export const handlers = [
  http.get('/api/v1/csrf', () => HttpResponse.json<CsrfToken>({ csrf_token: 'test-csrf-token' })),
];
