import type { components } from '../../api/schema';
import { API_BASE, ApiError, getCsrfToken } from '../../api/client';

export type User = components['schemas']['User'];
export type RegisterRequest = components['schemas']['RegisterRequest'];
export type LoginRequest = components['schemas']['LoginRequest'];

let csrfToken: Promise<string> | null = null;

/** Forget the cached CSRF token (after logout or a csrf_failed answer). */
export function resetCsrfToken() {
  csrfToken = null;
}

function csrf(): Promise<string> {
  csrfToken ??= getCsrfToken().catch((err: unknown) => {
    csrfToken = null;
    throw err;
  });
  return csrfToken;
}

async function parseError(res: Response): Promise<ApiError> {
  const body = (await res.json().catch(() => undefined)) as ApiError['body'];
  return new ApiError(res.status, body);
}

async function send(method: string, path: string, body?: unknown, retry = true): Promise<Response> {
  const headers: Record<string, string> = { 'X-CSRF-Token': await csrf() };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  const res = await fetch(`${API_BASE}${path}`, {
    method,
    headers,
    credentials: 'include',
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.ok) return res;
  const err = await parseError(res);
  if (retry && err.status === 403 && err.body?.code === 'csrf_failed') {
    resetCsrfToken();
    return send(method, path, body, false);
  }
  throw err;
}

async function sendJson<T>(method: string, path: string, body?: unknown): Promise<T> {
  return (await send(method, path, body)).json() as Promise<T>;
}

/** GET /auth/me; null when there is no session. */
export async function fetchCurrentUser(): Promise<User | null> {
  const res = await fetch(`${API_BASE}/auth/me`, { credentials: 'include' });
  if (res.status === 401) return null;
  if (!res.ok) throw await parseError(res);
  return (await res.json()) as User;
}

export const register = (req: RegisterRequest) => sendJson<User>('POST', '/auth/register', req);
export const login = (req: LoginRequest) => sendJson<User>('POST', '/auth/login', req);
export const logout = async () => {
  await send('POST', '/auth/logout');
  resetCsrfToken();
};
export const verifyEmail = async (token: string) => {
  await send('POST', '/auth/verify-email', { token });
};
export const forgotPassword = async (email: string) => {
  await send('POST', '/auth/forgot-password', { email });
};
export const resetPassword = async (token: string, newPassword: string) => {
  await send('POST', '/auth/reset-password', { token, new_password: newPassword });
};

export type FieldErrors = Record<string, string>;

export interface ErrorView {
  message: string;
  fields: FieldErrors;
}

/** Turn any thrown value into a message plus per-field errors (details.fields). */
export function describeError(err: unknown): ErrorView {
  if (err instanceof ApiError) {
    const raw = err.body?.details?.fields;
    const fields: FieldErrors = {};
    if (raw && typeof raw === 'object') {
      for (const [k, v] of Object.entries(raw)) if (typeof v === 'string') fields[k] = v;
    }
    if (err.status === 429) {
      return { message: 'Слишком много попыток. Попробуйте чуть позже.', fields };
    }
    return {
      message: err.body?.message ?? 'Не удалось выполнить запрос. Попробуйте ещё раз.',
      fields,
    };
  }
  return { message: 'Нет связи с сервером. Проверьте соединение и повторите.', fields: {} };
}
