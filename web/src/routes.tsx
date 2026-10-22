import { Route, Routes } from 'react-router-dom';
import { authRoutes } from './features/auth/routes';
import { TakeTierlistPage } from './features/take-tierlist/TakeTierlistPage';
import { TierlistEditorPage } from './features/tierlist-editor/TierlistEditorPage';
import { HomePage } from './pages/HomePage';
import { NotFoundPage } from './pages/NotFoundPage';

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<HomePage />} />
      {authRoutes()}
      <Route path="/t/:slug" element={<TakeTierlistPage />} />
      <Route path="/tierlists/new" element={<TierlistEditorPage />} />
      <Route path="/tierlists/:slug/edit" element={<TierlistEditorPage />} />
      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  );
}
