import type { FieldErrors } from './api';

export const PASSWORD_MIN = 10;
export const PASSWORD_MAX = 128;
export const EMAIL_MAX = 254;
export const NAME_MAX = 50;

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function validateEmail(value: string): string | undefined {
  const v = value.trim();
  if (!v) return 'Укажите email';
  if (v.length > EMAIL_MAX || !EMAIL_RE.test(v)) return 'Введите корректный email';
  return undefined;
}

export function validateNewPassword(value: string): string | undefined {
  if (value.length < PASSWORD_MIN || value.length > PASSWORD_MAX) {
    return `Длина пароля от ${PASSWORD_MIN} до ${PASSWORD_MAX} символов`;
  }
  return undefined;
}

export function validateLoginPassword(value: string): string | undefined {
  if (!value) return 'Введите пароль';
  if (value.length > PASSWORD_MAX) return `Пароль не длиннее ${PASSWORD_MAX} символов`;
  return undefined;
}

export function validateDisplayName(value: string): string | undefined {
  const v = value.trim();
  if (!v) return 'Укажите имя';
  if (v.length > NAME_MAX) return `Имя не длиннее ${NAME_MAX} символов`;
  return undefined;
}

/** Drop undefined entries so an empty result means "valid". */
export function compact(errors: Record<string, string | undefined>): FieldErrors {
  const out: FieldErrors = {};
  for (const [k, v] of Object.entries(errors)) if (v) out[k] = v;
  return out;
}
