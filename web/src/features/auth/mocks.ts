import { http, HttpResponse } from 'msw';
import type { components } from '../../api/schema';

type User = components['schemas']['User'];
type ApiErr = components['schemas']['Error'];

// Stateful in-memory mock of the auth endpoints in api/openapi.yaml.
export const MOCK_CSRF = 'test-csrf-token';
export const VALID_TOKEN = 'valid-token';
export const SEED_EMAIL = 'anna@example.com';
export const SEED_PASSWORD = 'correct-horse-battery';

interface Account {
  user: User;
  password: string;
}

let accounts = new Map<string, Account>();
let sessionEmail: string | null = null;

export function resetAuthMock() {
  sessionEmail = null;
  accounts = new Map([
    [
      SEED_EMAIL,
      {
        password: SEED_PASSWORD,
        user: {
          id: '0b6f0e9e-5c1a-4a53-9a0e-8a4d1d5b7c11',
          email: SEED_EMAIL,
          display_name: 'Anna',
          email_verified: true,
          created_at: '2026-10-22T09:00:00Z',
        },
      },
    ],
  ]);
}
resetAuthMock();

const err = (status: number, body: ApiErr) => HttpResponse.json(body, { status });
const csrfFailed = () => err(403, { code: 'csrf_failed', message: 'Неверный CSRF-токен' });
const badPassword = (field: string) =>
  err(422, {
    code: 'validation_failed',
    message: 'Проверьте поля формы',
    details: { fields: { [field]: 'Длина пароля от 10 до 128 символов' } },
  });

const okCsrf = (req: Request) => req.headers.get('X-CSRF-Token') === MOCK_CSRF;
const goodPassword = (p: unknown) => typeof p === 'string' && p.length >= 10 && p.length <= 128;

export const authHandlers = [
  http.get('/api/v1/auth/me', () => {
    const acc = sessionEmail ? accounts.get(sessionEmail) : undefined;
    return acc
      ? HttpResponse.json(acc.user)
      : err(401, { code: 'unauthorized', message: 'Требуется вход' });
  }),

  http.post('/api/v1/auth/register', async ({ request }) => {
    if (!okCsrf(request)) return csrfFailed();
    const body = (await request.json()) as {
      email: string;
      password: string;
      display_name: string;
    };
    if (!goodPassword(body.password)) return badPassword('password');
    const email = body.email.toLowerCase();
    if (accounts.has(email)) {
      return err(409, { code: 'email_taken', message: 'Этот email уже зарегистрирован' });
    }
    const user: User = {
      id: crypto.randomUUID(),
      email,
      display_name: body.display_name,
      email_verified: false,
      created_at: new Date().toISOString(),
    };
    accounts.set(email, { user, password: body.password });
    sessionEmail = email;
    return HttpResponse.json(user, { status: 201 });
  }),

  http.post('/api/v1/auth/login', async ({ request }) => {
    if (!okCsrf(request)) return csrfFailed();
    const body = (await request.json()) as { email: string; password: string };
    const acc = accounts.get(body.email.toLowerCase());
    if (!acc || acc.password !== body.password) {
      return err(401, { code: 'invalid_credentials', message: 'Неверный email или пароль' });
    }
    sessionEmail = acc.user.email;
    return HttpResponse.json(acc.user);
  }),

  http.post('/api/v1/auth/logout', ({ request }) => {
    if (!okCsrf(request)) return csrfFailed();
    if (!sessionEmail) return err(401, { code: 'unauthorized', message: 'Требуется вход' });
    sessionEmail = null;
    return new HttpResponse(null, { status: 204 });
  }),

  http.post('/api/v1/auth/verify-email', async ({ request }) => {
    if (!okCsrf(request)) return csrfFailed();
    const { token } = (await request.json()) as { token: string };
    if (token !== VALID_TOKEN) {
      return err(400, { code: 'invalid_token', message: 'Ссылка недействительна или устарела' });
    }
    const acc = sessionEmail ? accounts.get(sessionEmail) : undefined;
    if (acc) acc.user.email_verified = true;
    return new HttpResponse(null, { status: 204 });
  }),

  http.post('/api/v1/auth/forgot-password', ({ request }) => {
    if (!okCsrf(request)) return csrfFailed();
    return new HttpResponse(null, { status: 202 });
  }),

  http.post('/api/v1/auth/reset-password', async ({ request }) => {
    if (!okCsrf(request)) return csrfFailed();
    const body = (await request.json()) as { token: string; new_password: string };
    if (body.token !== VALID_TOKEN) {
      return err(400, { code: 'invalid_token', message: 'Ссылка недействительна или устарела' });
    }
    if (!goodPassword(body.new_password)) return badPassword('new_password');
    sessionEmail = null;
    return new HttpResponse(null, { status: 204 });
  }),
];
