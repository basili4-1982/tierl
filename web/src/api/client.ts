import type { components } from './schema';

export const API_BASE = '/api/v1';

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly body: components['schemas']['Error'] | undefined,
  ) {
    super(body?.message ?? `HTTP ${status}`);
    this.name = 'ApiError';
  }
}

export async function getCsrfToken(): Promise<string> {
  const res = await fetch(`${API_BASE}/csrf`, { credentials: 'include' });
  if (!res.ok) {
    throw new ApiError(res.status, await res.json().catch(() => undefined));
  }
  const data = (await res.json()) as components['schemas']['CsrfToken'];
  return data.csrf_token;
}
