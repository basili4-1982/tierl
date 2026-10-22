import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { AppRoutes } from './routes';

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <AppRoutes />
    </MemoryRouter>,
  );
}

describe('routes', () => {
  it('renders the home page stub', () => {
    renderAt('/');
    expect(screen.getByRole('heading', { name: 'Tierl' })).toBeInTheDocument();
  });

  it('renders 404 for unknown paths with a link home', () => {
    renderAt('/no/such/page');
    expect(screen.getByRole('heading', { name: 'Страница не найдена' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'На главную' })).toHaveAttribute('href', '/');
  });
});
