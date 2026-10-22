import {
  useEffect,
  useId,
  useRef,
  type FormEvent,
  type InputHTMLAttributes,
  type ReactNode,
} from 'react';
import './auth.css';
import type { FieldErrors } from './api';

interface TextFieldProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'id'> {
  label: string;
  error?: string;
  hint?: string;
}

export function TextField({ label, error, hint, ...input }: TextFieldProps) {
  const id = useId();
  const hintId = `${id}-hint`;
  const errorId = `${id}-error`;
  const describedBy = [hint ? hintId : '', error ? errorId : ''].filter(Boolean).join(' ');
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <input
        {...input}
        id={id}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy || undefined}
      />
      {hint && (
        <small id={hintId} className="hint">
          {hint}
        </small>
      )}
      {error && (
        <small id={errorId} className="error">
          {error}
        </small>
      )}
    </div>
  );
}

interface AuthFormProps {
  title: string;
  submitLabel: string;
  pendingLabel: string;
  pending: boolean;
  fieldErrors: FieldErrors;
  formError: string | null;
  onSubmit: () => void;
  children: ReactNode;
  footer?: ReactNode;
}

/** Form shell: noValidate, busy state, and focus moved to the first error after a failed submit. */
export function AuthForm(props: AuthFormProps) {
  const { title, submitLabel, pendingLabel, pending, fieldErrors, formError, onSubmit } = props;
  const formRef = useRef<HTMLFormElement>(null);
  const alertRef = useRef<HTMLParagraphElement>(null);

  useEffect(() => {
    const invalid = formRef.current?.querySelector<HTMLElement>('[aria-invalid="true"]');
    if (invalid) invalid.focus();
    else alertRef.current?.focus();
  }, [fieldErrors, formError]);

  function handle(e: FormEvent) {
    e.preventDefault();
    if (!pending) onSubmit();
  }

  return (
    <main>
      <h1>{title}</h1>
      <form ref={formRef} noValidate onSubmit={handle} aria-busy={pending}>
        {formError && (
          <p ref={alertRef} role="alert" tabIndex={-1} className="error">
            {formError}
          </p>
        )}
        {props.children}
        <button type="submit" disabled={pending}>
          {pending ? pendingLabel : submitLabel}
        </button>
      </form>
      {props.footer && <p className="links">{props.footer}</p>}
    </main>
  );
}
