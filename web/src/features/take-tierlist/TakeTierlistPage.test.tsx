import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '../../mocks/server';
import { sampleSubmission, sampleTierlist } from './handlers';
import { TakeTierlistPage } from './TakeTierlistPage';

function renderPage(slug = 'languages') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/t/${slug}`]}>
        <Routes>
          <Route path="/t/:slug" element={<TakeTierlistPage />} />
          <Route path="/r/:id" element={<p>result stub</p>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const move = (user: ReturnType<typeof userEvent.setup>, item: string, tier: string) =>
  user.selectOptions(screen.getByLabelText(`Переместить «${item}»`), tier);

const mineExists = () =>
  server.use(
    http.get('/api/v1/tierlists/:slug/submissions/mine', () => HttpResponse.json(sampleSubmission)),
  );

describe('TakeTierlistPage', () => {
  it('shows loading, then tiers, pool and a disabled submit', async () => {
    renderPage();
    expect(screen.getByRole('status')).toHaveTextContent('Загружаем');
    expect(
      await screen.findByRole('heading', { name: 'Языки программирования' }),
    ).toBeInTheDocument();
    expect(screen.getByRole('region', { name: /^S, элементов: 0/ })).toBeInTheDocument();
    expect(screen.getByRole('region', { name: /^Не размещено, элементов: 4/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Отправить ответ' })).toBeDisabled();
    expect(screen.getByText(/нужно не меньше 3/)).toBeInTheDocument();
  });

  it('enforces the minimal completeness, then submits and redirects to the result', async () => {
    const user = userEvent.setup();
    let body: unknown;
    server.use(
      http.post('/api/v1/tierlists/:slug/submissions', async ({ request }) => {
        body = await request.json();
        return HttpResponse.json(sampleSubmission, { status: 201 });
      }),
    );
    renderPage();
    await screen.findByRole('heading', { name: 'Языки программирования' });
    await move(user, 'Go', 'S');
    await move(user, 'Rust', 'A');
    expect(screen.getByRole('button', { name: 'Отправить ответ' })).toBeDisabled();
    await move(user, 'Python', 'S');
    expect(screen.getByRole('region', { name: /^S, элементов: 2/ })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Отправить ответ' }));
    expect(await screen.findByText('result stub')).toBeInTheDocument();
    expect(body).toEqual({
      placements: [
        { item_id: sampleTierlist.items[0]!.id, tier_id: sampleTierlist.tiers[0]!.id },
        { item_id: sampleTierlist.items[2]!.id, tier_id: sampleTierlist.tiers[0]!.id },
        { item_id: sampleTierlist.items[1]!.id, tier_id: sampleTierlist.tiers[1]!.id },
      ],
    });
  });

  it('lets an item go back to the pool', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole('heading', { name: 'Языки программирования' });
    await move(user, 'Go', 'S');
    await move(user, 'Go', 'Пул');
    expect(screen.getByRole('region', { name: /^S, элементов: 0/ })).toBeInTheDocument();
  });

  it('restores the previous answer and re-submits with PUT', async () => {
    mineExists();
    const user = userEvent.setup();
    let method = '';
    server.use(
      http.put('/api/v1/tierlists/:slug/submissions/mine', ({ request }) => {
        method = request.method;
        return HttpResponse.json(sampleSubmission);
      }),
    );
    renderPage();
    await screen.findByText(/Вы уже отвечали/);
    expect(screen.getByRole('link', { name: 'Текущий результат' })).toHaveAttribute(
      'href',
      `/r/${sampleSubmission.id}`,
    );
    expect(screen.getByRole('region', { name: /^S, элементов: 1/ })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Обновить ответ' }));
    expect(await screen.findByText('result stub')).toBeInTheDocument();
    expect(method).toBe('PUT');
  });

  it('shows a closed survey without the board', async () => {
    renderPage('old-poll');
    expect(await screen.findByText(/Опрос закрыт/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Отправить ответ' })).not.toBeInTheDocument();
  });

  it('offers the existing result of a closed survey', async () => {
    mineExists();
    server.use(
      http.get('/api/v1/tierlists/:slug', () =>
        HttpResponse.json({ ...sampleTierlist, status: 'closed' }),
      ),
    );
    renderPage();
    expect(
      await screen.findByRole('link', { name: 'Посмотреть ваш результат' }),
    ).toBeInTheDocument();
  });

  it('shows not found for an unknown slug', async () => {
    renderPage('nope');
    expect(await screen.findByRole('heading', { name: 'Опрос не найден' })).toBeInTheDocument();
  });

  it('shows a retryable error when the survey fails to load', async () => {
    const user = userEvent.setup();
    server.use(http.get('/api/v1/tierlists/:slug', () => new HttpResponse(null, { status: 500 })));
    renderPage();
    expect(await screen.findByRole('alert')).toHaveTextContent('Попробуйте ещё раз');
    server.resetHandlers();
    await user.click(screen.getByRole('button', { name: 'Повторить' }));
    expect(
      await screen.findByRole('heading', { name: 'Языки программирования' }),
    ).toBeInTheDocument();
  });

  it('shows a retryable error when the previous answer fails to load', async () => {
    const user = userEvent.setup();
    server.use(
      http.get(
        '/api/v1/tierlists/:slug/submissions/mine',
        () => new HttpResponse(null, { status: 500 }),
      ),
    );
    renderPage();
    expect(await screen.findByText(/Не удалось загрузить ваш предыдущий/)).toBeInTheDocument();
    server.resetHandlers();
    await user.click(screen.getByRole('button', { name: 'Повторить' }));
    expect(await screen.findByRole('button', { name: 'Отправить ответ' })).toBeInTheDocument();
  });

  async function fillThree(user: ReturnType<typeof userEvent.setup>) {
    await screen.findByRole('heading', { name: 'Языки программирования' });
    await move(user, 'Go', 'S');
    await move(user, 'Rust', 'A');
    await move(user, 'Python', 'B');
  }

  it.each([
    [429, { code: 'rate_limited', message: 'x' }, /Слишком много попыток/],
    [422, { code: 'validation_failed', message: 'Нужны ответы' }, /Нужны ответы/],
  ])('shows the submit error for %s and allows retry', async (status, body, text) => {
    const user = userEvent.setup();
    server.use(
      http.post('/api/v1/tierlists/:slug/submissions', () => HttpResponse.json(body, { status })),
    );
    renderPage();
    await fillThree(user);
    await user.click(screen.getByRole('button', { name: 'Отправить ответ' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(text);
    expect(screen.getByRole('button', { name: 'Отправить ответ' })).toBeEnabled();
  });

  it('shows a generic error on network failure', async () => {
    const user = userEvent.setup();
    server.use(http.post('/api/v1/tierlists/:slug/submissions', () => HttpResponse.error()));
    renderPage();
    await fillThree(user);
    await user.click(screen.getByRole('button', { name: 'Отправить ответ' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось отправить ответ');
  });

  it('switches to the closed view when the server reports tierlist_closed', async () => {
    const user = userEvent.setup();
    server.use(
      http.post('/api/v1/tierlists/:slug/submissions', () =>
        HttpResponse.json({ code: 'tierlist_closed', message: 'Опрос закрыт' }, { status: 409 }),
      ),
    );
    renderPage();
    await fillThree(user);
    await user.click(screen.getByRole('button', { name: 'Отправить ответ' }));
    await waitFor(() =>
      expect(screen.getByText(/новые ответы не принимаются/)).toBeInTheDocument(),
    );
  });

  it('renders item images', async () => {
    server.use(
      http.get('/api/v1/tierlists/:slug', () =>
        HttpResponse.json({
          ...sampleTierlist,
          items: sampleTierlist.items.map((i) => ({ ...i, image_url: 'https://x.test/a.png' })),
        }),
      ),
    );
    const { container } = renderPage();
    await screen.findByRole('heading', { name: 'Языки программирования' });
    expect(container.querySelectorAll('img.take-chip__img')).toHaveLength(4);
  });
});
