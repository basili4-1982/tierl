import { http, HttpResponse } from 'msw';
import type { components } from '../../api/schema';
import { sampleSubmission, sampleTierlist } from '../take-tierlist/handlers';

type Result = components['schemas']['Result'];
type ResultStats = components['schemas']['ResultStats'];
type ApiError = components['schemas']['Error'];

const tid = (n: number) => sampleTierlist.tiers[n]!.id;

export const sampleResult: Result = { submission: sampleSubmission, tierlist: sampleTierlist };

export const sampleStats: ResultStats = {
  total_submissions: 42,
  insufficient_data: false,
  min_submissions: 20,
  like_you_percent: 37.5,
  similarity_threshold: 0.8,
  items: sampleTierlist.items.map((item, i) => {
    const counts = [
      [20, 15, 7],
      [5, 25, 12],
      [10, 10, 22],
      [0, 12, 30],
    ][i]!;
    return {
      item_id: item.id,
      label: item.label,
      distribution: counts.map((count, t) => ({
        tier_id: tid(t),
        count,
        percent: Math.round((count / 42) * 1000) / 10,
      })),
    };
  }),
  similar: [{ similarity: 0.97 }, { similarity: 0.92 }],
  dissimilar: [{ similarity: 0.12 }],
};

export const fewDataResultId = '00000000-0000-4000-8000-000000000999';

export const sampleFewStats: ResultStats = {
  ...sampleStats,
  total_submissions: 3,
  insufficient_data: true,
  like_you_percent: null,
  items: sampleStats.items.map((it) => ({ ...it, distribution: [] })),
  similar: [],
  dissimilar: [],
};

const notFound = (): Response =>
  HttpResponse.json<ApiError>({ code: 'not_found', message: 'Не найдено' }, { status: 404 });

// Mocks follow api/openapi.yaml (results). Unknown ids return undefined so other
// handlers may answer; the real 404 for unknown ids comes from the last one.
export const resultHandlers = [
  http.get('/api/v1/results/:id', ({ params }) => {
    if (params.id === sampleResult.submission.id) return HttpResponse.json(sampleResult);
    if (params.id === fewDataResultId)
      return HttpResponse.json({
        ...sampleResult,
        submission: { ...sampleSubmission, id: fewDataResultId },
      });
    return undefined;
  }),
  http.get('/api/v1/results/:id/stats', ({ params }) => {
    if (params.id === sampleResult.submission.id) return HttpResponse.json(sampleStats);
    if (params.id === fewDataResultId) return HttpResponse.json(sampleFewStats);
    return undefined;
  }),
  http.get('/api/v1/results/:id', () => notFound()),
  http.get('/api/v1/results/:id/stats', () => notFound()),
];
