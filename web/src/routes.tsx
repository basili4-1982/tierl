import { Route, Routes } from 'react-router-dom';
import { authRoutes } from './features/auth/routes';
import { ResultPage } from './features/result/ResultPage';
import { TakeTierlistPage } from './features/take-tierlist/TakeTierlistPage';
import { TierlistEditorPage } from './features/tierlist-editor/TierlistEditorPage';
import { Layout } from './layout/Layout';
import { CatalogPage } from './pages/CatalogPage';
import { HomePage } from './pages/HomePage';
import { NotFoundPage } from './pages/NotFoundPage';

export function AppRoutes() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<HomePage />} />
        <Route path="/catalog" element={<CatalogPage />} />
        {authRoutes()}
        <Route path="/t/:slug" element={<TakeTierlistPage />} />
        <Route path="/r/:id" element={<ResultPage />} />
        <Route path="/tierlists/new" element={<TierlistEditorPage />} />
        <Route path="/tierlists/:slug/edit" element={<TierlistEditorPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  );
}
