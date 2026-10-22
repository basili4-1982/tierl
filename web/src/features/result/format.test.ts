import { formatPercent, formatSimilarity } from './format';

it.each([
  [37.5, '37,5%'],
  [40, '40%'],
  [47.62, '47,6%'],
  [0, '0%'],
])('formats %s', (v, want) => expect(formatPercent(v)).toBe(want));

it('formats similarity', () => expect(formatSimilarity(0.8)).toBe('80%'));
