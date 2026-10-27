import { http, HttpResponse } from 'msw';
import { takeTierlistHandlers } from '../features/take-tierlist/handlers';
import type { components } from '../api/schema';
import { authHandlers } from '../features/auth/mocks';
import { resultHandlers } from '../features/result/handlers';
import { tierlistEditorHandlers } from '../features/tierlist-editor/mocks';
import { profileHandlers } from '../features/profile/mocks';

type CsrfToken = components['schemas']['CsrfToken'];

// Mocks follow api/openapi.yaml. Extend as the screens appear.
export const handlers = [
  http.get('/api/v1/csrf', () => HttpResponse.json<CsrfToken>({ csrf_token: 'test-csrf-token' })),
  ...authHandlers,
  // Profile before the editor: it owns /me and passes unknown IDs on.
  ...profileHandlers,
  // Editor first: it answers for lists created in the session and passes the rest on.
  ...tierlistEditorHandlers,
  ...takeTierlistHandlers,
  ...resultHandlers,
];
