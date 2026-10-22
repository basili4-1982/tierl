import { useState } from 'react';
import { describeError, type FieldErrors } from './api';

/** Field and form-level error state shared by the auth forms. */
export function useFormFeedback() {
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [formError, setFormError] = useState<string | null>(null);

  return {
    fieldErrors,
    formError,
    clear() {
      setFieldErrors({});
      setFormError(null);
    },
    /** Client-side validation failure. Returns true when there were errors. */
    invalid(errors: FieldErrors) {
      const has = Object.keys(errors).length > 0;
      setFieldErrors(errors);
      setFormError(null);
      return has;
    },
    /** Server or network failure. */
    fail(err: unknown, fallbackField?: Record<string, string>) {
      const { message, fields } = describeError(err);
      const mapped: FieldErrors = {};
      for (const [k, v] of Object.entries(fields)) mapped[fallbackField?.[k] ?? k] = v;
      setFieldErrors(mapped);
      setFormError(Object.keys(mapped).length > 0 ? null : message);
    },
  };
}
