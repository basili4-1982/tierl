import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { describeError, verifyEmail } from './api';
import { ME_KEY } from './session';

export function VerifyEmailPage() {
  const qc = useQueryClient();
  const [params] = useSearchParams();
  const token = params.get('token') ?? '';

  // A query (not a mutation) so StrictMode's double mount does not spend the one-time token twice.
  const { isPending, isError, isSuccess, error } = useQuery({
    queryKey: ['auth', 'verify-email', token],
    queryFn: async () => {
      await verifyEmail(token);
      return true;
    },
    enabled: token !== '',
    retry: false,
    staleTime: Infinity,
    gcTime: Infinity,
    refetchOnWindowFocus: false,
  });

  useEffect(() => {
    if (isSuccess) void qc.invalidateQueries({ queryKey: ME_KEY });
  }, [isSuccess, qc]);

  let body;
  if (!token) {
    body = <p role="alert">Ссылка недействительна: в ней нет токена.</p>;
  } else if (isError) {
    body = <p role="alert">{describeError(error).message}</p>;
  } else if (isPending) {
    body = <p role="status">Подтверждаем email…</p>;
  } else {
    body = <p role="status">Email подтверждён. Теперь можно создавать опросы.</p>;
  }

  return (
    <main>
      <h1>Подтверждение email</h1>
      {body}
      <p>
        <Link to="/">На главную</Link>
      </p>
    </main>
  );
}
