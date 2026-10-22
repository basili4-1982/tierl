/** 37.5 -> "37,5%", 40 -> "40%". */
export function formatPercent(value: number): string {
  const rounded = Math.round(value * 10) / 10;
  return `${String(rounded).replace('.', ',')}%`;
}

/** Similarity 0..1 as a percent string. */
export const formatSimilarity = (s: number): string => formatPercent(s * 100);
