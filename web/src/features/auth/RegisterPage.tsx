import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { register } from './api';
import { AuthForm, TextField } from './AuthForm';
import { ME_KEY } from './session';
import { compact, validateDisplayName, validateEmail, validateNewPassword } from './validation';
import { useFormFeedback } from './useFormFeedback';

export function RegisterPage() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const fb = useFormFeedback();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [name, setName] = useState('');

  const mutation = useMutation({
    mutationFn: register,
    onSuccess: (user) => {
      qc.setQueryData(ME_KEY, user);
      void navigate('/', { replace: true });
    },
    onError: (err) => fb.fail(err),
  });

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
