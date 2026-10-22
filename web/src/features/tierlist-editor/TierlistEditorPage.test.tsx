import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { MemoryRouter } from 'react-router-dom';
import { AppRoutes } from '../../routes';
import { server } from '../../mocks/server';
import { resetTierlistStore } from './mocks';

beforeEach(() => resetTierlistStore());

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <AppRoutes />
    </MemoryRouter>,
  );
}

async function fillValidNew(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText('Название'), 'Мой опрос');
  await user.type(screen.getByLabelText('Название элемента 1'), 'Go');
  await user.type(screen.getByLabelText('Название элемента 2'), 'Rust');
  await user.type(screen.getByLabelText('Название элемента 3'), 'Zig');
}

describe('create', () => {
  it('shows default tiers and live preview', async () => {
    const user = userEvent.setup();
    renderAt('/tierlists/new');
    expect(screen.getByLabelText('Название тира 6')).toHaveValue('F');
    await user.type(screen.getByLabelText('Название'), 'Привет');
    const preview = screen.getByRole('region', { name: 'Предпросмотр' });
    expect(within(preview).getByRole('heading', { name: 'Привет' })).toBeInTheDocument();
    await user.type(screen.getByLabelText('Название элемента 1'), 'Go');
    expect(within(preview).getByText('Go')).toBeInTheDocument();
  });

  it('shows client validation errors and does not call the API', async () => {
    const user = userEvent.setup();
    let called = false;
    server.use(
      http.post('/api/v1/tierlists', () => {
        called = true;
        return new HttpResponse(null, { status: 500 });
      }),
    );
    renderAt('/tierlists/new');
    await user.click(screen.getByRole('button', { name: 'Создать опрос' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Исправьте ошибки');
    expect(screen.getByLabelText('Название')).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByLabelText('Название элемента 1')).toHaveAttribute('aria-invalid', 'true');
    expect(called).toBe(false);
  });

  it('creates an unlisted list and moves to the edit page', async () => {
    const user = userEvent.setup();
    renderAt('/tierlists/new');
    await fillValidNew(user);
    await user.click(screen.getByRole('button', { name: 'Создать опрос' }));
    expect(
      await screen.findByRole('heading', { name: 'Редактирование опроса' }),
    ).toBeInTheDocument();
    expect(await screen.findByText('Опрос создан.')).toBeInTheDocument();
  });

  it('requires a slug for public lists and shows API field errors', async () => {
    const user = userEvent.setup();
    renderAt('/tierlists/new');
    await fillValidNew(user);
    await user.click(screen.getByLabelText(/Публичный/));
    await user.click(screen.getByRole('button', { name: 'Создать опрос' }));
    expect(await screen.findByLabelText('Адрес опроса')).toHaveAttribute('aria-invalid', 'true');

    await user.type(screen.getByLabelText('Адрес опроса'), 'fresh-list');
    await user.click(screen.getByRole('button', { name: 'Создать опрос' }));
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Проверьте поля формы');
    expect(alert).toHaveTextContent('validation_failed');
    expect(screen.getByText('Этот адрес уже занят', { selector: 'p' })).toBeInTheDocument();

    await user.clear(screen.getByLabelText('Адрес опроса'));
    await user.type(screen.getByLabelText('Адрес опроса'), 'brand-new');
    await user.click(screen.getByRole('button', { name: 'Создать опрос' }));
    await waitFor(() => expect(screen.getByLabelText('Адрес опроса')).toHaveValue('brand-new'));
    expect(await screen.findByText('Опрос создан.')).toBeInTheDocument();
  });

  it('shows a network error message', async () => {
    const user = userEvent.setup();
    server.use(http.post('/api/v1/tierlists', () => HttpResponse.error()));
    renderAt('/tierlists/new');
    await fillValidNew(user);
    await user.click(screen.getByRole('button', { name: 'Создать опрос' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось связаться с сервером');
  });

  it('shows API errors without details', async () => {
    const user = userEvent.setup();
    server.use(
      http.post('/api/v1/tierlists', () =>
        HttpResponse.json(
          { code: 'email_not_verified', message: 'Подтвердите email' },
          { status: 403 },
        ),
      ),
    );
    renderAt('/tierlists/new');
    await fillValidNew(user);
    await user.click(screen.getByRole('button', { name: 'Создать опрос' }));
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Подтвердите email');
    expect(alert).toHaveTextContent('email_not_verified');
  });

  it('falls back to a status message for non-JSON errors', async () => {
    const user = userEvent.setup();
    server.use(http.post('/api/v1/tierlists', () => new HttpResponse('boom', { status: 502 })));
    renderAt('/tierlists/new');
    await fillValidNew(user);
    await user.click(screen.getByRole('button', { name: 'Создать опрос' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('HTTP 502');
  });
});

describe('items and tiers editing', () => {
  it('adds, reorders and removes items', async () => {
    const user = userEvent.setup();
    renderAt('/tierlists/new');
    await user.type(screen.getByLabelText('Название элемента 1'), 'one');
    await user.type(screen.getByLabelText('Название элемента 2'), 'two');
    await user.click(screen.getByRole('button', { name: 'Опустить элемент 1' }));
    expect(screen.getByLabelText('Название элемента 1')).toHaveValue('two');
    expect(screen.getByLabelText('Название элемента 2')).toHaveValue('one');
    await user.click(screen.getByRole('button', { name: 'Поднять элемент 2' }));
    expect(screen.getByLabelText('Название элемента 1')).toHaveValue('one');

    await user.click(screen.getByRole('button', { name: 'Добавить элемент' }));
    expect(screen.getByLabelText('Название элемента 4')).toHaveFocus();
    await user.click(screen.getByRole('button', { name: 'Удалить элемент 4' }));
    expect(screen.queryByLabelText('Название элемента 4')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Поднять элемент 1' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Опустить элемент 3' })).toBeDisabled();
  });

  it('stops adding at the maximum', async () => {
    const user = userEvent.setup();
    renderAt('/tierlists/new');
    const add = screen.getByRole('button', { name: 'Добавить элемент' });
    for (let i = 0; i < 47; i++) await user.click(add);
    expect(add).toBeDisabled();
  });

  it('edits tiers: color, order, add, remove, reset to defaults', async () => {
    const user = userEvent.setup();
    renderAt('/tierlists/new');
    const color = screen.getByLabelText('Цвет тира 1 (#rrggbb)');
    await user.clear(color);
    await user.type(color, '#112233');
    expect(screen.getByLabelText('Выбор цвета тира 1')).toHaveValue('#112233');
    await user.clear(color);
    await user.type(color, 'zz');
    expect(screen.getByLabelText('Выбор цвета тира 1')).toHaveValue('#cccccc');

    await user.click(screen.getByRole('button', { name: 'Опустить тир 1' }));
    expect(screen.getByLabelText('Название тира 1')).toHaveValue('A');
    await user.click(screen.getByRole('button', { name: 'Поднять тир 2' }));
    expect(screen.getByLabelText('Название тира 1')).toHaveValue('S');

    await user.click(screen.getByRole('button', { name: 'Добавить тир' }));
    expect(screen.getByLabelText('Название тира 7')).toHaveValue('');
    await user.click(screen.getByRole('button', { name: 'Удалить тир 7' }));
    await user.click(screen.getByRole('button', { name: 'Удалить тир 1' }));
    expect(screen.getByLabelText('Название тира 1')).toHaveValue('A');
    await user.click(screen.getByRole('button', { name: /Тиры по умолчанию/ }));
    expect(screen.getByLabelText('Название тира 1')).toHaveValue('S');
    for (let i = 0; i < 4; i++)
      await user.click(screen.getByRole('button', { name: 'Добавить тир' }));
    expect(screen.getByRole('button', { name: 'Добавить тир' })).toBeDisabled();
    await user.click(screen.getByLabelText('Выбор цвета тира 1'));
  });

  it('shows tier and image url errors inline', async () => {
    const user = userEvent.setup();
    renderAt('/tierlists/new');
    await fillValidNew(user);
    await user.type(screen.getByLabelText('Ссылка на картинку 1 (необязательно)'), 'nope');
    await user.clear(screen.getByLabelText('Название тира 1'));
    await user.clear(screen.getByLabelText('Цвет тира 2 (#rrggbb)'));
    await user.click(screen.getByRole('button', { name: 'Создать опрос' }));
    expect(await screen.findByText('Нужна ссылка http:// или https://')).toBeInTheDocument();
    expect(
      screen.getByText('Введите название тира', { selector: 'p.te-error' }),
    ).toBeInTheDocument();
    expect(
      screen.getByText('Цвет в формате #rrggbb', { selector: 'p.te-error' }),
    ).toBeInTheDocument();
  });

  it('shows a description counter error', async () => {
    const user = userEvent.setup();
    renderAt('/tierlists/new');
    await fillValidNew(user);
    await user.click(screen.getByLabelText('Описание (необязательно)'));
    await user.paste('x'.repeat(1001));
    await user.click(screen.getByRole('button', { name: 'Создать опрос' }));
    expect(
      await screen.findByText('Не длиннее 1000 символов', { selector: 'p.te-error' }),
    ).toBeInTheDocument();
  });
});

describe('edit', () => {
  it('loads, edits and saves a list without answers', async () => {
    const user = userEvent.setup();
    renderAt('/tierlists/fresh-list/edit');
    expect(await screen.findByLabelText('Название')).toHaveValue('Лучшие языки');
    expect(screen.getByLabelText('Адрес опроса')).toHaveAttribute('readonly');
    expect(screen.getByLabelText('Название элемента 3')).toBeEnabled();
    await user.clear(screen.getByLabelText('Название'));
    await user.type(screen.getByLabelText('Название'), 'Новое');
    await user.click(screen.getByRole('button', { name: 'Сохранить' }));
    expect(await screen.findByText('Изменения сохранены.')).toBeInTheDocument();
    expect(screen.getByText(/Статус: открыт/)).toBeInTheDocument();
  });

  it('locks structure after the first answer but allows text', async () => {
    const user = userEvent.setup();
    renderAt('/tierlists/locked-list/edit');
    expect(await screen.findByText(/элементы и тиры изменить нельзя/)).toBeInTheDocument();
    expect(screen.getByLabelText('Название элемента 1')).toBeDisabled();
    expect(screen.getByLabelText('Название тира 1')).toBeDisabled();
    await user.clear(screen.getByLabelText('Название'));
    await user.type(screen.getByLabelText('Название'), 'Только название');
    await user.click(screen.getByRole('button', { name: 'Сохранить' }));
    expect(await screen.findByText('Изменения сохранены.')).toBeInTheDocument();
  });

  it('does not validate locked structure', async () => {
    const user = userEvent.setup();
    server.use(
      http.get('/api/v1/tierlists/locked-list', () =>
        HttpResponse.json({
          id: 'x',
          slug: 'locked-list',
          title: 'T',
          description: '',
          visibility: 'unlisted',
          status: 'open',
          tiers: [{ id: 't', name: 'S', color: '#ffffff', position: 0 }],
          items: [],
          author: { id: 'a', display_name: 'A' },
          is_owner: true,
          submissions_count: 1,
          created_at: '2026-10-22T09:00:00Z',
          closed_at: null,
        }),
      ),
    );
    renderAt('/tierlists/locked-list/edit');
    await screen.findByLabelText('Название');
    await user.click(screen.getByRole('button', { name: 'Сохранить' }));
    expect(await screen.findByText('Изменения сохранены.')).toBeInTheDocument();
  });

  it('reacts to 409 tierlist_locked from the API', async () => {
    const user = userEvent.setup();
    server.use(
      http.patch('/api/v1/tierlists/fresh-list', () =>
        HttpResponse.json({ code: 'tierlist_locked', message: 'Уже проходили' }, { status: 409 }),
      ),
    );
    renderAt('/tierlists/fresh-list/edit');
    await screen.findByLabelText('Название');
    await user.click(screen.getByRole('button', { name: 'Сохранить' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('tierlist_locked');
    expect(screen.getByLabelText('Название элемента 1')).toBeDisabled();
  });

  it('shows a not-found error', async () => {
    renderAt('/tierlists/nope/edit');
    expect(await screen.findByRole('alert')).toHaveTextContent('Не найдено');
  });

  it('refuses non-owners', async () => {
    renderAt('/tierlists/foreign-list/edit');
    expect(await screen.findByRole('alert')).toHaveTextContent('только его автор');
  });
});

describe('close and delete', () => {
  it('closes after confirmation in a non-blocking dialog', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.spyOn(window, 'confirm');
    renderAt('/tierlists/fresh-list/edit');
    await screen.findByLabelText('Название');
    await user.click(screen.getByRole('button', { name: 'Закрыть опрос' }));
    const dialog = screen.getByRole('alertdialog', { name: 'Закрыть опрос?' });
    expect(within(dialog).getByRole('button', { name: 'Отмена' })).toHaveFocus();
    await user.click(within(dialog).getByRole('button', { name: 'Закрыть' }));
    expect(await screen.findByText(/Опрос закрыт/)).toBeInTheDocument();
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Закрыть опрос' })).not.toBeInTheDocument();
    expect(screen.getByText(/Статус: закрыт/)).toBeInTheDocument();
    expect(confirmSpy).not.toHaveBeenCalled();
  });

  it('cancels with Escape and the cancel button', async () => {
    const user = userEvent.setup();
    renderAt('/tierlists/fresh-list/edit');
    await screen.findByLabelText('Название');
    const del = screen.getByRole('button', { name: 'Удалить опрос' });
    await user.click(del);
    await user.keyboard('{Escape}');
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(del).toHaveFocus();
    await user.click(del);
    await user.click(screen.getByRole('button', { name: 'Отмена' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
  });

  it('traps Tab inside the dialog', async () => {
    const user = userEvent.setup();
    renderAt('/tierlists/fresh-list/edit');
    await screen.findByLabelText('Название');
    await user.click(screen.getByRole('button', { name: 'Удалить опрос' }));
    const cancel = screen.getByRole('button', { name: 'Отмена' });
    const ok = screen.getByRole('button', { name: 'Удалить' });
    await user.tab({ shift: true });
    expect(ok).toHaveFocus();
    await user.tab();
    expect(cancel).toHaveFocus();
    await user.tab();
    expect(ok).toHaveFocus();
  });

  it('deletes after confirmation and goes home', async () => {
    const user = userEvent.setup();
    renderAt('/tierlists/fresh-list/edit');
    await screen.findByLabelText('Название');
    await user.click(screen.getByRole('button', { name: 'Удалить опрос' }));
    await user.click(screen.getByRole('button', { name: 'Удалить' }));
    expect(await screen.findByRole('heading', { name: 'Tierl' })).toBeInTheDocument();
  });

  it('reports a failed delete', async () => {
    const user = userEvent.setup();
    server.use(
      http.delete('/api/v1/tierlists/fresh-list', () =>
        HttpResponse.json({ code: 'forbidden', message: 'Нет прав' }, { status: 403 }),
      ),
    );
    renderAt('/tierlists/fresh-list/edit');
    await screen.findByLabelText('Название');
    await user.click(screen.getByRole('button', { name: 'Удалить опрос' }));
    await user.click(screen.getByRole('button', { name: 'Удалить' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Нет прав');
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
  });
});
