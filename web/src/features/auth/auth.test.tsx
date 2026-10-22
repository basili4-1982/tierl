import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { Route, Routes, useLocation } from 'react-router-dom';
import { server } from '../../mocks/server';
import { ApiError } from '../../api/client';
import { describeError, fetchCurrentUser, resetCsrfToken } from './api';
import { authRoutes } from './routes';
import { RequireAuth } from './RequireAuth';
import { useCurrentUser, useLogout } from './session';
import { SEED_EMAIL, SEED_PASSWORD, VALID_TOKEN } from './mocks';
import { renderApp, resetAuthTestState } from './test-utils';
import { StrictMode } from 'react';

function Where() {
  const l = useLocation();
  return <p data-testid="where">{l.pathname}</p>;
}

function Shell() {
  return (
    <Routes>
      <Route
        path="/"
        element={
          <>
            <h1>Домой</h1>
            <Where />
          </>
        }
      />
      {authRoutes()}
      <Route element={<RequireAuth />}>
        <Route path="/private" element={<h1>Секрет</h1>} />
      </Route>
    </Routes>
  );
}

beforeEach(resetAuthTestState);

describe('register', () => {
  it('validates fields on the client and focuses the first invalid one', async () => {
    const user = userEvent.setup();
    renderApp(<Shell />, ['/register']);
    await user.click(screen.getByRole('button', { name: 'Зарегистрироваться' }));
    expect(screen.getByText('Укажите имя')).toBeInTheDocument();
    expect(screen.getByText('Укажите email')).toBeInTheDocument();
    expect(
      screen.getByText('Длина пароля от 10 до 128 символов', { selector: '.error' }),
    ).toBeInTheDocument();
    const name = screen.getByLabelText('Имя');
    expect(name).toHaveAttribute('aria-invalid', 'true');
    expect(name).toHaveFocus();
    expect(name).toHaveAccessibleDescription('Укажите имя');
  });

  it('rejects a malformed email and a 129-char password', async () => {
    const user = userEvent.setup();
    renderApp(<Shell />, ['/register']);
    await user.type(screen.getByLabelText('Имя'), 'Bob');
    await user.type(screen.getByLabelText('Email'), 'nope');
    await user.click(screen.getByLabelText('Пароль'));
    await user.paste('x'.repeat(129));
    await user.click(screen.getByRole('button', { name: 'Зарегистрироваться' }));
    expect(screen.getByText('Введите корректный email')).toBeInTheDocument();
    expect(screen.getByLabelText('Пароль')).toHaveAttribute('aria-invalid', 'true');
  });

  it('registers, sends the CSRF header and shows the check-your-mail state', async () => {
    const user = userEvent.setup();
    let csrf: string | null = null;
    server.events.on('request:start', ({ request }) => {
      if (request.url.endsWith('/auth/register')) csrf = request.headers.get('X-CSRF-Token');
    });
    const { client } = renderApp(<Shell />, ['/register']);
    await user.type(screen.getByLabelText('Имя'), 'Bob');
    await user.type(screen.getByLabelText('Email'), 'bob@example.com');
    await user.type(screen.getByLabelText('Пароль'), 'a-long-enough-password');
    await user.click(screen.getByRole('button', { name: 'Зарегистрироваться' }));
    expect(await screen.findByRole('heading', { name: 'Проверьте почту' })).toBeInTheDocument();
    expect(screen.getByRole('status')).toHaveTextContent('bob@example.com');
    expect(csrf).toBe('test-csrf-token');
    expect(client.getQueryData(['auth', 'me'])).toMatchObject({ email: 'bob@example.com' });
    server.events.removeAllListeners();
  });

  it('shows the pending state while the request is in flight', async () => {
    const user = userEvent.setup();
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    server.use(
      http.post('/api/v1/auth/register', async () => {
        await gate;
        return HttpResponse.json(
          { code: 'email_taken', message: 'Этот email уже зарегистрирован' },
          { status: 409 },
        );
      }),
    );
    renderApp(<Shell />, ['/register']);
    await user.type(screen.getByLabelText('Имя'), 'Bob');
    await user.type(screen.getByLabelText('Email'), 'bob@example.com');
    await user.type(screen.getByLabelText('Пароль'), 'a-long-enough-password');
    await user.click(screen.getByRole('button', { name: 'Зарегистрироваться' }));
    const busy = await screen.findByRole('button', { name: 'Регистрируем…' });
    expect(busy).toBeDisabled();
    release();
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Этот email уже зарегистрирован');
    expect(alert).toHaveFocus();
  });

  it('maps server field errors (422 details.fields) onto inputs', async () => {
    const user = userEvent.setup();
    server.use(
      http.post('/api/v1/auth/register', () =>
        HttpResponse.json(
          {
            code: 'validation_failed',
            message: 'Проверьте поля формы',
            details: { fields: { email: 'Домен не поддерживается' } },
          },
          { status: 422 },
        ),
      ),
    );
    renderApp(<Shell />, ['/register']);
    await user.type(screen.getByLabelText('Имя'), 'Bob');
    await user.type(screen.getByLabelText('Email'), 'bob@example.com');
    await user.type(screen.getByLabelText('Пароль'), 'a-long-enough-password');
    await user.click(screen.getByRole('button', { name: 'Зарегистрироваться' }));
    const email = screen.getByLabelText('Email');
    await waitFor(() => expect(email).toHaveAccessibleDescription('Домен не поддерживается'));
    expect(email).toHaveFocus();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('reports a network failure', async () => {
    const user = userEvent.setup();
    server.use(http.post('/api/v1/auth/register', () => HttpResponse.error()));
    renderApp(<Shell />, ['/register']);
    await user.type(screen.getByLabelText('Имя'), 'Bob');
    await user.type(screen.getByLabelText('Email'), 'bob@example.com');
    await user.type(screen.getByLabelText('Пароль'), 'a-long-enough-password');
    await user.click(screen.getByRole('button', { name: 'Зарегистрироваться' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Нет связи с сервером');
  });
});

describe('login', () => {
  it('requires email and password', async () => {
    const user = userEvent.setup();
    renderApp(<Shell />, ['/login']);
    await user.click(screen.getByRole('button', { name: 'Войти' }));
    expect(screen.getByText('Укажите email')).toBeInTheDocument();
    expect(screen.getByText('Введите пароль')).toBeInTheDocument();
    expect(screen.getByLabelText('Email')).toHaveFocus();
  });

  it('rejects an over-long password locally', async () => {
    const user = userEvent.setup();
    renderApp(<Shell />, ['/login']);
    await user.type(screen.getByLabelText('Email'), SEED_EMAIL);
    await user.click(screen.getByLabelText('Пароль'));
    await user.paste('x'.repeat(129));
    await user.click(screen.getByRole('button', { name: 'Войти' }));
    expect(screen.getByText('Пароль не длиннее 128 символов')).toBeInTheDocument();
  });

  it('shows invalid_credentials as a focused alert', async () => {
    const user = userEvent.setup();
    renderApp(<Shell />, ['/login']);
    await user.type(screen.getByLabelText('Email'), SEED_EMAIL);
    await user.type(screen.getByLabelText('Пароль'), 'wrong-password-1');
    await user.click(screen.getByRole('button', { name: 'Войти' }));
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Неверный email или пароль');
    expect(alert).toHaveFocus();
  });

  it('shows a rate-limit message on 429', async () => {
    const user = userEvent.setup();
    server.use(
      http.post('/api/v1/auth/login', () =>
        HttpResponse.json({ code: 'rate_limited', message: 'x' }, { status: 429 }),
      ),
    );
    renderApp(<Shell />, ['/login']);
    await user.type(screen.getByLabelText('Email'), SEED_EMAIL);
    await user.type(screen.getByLabelText('Пароль'), SEED_PASSWORD);
    await user.click(screen.getByRole('button', { name: 'Войти' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Слишком много попыток');
  });

  it('logs in and goes home, updating the session', async () => {
    const user = userEvent.setup();
    const { client } = renderApp(<Shell />, ['/login']);
    await user.type(screen.getByLabelText('Email'), SEED_EMAIL);
    await user.type(screen.getByLabelText('Пароль'), SEED_PASSWORD);
    await user.click(screen.getByRole('button', { name: 'Войти' }));
    expect(await screen.findByRole('heading', { name: 'Домой' })).toBeInTheDocument();
    expect(client.getQueryData(['auth', 'me'])).toMatchObject({ email: SEED_EMAIL });
  });

  it('returns to the protected page after login and ignores external redirects', async () => {
    const user = userEvent.setup();
    renderApp(<Shell />, ['/private']);
    expect(await screen.findByRole('heading', { name: 'Вход' })).toBeInTheDocument();
    await user.type(screen.getByLabelText('Email'), SEED_EMAIL);
    await user.type(screen.getByLabelText('Пароль'), SEED_PASSWORD);
    await user.click(screen.getByRole('button', { name: 'Войти' }));
    expect(await screen.findByRole('heading', { name: 'Секрет' })).toBeInTheDocument();
  });

  it('falls back to / when state.from is not a local path', async () => {
    const user = userEvent.setup();
    renderApp(<Shell />, [{ pathname: '/login', state: { from: '//evil.example' } }]);
    await user.type(screen.getByLabelText('Email'), SEED_EMAIL);
    await user.type(screen.getByLabelText('Пароль'), SEED_PASSWORD);
    await user.click(screen.getByRole('button', { name: 'Войти' }));
    expect(await screen.findByTestId('where')).toHaveTextContent(/^\/$/);
  });

  it('recovers when the CSRF token was rejected once', async () => {
    const user = userEvent.setup();
    let tokens = 0;
    server.use(
      http.get('/api/v1/csrf', () =>
        HttpResponse.json({ csrf_token: tokens++ === 0 ? 'stale' : 'test-csrf-token' }),
      ),
    );
    renderApp(<Shell />, ['/login']);
    await user.type(screen.getByLabelText('Email'), SEED_EMAIL);
    await user.type(screen.getByLabelText('Пароль'), SEED_PASSWORD);
    await user.click(screen.getByRole('button', { name: 'Войти' }));
    expect(await screen.findByRole('heading', { name: 'Домой' })).toBeInTheDocument();
    expect(tokens).toBe(2);
  });
});

describe('verify email', () => {
  it('confirms with a valid token (once, even under StrictMode)', async () => {
    let calls = 0;
    server.events.on('request:start', ({ request }) => {
      if (request.url.endsWith('/auth/verify-email')) calls++;
    });
    renderApp(
      <StrictMode>
        <Shell />
      </StrictMode>,
      [`/verify-email?token=${VALID_TOKEN}`],
    );
    expect(screen.getByRole('status')).toHaveTextContent('Подтверждаем');
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Email подтверждён'));
    expect(calls).toBe(1);
    server.events.removeAllListeners();
  });

  it('shows the API error for a bad token', async () => {
    renderApp(<Shell />, ['/verify-email?token=bad']);
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Ссылка недействительна или устарела',
    );
  });

  it('handles a missing token without calling the API', () => {
    renderApp(<Shell />, ['/verify-email']);
    expect(screen.getByRole('alert')).toHaveTextContent('нет токена');
  });
});

describe('forgot password', () => {
  it('validates the email', async () => {
    const user = userEvent.setup();
    renderApp(<Shell />, ['/forgot-password']);
    await user.click(screen.getByRole('button', { name: 'Отправить ссылку' }));
    expect(screen.getByLabelText('Email')).toHaveAccessibleDescription('Укажите email');
  });

  it('gives the same answer for any address', async () => {
    const user = userEvent.setup();
    renderApp(<Shell />, ['/forgot-password']);
    await user.type(screen.getByLabelText('Email'), 'ghost@example.com');
    await user.click(screen.getByRole('button', { name: 'Отправить ссылку' }));
    expect(await screen.findByRole('status')).toHaveTextContent('Если адрес зарегистрирован');
  });

  it('shows API failures', async () => {
    const user = userEvent.setup();
    server.use(
      http.post('/api/v1/auth/forgot-password', () =>
        HttpResponse.json({ code: 'bad_request', message: 'Некорректный запрос' }, { status: 400 }),
      ),
    );
    renderApp(<Shell />, ['/forgot-password']);
    await user.type(screen.getByLabelText('Email'), 'a@example.com');
    await user.click(screen.getByRole('button', { name: 'Отправить ссылку' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Некорректный запрос');
  });
});

describe('reset password', () => {
  it('needs a token in the link', () => {
    renderApp(<Shell />, ['/reset-password']);
    expect(screen.getByRole('alert')).toHaveTextContent('нет токена');
  });

  it('enforces the 10..128 password length', async () => {
    const user = userEvent.setup();
    renderApp(<Shell />, [`/reset-password?token=${VALID_TOKEN}`]);
    await user.type(screen.getByLabelText('Новый пароль'), 'short');
    await user.click(screen.getByRole('button', { name: 'Сохранить пароль' }));
    expect(screen.getByLabelText('Новый пароль')).toHaveAccessibleDescription(
      /От 10 до 128 символов/,
    );
    expect(screen.getByLabelText('Новый пароль')).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByLabelText('Новый пароль')).toHaveFocus();
  });

  it('sets the new password', async () => {
    const user = userEvent.setup();
    renderApp(<Shell />, [`/reset-password?token=${VALID_TOKEN}`]);
    await user.type(screen.getByLabelText('Новый пароль'), 'another-long-password');
    await user.click(screen.getByRole('button', { name: 'Сохранить пароль' }));
    expect(await screen.findByRole('heading', { name: 'Пароль изменён' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Войти' })).toHaveAttribute('href', '/login');
  });

  it('shows invalid_token from the API', async () => {
    const user = userEvent.setup();
    renderApp(<Shell />, ['/reset-password?token=expired']);
    await user.type(screen.getByLabelText('Новый пароль'), 'another-long-password');
    await user.click(screen.getByRole('button', { name: 'Сохранить пароль' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Ссылка недействительна или устарела',
    );
  });

  it('maps a server new_password field error', async () => {
    const user = userEvent.setup();
    server.use(
      http.post('/api/v1/auth/reset-password', () =>
        HttpResponse.json(
          {
            code: 'validation_failed',
            message: 'm',
            details: { fields: { new_password: 'Слишком простой' } },
          },
          { status: 422 },
        ),
      ),
    );
    renderApp(<Shell />, [`/reset-password?token=${VALID_TOKEN}`]);
    await user.type(screen.getByLabelText('Новый пароль'), 'another-long-password');
    await user.click(screen.getByRole('button', { name: 'Сохранить пароль' }));
    await waitFor(() =>
      expect(screen.getByLabelText('Новый пароль')).toHaveAccessibleDescription(/Слишком простой/),
    );
  });
});

describe('session', () => {
  it('RequireAuth redirects guests to /login', async () => {
    renderApp(<Shell />, ['/private']);
    expect(await screen.findByRole('heading', { name: 'Вход' })).toBeInTheDocument();
  });

  it('RequireAuth shows loading, then protected content for a signed-in user', async () => {
    const user = userEvent.setup();
    renderApp(<Shell />, ['/login']);
    await user.type(screen.getByLabelText('Email'), SEED_EMAIL);
    await user.type(screen.getByLabelText('Пароль'), SEED_PASSWORD);
    await user.click(screen.getByRole('button', { name: 'Войти' }));
    await screen.findByRole('heading', { name: 'Домой' });
  });

  it('RequireAuth lets a signed-in user through', async () => {
    await fetch('/api/v1/csrf');
    const res = await fetch('/api/v1/auth/login', {
      method: 'POST',
      headers: { 'X-CSRF-Token': 'test-csrf-token' },
      body: JSON.stringify({ email: SEED_EMAIL, password: SEED_PASSWORD }),
    });
    expect(res.ok).toBe(true);
    renderApp(<Shell />, ['/private']);
    expect(screen.getByRole('status')).toHaveTextContent('Загрузка');
    expect(await screen.findByRole('heading', { name: 'Секрет' })).toBeInTheDocument();
  });

  it('RequireAuth offers a retry when /auth/me fails', async () => {
    const user = userEvent.setup();
    server.use(http.get('/api/v1/auth/me', () => new HttpResponse('boom', { status: 500 })));
    renderApp(<Shell />, ['/private']);
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить профиль');
    server.use(http.get('/api/v1/auth/me', () => new HttpResponse(null, { status: 401 })));
    await user.click(screen.getByRole('button', { name: 'Повторить' }));
    expect(await screen.findByRole('heading', { name: 'Вход' })).toBeInTheDocument();
  });

  it('useCurrentUser and useLogout manage the session', async () => {
    const user = userEvent.setup();
    await fetch('/api/v1/auth/login', {
      method: 'POST',
      headers: { 'X-CSRF-Token': 'test-csrf-token' },
      body: JSON.stringify({ email: SEED_EMAIL, password: SEED_PASSWORD }),
    });
    function Probe() {
      const { data } = useCurrentUser();
      const logout = useLogout();
      return (
        <>
          <p>{data ? `hi ${data.display_name}` : 'guest'}</p>
          <button onClick={() => logout.mutate()}>out</button>
        </>
      );
    }
    renderApp(<Probe />);
    expect(await screen.findByText('hi Anna')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'out' }));
    expect(await screen.findByText('guest')).toBeInTheDocument();
  });

  it('logout requires a session (401 surfaces as ApiError)', async () => {
    const { logout } = await import('./api');
    await expect(logout()).rejects.toMatchObject({ status: 401 });
  });
});

describe('api helpers', () => {
  it('fetchCurrentUser is null for guests', async () => {
    await expect(fetchCurrentUser()).resolves.toBeNull();
  });

  it('fetchCurrentUser throws on server errors', async () => {
    server.use(
      http.get('/api/v1/auth/me', () =>
        HttpResponse.json({ code: 'x', message: 'y' }, { status: 500 }),
      ),
    );
    await expect(fetchCurrentUser()).rejects.toBeInstanceOf(ApiError);
  });

  it('does not cache a failed CSRF fetch', async () => {
    const { login } = await import('./api');
    let n = 0;
    server.use(
      http.get('/api/v1/csrf', () =>
        n++ === 0
          ? new HttpResponse('x', { status: 500 })
          : HttpResponse.json({ csrf_token: 'test-csrf-token' }),
      ),
    );
    resetCsrfToken();
    await expect(login({ email: SEED_EMAIL, password: SEED_PASSWORD })).rejects.toBeInstanceOf(
      ApiError,
    );
    await expect(login({ email: SEED_EMAIL, password: SEED_PASSWORD })).resolves.toMatchObject({
      email: SEED_EMAIL,
    });
  });

  it('describeError ignores non-string field values and bodies without message', () => {
    const e = new ApiError(422, {
      code: 'v',
      message: '',
      details: { fields: { a: 'bad', b: 1 } },
    });
    expect(describeError(e).fields).toEqual({ a: 'bad' });
    expect(describeError(new ApiError(500, undefined)).message).toMatch(/Не удалось/);
  });
});
