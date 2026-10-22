import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '../../mocks/server';
import { fewDataResultId, sampleResult, sampleStats } from './handlers';
import { ResultPage } from './ResultPage';

function renderPage(id = sampleResult.submission.id) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/r/${id}`]}>
        <Routes>
          <Route path="/r/:id" element={<ResultPage />} />
          <Route path="/t/:slug" element={<p>take stub</p>} />
          <Route path="/" element={<p>home stub</p>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  Reflect.deleteProperty(navigator, 'share');
});

describe('ResultPage', () => {
  it('shows loading, own placement, percent with threshold and totals', async () => {
    renderPage();
    expect(screen.getByRole('status')).toHaveTextContent('Загружаем');
    expect(
      await screen.findByRole('heading', { name: /Языки программирования/ }),
    ).toBeInTheDocument();
    expect(screen.getByRole('region', { name: 'S, элементов: 1' })).toHaveTextContent('Go');
    expect(screen.getByText(/Не размещено: Java/)).toBeInTheDocument();
    expect(await screen.findByText('37,5%')).toBeInTheDocument();
    expect(screen.getByText(/не меньше чем на 80%/)).toHaveTextContent('Всего ответов: 42');
  });

  it('describes each bar in text, marks own choice, lists anonymous similar answers', async () => {
    renderPage();
    const bar = await screen.findByRole('img', { name: 'Тир S: 47,6%, ответов: 20, ваш выбор' });
    expect(bar).toBeInTheDocument();
    expect(screen.getAllByRole('img', { name: /^Тир A: 35,7%/ })[0]).toBeInTheDocument();
    const similar = screen.getByRole('heading', { name: 'Самые похожие' }).parentElement!;
    expect(within(similar).getByText('Сходство 97%')).toBeInTheDocument();
    const diss = screen.getByRole('heading', { name: 'Самые непохожие' }).parentElement!;
    expect(within(diss).getByText('Сходство 12%')).toBeInTheDocument();
    expect(screen.queryByText(/1111/)).not.toBeInTheDocument();
  });

  it('shows "not enough data" using numbers from the API', async () => {
    renderPage(fewDataResultId);
    expect(await screen.findByText('Пока мало данных')).toBeInTheDocument();
    expect(screen.getByText(/Ответов: 3, для статистики нужно не меньше 20/)).toBeInTheDocument();
    expect(screen.queryByText('Распределение по элементам')).not.toBeInTheDocument();
  });

  it('shows empty similar lists', async () => {
    server.use(
      http.get('/api/v1/results/:id/stats', () =>
        HttpResponse.json({ ...sampleStats, similar: [], dissimilar: [] }),
      ),
    );
    renderPage();
    expect(await screen.findAllByText('Пока нет.')).toHaveLength(2);
  });

  it('shows 404', async () => {
    renderPage('00000000-0000-4000-8000-00000000dead');
    expect(await screen.findByRole('heading', { name: 'Результат не найден' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'На главную' })).toBeInTheDocument();
  });

  it('shows an error with retry for the result', async () => {
    const user = userEvent.setup();
    let fail = true;
    server.use(
      http.get('/api/v1/results/:id', () =>
        fail
          ? HttpResponse.json({ code: 'x', message: 'x' }, { status: 500 })
          : HttpResponse.json(sampleResult),
      ),
    );
    renderPage();
    expect(
      await screen.findByRole('heading', { name: 'Не удалось загрузить результат' }),
    ).toBeInTheDocument();
    fail = false;
    await user.click(screen.getByRole('button', { name: 'Повторить' }));
    expect(await screen.findByRole('heading', { name: /Ваш результат/ })).toBeInTheDocument();
  });

  it('keeps the result when stats fail and allows retry', async () => {
    const user = userEvent.setup();
    let fail = true;
    server.use(
      http.get('/api/v1/results/:id/stats', () =>
        fail
          ? HttpResponse.json({ code: 'x', message: 'x' }, { status: 500 })
          : HttpResponse.json(sampleStats),
      ),
    );
    renderPage();
    expect(await screen.findByText('Не удалось загрузить статистику.')).toBeInTheDocument();
    expect(screen.getByRole('region', { name: 'S, элементов: 1' })).toBeInTheDocument();
    fail = false;
    await user.click(screen.getByRole('button', { name: 'Повторить статистику' }));
    expect(await screen.findByText('37,5%')).toBeInTheDocument();
  });

  it('copies the link when navigator.share is missing', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('37,5%');
    await user.click(screen.getByRole('button', { name: 'Поделиться ссылкой' }));
    expect(screen.getByText('Ссылка скопирована.')).toBeInTheDocument();
    expect(await navigator.clipboard.readText()).toBe(
      `${window.location.origin}/r/${sampleResult.submission.id}`,
    );
  });

  it('uses navigator.share when available', async () => {
    const user = userEvent.setup();
    const share = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'share', { value: share, configurable: true });
    renderPage();
    await screen.findByText('37,5%');
    await user.click(screen.getByRole('button', { name: 'Поделиться ссылкой' }));
    expect(share).toHaveBeenCalledWith(
      expect.objectContaining({ url: expect.stringContaining(`/r/${sampleResult.submission.id}`) }),
    );
    expect(screen.getByText('Ссылка отправлена.')).toBeInTheDocument();
  });

  it('links back to the poll', async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole('link', { name: 'Пройти ещё раз' }));
    expect(screen.getByText('take stub')).toBeInTheDocument();
  });
});
