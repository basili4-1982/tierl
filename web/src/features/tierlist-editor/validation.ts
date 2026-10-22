// Client-side mirror of internal/domain validation rules (docs/SPEC.md section 3).
// The Go constants are the source of truth; keep these in sync.
export const LIMITS = {
  minItems: 3,
  maxItems: 50,
  minTiers: 2,
  maxTiers: 10,
  maxTitle: 120,
  maxDescription: 1000,
  maxItemLabel: 80,
  // domain allows 40, api/openapi.yaml allows 30: use the stricter one so both accept it.
  maxTierName: 30,
  maxImageUrl: 2048,
  minSlug: 3,
  maxSlug: 64,
  minUnlistedSlug: 12,
} as const;

export type Visibility = 'public' | 'unlisted';

export interface DraftItem {
  key: string;
  label: string;
  imageUrl: string;
}

export interface DraftTier {
  key: string;
  name: string;
  color: string;
}

export interface Draft {
  title: string;
  description: string;
  visibility: Visibility;
  slug: string;
  items: DraftItem[];
  tiers: DraftTier[];
}

/**
 * Errors by field path: `title`, `description`, `slug`, `items`, `tiers`,
 * `items.<i>.label`, `items.<i>.image_url`, `tiers.<i>.name`, `tiers.<i>.color`.
 */
export type FieldErrors = Record<string, string>;

const COLOR_RE = /^#[0-9a-fA-F]{6}$/;
const SLUG_RE = /^[A-Za-z0-9_-]+$/;

// Counts Unicode code points, like utf8.RuneCountInString.
const runes = (s: string) => Array.from(s).length;

export function isValidColor(color: string): boolean {
  return COLOR_RE.test(color);
}

function isHttpUrl(s: string): boolean {
  try {
    const u = new URL(s);
    return u.protocol === 'http:' || u.protocol === 'https:';
  } catch {
    return false;
  }
}

export function validateSlug(slug: string): string | undefined {
  if (slug.length < LIMITS.minSlug || slug.length > LIMITS.maxSlug) {
    return `Длина адреса от ${LIMITS.minSlug} до ${LIMITS.maxSlug} символов`;
  }
  if (!SLUG_RE.test(slug)) {
    return 'Допустимы латинские буквы, цифры, _ и -';
  }
  return undefined;
}

export function validateDraft(d: Draft): FieldErrors {
  const e: FieldErrors = {};

  if (d.title.trim() === '') e.title = 'Введите название';
  else if (runes(d.title) > LIMITS.maxTitle) e.title = `Не длиннее ${LIMITS.maxTitle} символов`;

  if (runes(d.description) > LIMITS.maxDescription) {
    e.description = `Не длиннее ${LIMITS.maxDescription} символов`;
  }

  if (d.visibility === 'public') {
    const msg = validateSlug(d.slug);
    if (msg) e.slug = msg;
  }

  if (d.items.length < LIMITS.minItems || d.items.length > LIMITS.maxItems) {
    e.items = `Элементов от ${LIMITS.minItems} до ${LIMITS.maxItems}, сейчас ${d.items.length}`;
  }
  d.items.forEach((it, i) => {
    if (it.label.trim() === '') e[`items.${i}.label`] = 'Введите название элемента';
    else if (runes(it.label) > LIMITS.maxItemLabel) {
      e[`items.${i}.label`] = `Не длиннее ${LIMITS.maxItemLabel} символов`;
    }
    const url = it.imageUrl.trim();
    if (url !== '') {
      if (url.length > LIMITS.maxImageUrl) {
        e[`items.${i}.image_url`] = `Ссылка не длиннее ${LIMITS.maxImageUrl} символов`;
      } else if (!isHttpUrl(url)) {
        e[`items.${i}.image_url`] = 'Нужна ссылка http:// или https://';
      }
    }
  });

  if (d.tiers.length < LIMITS.minTiers || d.tiers.length > LIMITS.maxTiers) {
    e.tiers = `Тиров от ${LIMITS.minTiers} до ${LIMITS.maxTiers}, сейчас ${d.tiers.length}`;
  }
  d.tiers.forEach((t, i) => {
    if (t.name.trim() === '') e[`tiers.${i}.name`] = 'Введите название тира';
    else if (runes(t.name) > LIMITS.maxTierName) {
      e[`tiers.${i}.name`] = `Не длиннее ${LIMITS.maxTierName} символов`;
    }
    if (!isValidColor(t.color)) e[`tiers.${i}.color`] = 'Цвет в формате #rrggbb';
  });

  return e;
}
