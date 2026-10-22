import { Link } from 'react-router-dom';
import { useCurrentUser } from '../features/auth/session';

export function HomePage() {
  const { data: user, isPending } = useCurrentUser();
  const guest = !isPending && !user;
  return (
    <main>
      <h1>Tierl</h1>
      <p>Опросы-тирлисты: расставьте элементы по тирам и сравните с другими.</p>
      <p className="cta">
        <Link
          to={guest ? '/login' : '/tierlists/new'}
          state={guest ? { from: '/tierlists/new' } : undefined}
          className="button"
        >
          Создать опрос
        </Link>
        <Link to="/catalog" className="button button--secondary">
          Смотреть каталог
        </Link>
      </p>
    </main>
  );
}
