import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { renderApp } from '../features/auth/test-utils';
import { sampleCatalogItem, sampleUnlistedItem } from '../features/catalog/mocks';
import { server } from '../mocks/server';
import { CatalogPage } from './CatalogPage';

function renderCatalog() {
  return renderApp(<CatalogPage />, ['/catalog']);
}

describe('CatalogPage', () => {
  it('shows loading and then public catalog entries', async () => {
    renderCatalog();
    expect(screen.getByRole('status')).toHaveTextContent('Загружаем каталог');
    expect(
      await screen.findByRole('heading', { name: 'Языки программирования' }),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Языки программирования' })).toHaveAttribute(
      'href',
      '/t/languages',
    );
    expect(screen.getByText('Расставьте языки по тирам.')).toBeInTheDocument();
    expect(screen.getByText(/Автор: Anna\. Элементов: 4\. Ответов: 42\./)).toBeInTheDocument();
  });

  it('sends the selected sort and search query', async () => {
    const user = userEvent.setup();
    const requests: URLSearchParams[] = [];
    server.use(
      http.get('/api/v1/catalog', ({ request }) => {
        const url = new URL(request.url);
        requests.push(url.searchParams);
        return HttpResponse.json({ items: [sampleCatalogItem], next_cursor: null });
      }),
    );

    renderCatalog();
    await screen.findByRole('heading', { name: 'Языки программирования' });

    await user.selectOptions(screen.getByLabelText('Сортировка'), 'popular');
    await waitFor(() => expect(requests.some((p) => p.get('sort') === 'popular')).toBe(true));

    await user.type(screen.getByLabelText('Поиск'), '  rust  ');
    await user.click(screen.getByRole('button', { name: 'Найти' }));
    await waitFor(() => expect(requests.some((p) => p.get('q') === 'rust')).toBe(true));
  });

  it('loads the next page by cursor without replacing the first one', async () => {
    const user = userEvent.setup();
    server.use(
      http.get('/api/v1/catalog', ({ request }) => {
        const cursor = new URL(request.url).searchParams.get('cursor');
        if (cursor === 'next-page') {
          return HttpResponse.json({
            items: [{ ...sampleCatalogItem, id: 'second', slug: 'second', title: 'Второй опрос' }],
            next_cursor: null,
          });
        }
        return HttpResponse.json({ items: [sampleCatalogItem], next_cursor: 'next-page' });
      }),
    );

    renderCatalog();
    await screen.findByRole('heading', { name: 'Языки программирования' });
    await user.click(screen.getByRole('button', { name: 'Следующая страница' }));

    expect(await screen.findByRole('heading', { name: 'Второй опрос' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Языки программирования' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Следующая страница' })).not.toBeInTheDocument();
  });

  it('shows an empty state', async () => {
    server.use(
      http.get('/api/v1/catalog', () => HttpResponse.json({ items: [], next_cursor: null })),
    );
    renderCatalog();
    expect(await screen.findByText('В каталоге пока нет опросов.')).toBeInTheDocument();
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('shows an error and retries', async () => {
    const user = userEvent.setup();
    let fail = true;
    server.use(
      http.get('/api/v1/catalog', () =>
        fail
          ? HttpResponse.json({ code: 'x', message: 'x' }, { status: 500 })
          : HttpResponse.json({ items: [sampleCatalogItem], next_cursor: null }),
      ),
    );
    renderCatalog();
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить каталог');
    fail = false;
    await user.click(screen.getByRole('button', { name: 'Повторить' }));
    expect(
      await screen.findByRole('heading', { name: 'Языки программирования' }),
    ).toBeInTheDocument();
  });

  it('never shows unlisted entries even if the response contains one', async () => {
    server.use(
      http.get('/api/v1/catalog', () =>
        HttpResponse.json({
          items: [sampleCatalogItem, sampleUnlistedItem],
          next_cursor: null,
        }),
      ),
    );
    renderCatalog();
    await screen.findByRole('heading', { name: 'Языки программирования' });
    expect(screen.queryByText('Секретный опрос')).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Секретный опрос' })).not.toBeInTheDocument();
  });
});
