import { Route, Routes } from 'react-router-dom';
import { authRoutes } from './features/auth/routes';
import { TakeTierlistPage } from './features/take-tierlist/TakeTierlistPage';
import { HomePage } from './pages/HomePage';
import { NotFoundPage } from './pages/NotFoundPage';

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<HomePage />} />
      {authRoutes()}
      <Route path="/t/:slug" element={<TakeTierlistPage />} />
      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  );
}
