import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { resetPassword } from './api';
import { AuthForm, TextField } from './AuthForm';
import { ME_KEY } from './session';
import { compact, validateNewPassword } from './validation';
import { useFormFeedback } from './useFormFeedback';

export function ResetPasswordPage() {
  const qc = useQueryClient();
  const [params] = useSearchParams();
  const token = params.get('token') ?? '';
  const fb = useFormFeedback();
  const [password, setPassword] = useState('');
  const [done, setDone] = useState(false);

  const mutation = useMutation({
    mutationFn: (newPassword: string) => resetPassword(token, newPassword),
    onSuccess: () => {
      // The server revokes every session on a password change.
      qc.setQueryData(ME_KEY, null);
      setDone(true);
    },
    onError: (err) => fb.fail(err, { password: 'new_password' }),
  });

  if (!token) {
    return (
      <main>
        <h1>Новый пароль</h1>
        <p role="alert">Ссылка недействительна: в ней нет токена.</p>
        <p>
          <Link to="/forgot-password">Запросить новую ссылку</Link>
        </p>
      </main>
    );
  }

  if (done) {
    return (
      <main>
        <h1>Пароль изменён</h1>
        <p role="status">Теперь можно войти с новым паролем.</p>
        <p>
          <Link to="/login">Войти</Link>
        </p>
      </main>
    );
  }

  return (
    <AuthForm
      title="Новый пароль"
      submitLabel="Сохранить пароль"
      pendingLabel="Сохраняем…"
      pending={mutation.isPending}
      fieldErrors={fb.fieldErrors}
      formError={fb.formError}
      onSubmit={() => {
        if (fb.invalid(compact({ new_password: validateNewPassword(password) }))) return;
        mutation.mutate(password);
      }}
      footer={<Link to="/forgot-password">Запросить новую ссылку</Link>}
    >
      <TextField
        label="Новый пароль"
        name="new_password"
        type="password"
        autoComplete="new-password"
        hint="От 10 до 128 символов"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        error={fb.fieldErrors.new_password}
      />
    </AuthForm>
  );
}
