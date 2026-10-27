import type { components } from '../../api/schema';
import { API_BASE, ApiError, getCsrfToken } from '../../api/client';

export type TierlistSummary = components['schemas']['TierlistSummary'];
export type TierlistPage = components['schemas']['TierlistPage'];

export interface PasswordChangeResponse {
  csrf_token: string;
  revoked_sessions?: number;
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {};
  if (method !== 'GET') headers['X-CSRF-Token'] = await getCsrfToken();
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  const res = await fetch(`${API_BASE}${path}`, {
    method,
    credentials: 'include',
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    throw new ApiError(res.status, await res.json().catch(() => undefined));
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

const enc = encodeURIComponent;

export const fetchMyTierlists = () => call<TierlistPage>('GET', '/me/tierlists');

/** Reopen ('public') or close ('closed') a tier list by its stable ID. */
export const setTierlistStatus = (id: string, status: 'public' | 'closed') =>
  call<components['schemas']['Tierlist']>('PATCH', `/tierlists/${enc(id)}`, { status });

export const deleteTierlist = (id: string) => call<void>('DELETE', `/tierlists/${enc(id)}`);

export const changePassword = (currentPassword: string, newPassword: string) =>
  call<PasswordChangeResponse>('POST', '/me/password', {
    current_password: currentPassword,
    new_password: newPassword,
  });
