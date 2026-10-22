import { useMutation } from '@tanstack/react-query';
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { forgotPassword } from './api';
import { AuthForm, TextField } from './AuthForm';
import { compact, validateEmail } from './validation';
import { useFormFeedback } from './useFormFeedback';

export function ForgotPasswordPage() {
  const fb = useFormFeedback();
  const [email, setEmail] = useState('');
  const [sent, setSent] = useState(false);

  const mutation = useMutation({
    mutationFn: forgotPassword,
    onSuccess: () => setSent(true),
    onError: (err) => fb.fail(err),
  });

  if (sent) {
    return (
      <main>
        <h1>Проверьте почту</h1>
        <p role="status">
          Если адрес зарегистрирован, мы отправили письмо со ссылкой для сброса пароля.
        </p>
        <p>
          <Link to="/login">Вернуться ко входу</Link>
        </p>
      </main>
    );
  }

  return (
    <AuthForm
      title="Восстановление пароля"
      submitLabel="Отправить ссылку"
      pendingLabel="Отправляем…"
      pending={mutation.isPending}
      fieldErrors={fb.fieldErrors}
      formError={fb.formError}
      onSubmit={() => {
        if (fb.invalid(compact({ email: validateEmail(email) }))) return;
        mutation.mutate(email.trim());
      }}
      footer={<Link to="/login">Вернуться ко входу</Link>}
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
    </AuthForm>
  );
}
