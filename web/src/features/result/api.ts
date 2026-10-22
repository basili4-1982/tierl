import { API_BASE, ApiError } from '../../api/client';
import type { components } from '../../api/schema';

export type Result = components['schemas']['Result'];
export type ResultStats = components['schemas']['ResultStats'];
export type Tierlist = components['schemas']['Tierlist'];

async function parse<T>(res: Response): Promise<T> {
  if (!res.ok) throw new ApiError(res.status, await res.json().catch(() => undefined));
  return (await res.json()) as T;
}

export async function fetchResult(id: string): Promise<Result> {
  return parse<Result>(
    await fetch(`${API_BASE}/results/${encodeURIComponent(id)}`, { credentials: 'include' }),
  );
}

export async function fetchResultStats(id: string): Promise<ResultStats> {
  return parse<ResultStats>(
    await fetch(`${API_BASE}/results/${encodeURIComponent(id)}/stats`, {
      credentials: 'include',
    }),
  );
}
