import { Route, Routes } from 'react-router-dom';
import { authRoutes } from './features/auth/routes';
import { HomePage } from './pages/HomePage';
import { NotFoundPage } from './pages/NotFoundPage';

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<HomePage />} />
      {authRoutes()}
      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  );
}
