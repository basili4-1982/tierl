import { Navigate, Outlet, useLocation } from 'react-router-dom';
import { useCurrentUser } from './session';

/** Route wrapper: renders nested routes for signed-in users, else redirects to /login. */
export function RequireAuth() {
  const { data: user, isPending, isError, refetch } = useCurrentUser();
  const location = useLocation();

  if (isPending) return <p role="status">Загрузка…</p>;
  if (isError) {
    return (
      <main>
        <p role="alert">Не удалось загрузить профиль.</p>
        <button type="button" onClick={() => void refetch()}>
          Повторить
        </button>
      </main>
    );
  }
  if (!user)
    return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />;
  return <Outlet />;
}
