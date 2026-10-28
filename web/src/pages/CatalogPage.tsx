import { useInfiniteQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { fetchCatalog, type CatalogSort } from '../features/catalog/api';
import '../features/catalog/catalog.css';

/** The contract already excludes unlisted and closed polls; filter again so a
 * bad response can never expose them in the catalog UI. */
function isCatalogVisible(item: { visibility: string; status: string }) {
  return item.visibility === 'public' && item.status === 'open';
}

export function CatalogPage() {
  const [sort, setSort] = useState<CatalogSort>('new');
  const [search, setSearch] = useState('');
  const [query, setQuery] = useState('');

  const catalog = useInfiniteQuery({
    queryKey: ['catalog', sort, query],
    queryFn: ({ pageParam }) =>
      fetchCatalog({ sort, q: query || undefined, cursor: pageParam || undefined }),
    initialPageParam: '',
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    retry: false,
  });

  const items = (catalog.data?.pages ?? []).flatMap((page) => page.items).filter(isCatalogVisible);
  const empty = catalog.isSuccess && items.length === 0;

  const submitSearch = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setQuery(search.trim());
  };

  return (
    <main className="catalog">
      <h1>Каталог</h1>
      <p>Публичные открытые опросы. Выберите опрос и расставьте элементы по тирам.</p>

      <div className="catalog-controls">
        <form role="search" onSubmit={submitSearch}>
          <label htmlFor="catalog-search">Поиск</label>
          <input
            id="catalog-search"
            type="search"
            maxLength={100}
            placeholder="Название или описание"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
          <button type="submit">Найти</button>
        </form>
        <div>
          <label htmlFor="catalog-sort">Сортировка</label>
          <select
            id="catalog-sort"
            value={sort}
            onChange={(event) => setSort(event.target.value as CatalogSort)}
          >
            <option value="new">Новые</option>
            <option value="popular">Популярные</option>
          </select>
        </div>
      </div>

      {catalog.isPending && <p role="status">Загружаем каталог…</p>}

      {catalog.isError && (
        <>
          <p role="alert">Не удалось загрузить каталог.</p>
          <button type="button" onClick={() => void catalog.refetch()}>
            Повторить
          </button>
        </>
      )}

      {empty && <p>В каталоге пока нет опросов.</p>}

      {items.length > 0 && (
        <ul className="catalog-list">
          {items.map((item) => (
            <li key={item.id}>
              <article className="catalog-card">
                <h2>
                  <Link to={`/t/${item.slug}`}>{item.title}</Link>
                </h2>
                {item.description && <p>{item.description}</p>}
                <p className="catalog-meta">
                  Автор: {item.author.display_name}. Элементов: {item.items_count}. Ответов:{' '}
                  {item.submissions_count}.
                </p>
              </article>
            </li>
          ))}
        </ul>
      )}

      {catalog.hasNextPage && (
        <button
          type="button"
          disabled={catalog.isFetchingNextPage}
          onClick={() => void catalog.fetchNextPage()}
        >
          {catalog.isFetchingNextPage ? 'Загружаем…' : 'Следующая страница'}
        </button>
      )}
    </main>
  );
}
