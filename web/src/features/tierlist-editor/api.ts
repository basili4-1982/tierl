import { API_BASE, ApiError, getCsrfToken } from '../../api/client';
import type { components } from '../../api/schema';

export type Tierlist = components['schemas']['Tierlist'];
// The contract's TierlistInput has no slug yet; the editor sends one for public lists (see PR notes).
export type CreateBody = components['schemas']['TierlistInput'] & { slug?: string };
export type PatchBody = components['schemas']['TierlistPatch'];

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {};
  if (method !== 'GET') {
    headers['X-CSRF-Token'] = await getCsrfToken();
  }
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

export const getTierlist = (slug: string) => call<Tierlist>('GET', `/tierlists/${enc(slug)}`);
export const createTierlist = (b: CreateBody) => call<Tierlist>('POST', '/tierlists', b);
export const updateTierlist = (slug: string, b: PatchBody) =>
  call<Tierlist>('PATCH', `/tierlists/${enc(slug)}`, b);
export const closeTierlist = (slug: string) =>
  call<Tierlist>('POST', `/tierlists/${enc(slug)}/close`);
export const deleteTierlist = (slug: string) => call<void>('DELETE', `/tierlists/${enc(slug)}`);

/** Uploads an element picture and returns its public URL. */
export async function uploadMedia(file: File): Promise<string> {
  const headers: Record<string, string> = { 'X-CSRF-Token': await getCsrfToken() };
  const body = new FormData();
  body.append('file', file);
  const res = await fetch(`${API_BASE}/media`, {
    method: 'POST',
    credentials: 'include',
    headers,
    body,
  });
  if (!res.ok) {
    throw new ApiError(res.status, await res.json().catch(() => undefined));
  }
  const data = (await res.json()) as { url?: string };
  if (!data.url) throw new ApiError(500, undefined);
  return data.url;
}

/** Aliases kept for callers that name the operation after its result. */
export const uploadImage = uploadMedia;
export const uploadItemImage = uploadMedia;

/** Extracts per-field messages from `details.fields` of an API error. */
export function apiFieldErrors(err: ApiError): Record<string, string> {
  const fields = (err.body?.details as { fields?: unknown } | undefined)?.fields;
  if (!fields || typeof fields !== 'object') return {};
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(fields)) {
    if (typeof v === 'string') out[k] = v;
  }
  return out;
}
