import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { SEED_EMAIL, SEED_PASSWORD } from '../features/auth/mocks';
import { renderApp, resetAuthTestState } from '../features/auth/test-utils';
import { AppRoutes } from '../routes';
import { server } from '../mocks/server';
import { http, HttpResponse } from 'msw';

beforeEach(resetAuthTestState);

async function signIn() {
  const { login } = await import('../features/auth/api');
  await login({ email: SEED_EMAIL, password: SEED_PASSWORD });
}

const nav = () => screen.getByRole('navigation', { name: 'Основная' });

describe('Layout', () => {
  it('shows guest links and landmarks', async () => {
    renderApp(<AppRoutes />);
    expect(await within(nav()).findByRole('link', { name: 'Вход' })).toHaveAttribute(
      'href',
      '/login',
    );
    expect(within(nav()).getByRole('link', { name: 'Регистрация' })).toHaveAttribute(
      'href',
      '/register',
    );
    expect(within(nav()).getByRole('link', { name: 'Каталог' })).toHaveAttribute(
      'href',
      '/catalog',
    );
    expect(screen.getByRole('banner')).toBeInTheDocument();
    expect(screen.getByRole('main')).toBeInTheDocument();
    expect(screen.getByRole('contentinfo')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'К основному содержимому' })).toHaveAttribute(
      'href',
      '#main-content',
    );
    expect(within(nav()).queryByRole('button', { name: 'Выйти' })).not.toBeInTheDocument();
  });

  it('sends a guest from "create" through sign-in and back to the editor', async () => {
    const user = userEvent.setup();
    renderApp(<AppRoutes />);
    await within(nav()).findByRole('link', { name: 'Вход' });
    await user.click(within(nav()).getByRole('link', { name: 'Создать опрос' }));
    await user.type(await screen.findByLabelText('Email'), SEED_EMAIL);
    await user.type(screen.getByLabelText('Пароль'), SEED_PASSWORD);
    await user.click(screen.getByRole('button', { name: 'Войти' }));
    expect(await screen.findByLabelText('Название')).toBeInTheDocument();
  });

  it('shows the user name and logs out', async () => {
    const user = userEvent.setup();
    await signIn();
    renderApp(<AppRoutes />);
    expect(await within(nav()).findByText('Anna')).toBeInTheDocument();
    expect(within(nav()).getByRole('link', { name: 'Создать опрос' })).toHaveAttribute(
      'href',
      '/tierlists/new',
    );
    expect(within(nav()).queryByRole('link', { name: 'Вход' })).not.toBeInTheDocument();
    await user.click(within(nav()).getByRole('button', { name: 'Выйти' }));
    expect(await within(nav()).findByRole('link', { name: 'Вход' })).toBeInTheDocument();
    expect(within(nav()).queryByText('Anna')).not.toBeInTheDocument();
  });

  it('marks the active link with aria-current', async () => {
    renderApp(<AppRoutes />, ['/catalog']);
    await within(nav()).findByRole('link', { name: 'Регистрация' });
    const catalog = within(nav()).getByRole('link', { name: 'Каталог' });
    expect(catalog).toHaveAttribute('aria-current', 'page');
    expect(within(nav()).getByRole('link', { name: 'Регистрация' })).not.toHaveAttribute(
      'aria-current',
    );
    expect(screen.getByRole('heading', { name: 'Каталог' })).toBeInTheDocument();
  });

  it('keeps the nav usable when /auth/me fails', async () => {
    server.use(http.get('/api/v1/auth/me', () => HttpResponse.error()));
    renderApp(<AppRoutes />);
    expect(within(nav()).getByRole('link', { name: 'Каталог' })).toBeInTheDocument();
  });

  it('toggles the mobile menu, closes on Escape and on navigation', async () => {
    const user = userEvent.setup();
    renderApp(<AppRoutes />);
    const toggle = screen.getByRole('button', { name: 'Меню' });
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(toggle).toHaveAttribute('aria-controls', 'site-nav');
    expect(nav()).toHaveAttribute('data-open', 'false');

    await user.click(toggle);
    expect(screen.getByRole('button', { name: 'Закрыть меню' })).toHaveAttribute(
      'aria-expanded',
      'true',
    );
    expect(nav()).toHaveAttribute('data-open', 'true');

    await user.keyboard('{Escape}');
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(toggle).toHaveFocus();

    await user.keyboard('{Escape}'); // no-op when closed
    await user.click(toggle);
    await user.click(within(nav()).getByRole('link', { name: 'Каталог' }));
    expect(screen.getByRole('button', { name: 'Меню' })).toHaveAttribute('aria-expanded', 'false');
  });

  it('opens the menu with the keyboard', async () => {
    const user = userEvent.setup();
    renderApp(<AppRoutes />);
    screen.getByRole('button', { name: 'Меню' }).focus();
    await user.keyboard('{Enter}');
    expect(nav()).toHaveAttribute('data-open', 'true');
  });

  it('renders home calls to action for guests and signed-in users', async () => {
    renderApp(<AppRoutes />);
    const main = screen.getByRole('main');
    await within(nav()).findByRole('link', { name: 'Регистрация' });
    expect(within(main).getByRole('link', { name: 'Создать опрос' })).toHaveAttribute(
      'href',
      '/login',
    );
    expect(within(main).getByRole('link', { name: 'Смотреть каталог' })).toHaveAttribute(
      'href',
      '/catalog',
    );
  });

  it('home links to the editor for signed-in users', async () => {
    await signIn();
    renderApp(<AppRoutes />);
    const main = screen.getByRole('main');
    expect(await within(main).findByRole('link', { name: 'Создать опрос' })).toHaveAttribute(
      'href',
      '/tierlists/new',
    );
  });
});
