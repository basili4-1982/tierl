import { API_BASE, ApiError } from '../../api/client';
import type { components } from '../../api/schema';

export type TierlistSummary = components['schemas']['TierlistSummary'];
export type TierlistPage = components['schemas']['TierlistPage'];
export type CatalogSort = 'new' | 'popular';

export interface CatalogParams {
  sort?: CatalogSort;
  q?: string;
  limit?: number;
  cursor?: string;
}

export async function fetchCatalog(params: CatalogParams = {}): Promise<TierlistPage> {
  const search = new URLSearchParams();
  if (params.sort) search.set('sort', params.sort);
  if (params.q) search.set('q', params.q);
  if (params.limit) search.set('limit', String(params.limit));
  if (params.cursor) search.set('cursor', params.cursor);
  const query = search.toString();
  const res = await fetch(`${API_BASE}/catalog${query ? `?${query}` : ''}`, {
    credentials: 'include',
  });
  if (!res.ok) {
    throw new ApiError(res.status, await res.json().catch(() => undefined));
  }
  return (await res.json()) as TierlistPage;
}
