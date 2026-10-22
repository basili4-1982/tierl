import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { login } from './api';
import { AuthForm, TextField } from './AuthForm';
import { ME_KEY } from './session';
import { compact, validateEmail, validateLoginPassword } from './validation';
import { useFormFeedback } from './useFormFeedback';

function redirectTarget(state: unknown): string {
  const from = (state as { from?: unknown } | null)?.from;
  // Only same-origin absolute paths, never "//host" or full URLs.
  return typeof from === 'string' && from.startsWith('/') && !from.startsWith('//') ? from : '/';
}

export function LoginPage() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();
  const fb = useFormFeedback();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');

  const mutation = useMutation({
    mutationFn: login,
    onSuccess: (user) => {
      qc.setQueryData(ME_KEY, user);
      void navigate(redirectTarget(location.state), { replace: true });
    },
    onError: (err) => fb.fail(err),
  });

  return (
    <AuthForm
      title="Вход"
      submitLabel="Войти"
      pendingLabel="Входим…"
      pending={mutation.isPending}
      fieldErrors={fb.fieldErrors}
      formError={fb.formError}
      onSubmit={() => {
        const errors = compact({
          email: validateEmail(email),
          password: validateLoginPassword(password),
        });
        if (fb.invalid(errors)) return;
        mutation.mutate({ email: email.trim(), password });
      }}
      footer={
        <>
          <Link to="/forgot-password">Забыли пароль?</Link> ·{' '}
          <Link to="/register">Регистрация</Link>
        </>
      }
    >
      <TextField
        label="Email"
        name="email"
        type="email"
        autoComplete="email"
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        error={fb.fieldErrors.email}
      />
      <TextField
        label="Пароль"
        name="password"
        type="password"
        autoComplete="current-password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        error={fb.fieldErrors.password}
      />
    </AuthForm>
  );
}
