import { isValidColor } from './validation';

/** Black or white, whichever reads better on the given #rrggbb background. */
export function readableText(color: string): string {
  if (!isValidColor(color)) return '#000000';
  const n = parseInt(color.slice(1), 16);
  const [r, g, b] = [(n >> 16) & 255, (n >> 8) & 255, n & 255];
  return (r * 299 + g * 587 + b * 114) / 1000 > 140 ? '#000000' : '#ffffff';
}
