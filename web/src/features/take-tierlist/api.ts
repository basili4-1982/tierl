import { API_BASE, ApiError, getCsrfToken } from '../../api/client';
import type { components } from '../../api/schema';
import type { Placement } from './placement';

export type Tierlist = components['schemas']['Tierlist'];
export type Submission = components['schemas']['Submission'];

async function parse<T>(res: Response): Promise<T> {
  if (!res.ok) throw new ApiError(res.status, await res.json().catch(() => undefined));
  return (await res.json()) as T;
}

export async function fetchTierlist(slug: string): Promise<Tierlist> {
  const res = await fetch(`${API_BASE}/tierlists/${encodeURIComponent(slug)}`, {
    credentials: 'include',
  });
  return parse<Tierlist>(res);
}

/** Current anonymous session's answer, or null when there is none (404). */
export async function fetchMySubmission(slug: string): Promise<Submission | null> {
  const res = await fetch(`${API_BASE}/tierlists/${encodeURIComponent(slug)}/submissions/mine`, {
    credentials: 'include',
  });
  if (res.status === 404) return null;
  return parse<Submission>(res);
}

async function send(
  method: 'POST' | 'PUT',
  path: string,
  placements: Placement[],
): Promise<Submission> {
  const csrf = await getCsrfToken();
  const res = await fetch(path, {
    method,
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify({ placements }),
  });
  return parse<Submission>(res);
}

const base = (slug: string) => `${API_BASE}/tierlists/${encodeURIComponent(slug)}/submissions`;

export const createSubmission = (slug: string, placements: Placement[]) =>
  send('POST', base(slug), placements);

export const replaceMySubmission = (slug: string, placements: Placement[]) =>
  send('PUT', `${base(slug)}/mine`, placements);

/** First answer goes through POST, re-taking through PUT. */
export async function saveSubmission(
  slug: string,
  placements: Placement[],
  hasPrevious: boolean,
): Promise<Submission> {
  if (hasPrevious) return replaceMySubmission(slug, placements);
  try {
    return await createSubmission(slug, placements);
  } catch (e) {
    // The cookie already holds an answer we failed to load: replace it.
    if (e instanceof ApiError && e.status === 409 && e.body?.code === 'already_submitted') {
      return replaceMySubmission(slug, placements);
    }
    throw e;
  }
}
