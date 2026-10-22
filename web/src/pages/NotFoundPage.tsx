import { Link } from 'react-router-dom';

export function NotFoundPage() {
  return (
    <main>
      <h1>Страница не найдена</h1>
      <p>Такой страницы нет.</p>
      <Link to="/">На главную</Link>
    </main>
  );
}
