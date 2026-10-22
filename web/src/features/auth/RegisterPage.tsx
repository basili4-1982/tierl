import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { register } from './api';
import { AuthForm, TextField } from './AuthForm';
import { ME_KEY } from './session';
import { compact, validateDisplayName, validateEmail, validateNewPassword } from './validation';
import { useFormFeedback } from './useFormFeedback';

export function RegisterPage() {
  const qc = useQueryClient();
  const fb = useFormFeedback();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [name, setName] = useState('');
  const [done, setDone] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: register,
    onSuccess: (user) => {
      qc.setQueryData(ME_KEY, user);
      setDone(user.email);
    },
    onError: (err) => fb.fail(err),
  });

  if (done) {
    return (
      <main>
        <h1>Проверьте почту</h1>
        <p role="status">
          Мы отправили ссылку для подтверждения на {done}. Без подтверждения email создавать опросы
          нельзя.
        </p>
        <p>
          <Link to="/">На главную</Link>
        </p>
      </main>
    );
  }

  return (
    <AuthForm
      title="Регистрация"
      submitLabel="Зарегистрироваться"
      pendingLabel="Регистрируем…"
      pending={mutation.isPending}
      fieldErrors={fb.fieldErrors}
      formError={fb.formError}
      onSubmit={() => {
        const errors = compact({
          display_name: validateDisplayName(name),
          email: validateEmail(email),
          password: validateNewPassword(password),
        });
        if (fb.invalid(errors)) return;
        mutation.mutate({ email: email.trim(), password, display_name: name.trim() });
      }}
      footer={
        <>
          Уже есть аккаунт? <Link to="/login">Войти</Link>
        </>
      }
    >
      <TextField
        label="Имя"
        name="display_name"
        autoComplete="nickname"
        value={name}
        onChange={(e) => setName(e.target.value)}
        error={fb.fieldErrors.display_name}
      />
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
        autoComplete="new-password"
        hint="От 10 до 128 символов"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        error={fb.fieldErrors.password}
      />
    </AuthForm>
  );
}
