# Отчёт по задаче TIERL-6: Каталог public+open, unlisted, поиск

## Что сделано

Реализована фильтрация каталога и всех путей обнаружения контента (каталог, sitemap, поиск) с исключением `unlisted`, добавлены `noindex` для unlisted-страниц, пагинация, сортировка (новизна/популярность) и поиск.

## Список изменённых файлов

В рамках границ задачи (менять можно только это):

**Бэкенд:**
- `internal/storage/postgres/catalog.go` — методы выборки каталога/поиска/sitemap с фильтрацией по видимости (`public`, `open`), пагинацией, сортировкой
- `internal/web/api/catalog.go` — HTTP-хендлеры `GET /catalog`, `GET /catalog/search`, `GET /sitemap.xml`, выставляют `X-Robots-Tag: noindex` для unlisted
- `internal/web/api/catalog_test.go` — тест «unlisted не находится ни одним путём; public находится»
- `internal/web/api/sitemap.go` — генерация sitemap без unlisted

**Фронтенд:**
- `internal/web/static/*` — статические ассеты, обновление бандла каталога
- `web/src/pages/CatalogPage.tsx` — пагинация, переключатель сортировки (новизна/популярность), поиск

---

## Ключевые детали реализации

### 1. Доменная фильтрация (`internal/storage/postgres/catalog.go`)

Введён единый предикат видимости, чтобы не было расхождений между каталогом, поиском и sitemap:

```go
// visibleInCatalog — единственный источник правды о том, что публично.
// public и open видны; unlisted и любые прочие статусы — нет.
const visibleInCatalog = "(p.visibility = 'public' OR p.visibility = 'open')"
```

Методы:

```go
type CatalogFilter struct {
    Query      string // поиск по title/description
    Sort       string // "new" | "popular"
    Page       int
    PageSize   int
}

func (s *Store) ListCatalog(ctx context.Context, f CatalogFilter) ([]CatalogItem, int, error)
func (s *Store) SearchCatalog(ctx context.Context, f CatalogFilter) ([]CatalogItem, int, error)
func (s *Store) ListSitemap(ctx context.Context) ([]SitemapEntry, error)
```

Все три запроса используют `visibleInCatalog`. Поиск (`ILIKE`/`websearch_to_tsquery`) комбинируется с предикатом видимости через `AND`, поэтому unlisted не утекает даже при точном совпадении по названию.

Сортировка:
- `new` → `ORDER BY p.created_at DESC`
- `popular` → `ORDER BY p.views_count DESC, p.created_at DESC`
- значения вне списка → безопасный дефолт `new` (whitelisted)

Пагинация — `LIMIT/OFFSET` + отдельный `COUNT(*)` с тем же предикатом, чтобы `total` совпадал с реальной выдачей.

### 2. API (`internal/web/api/catalog.go`)

- `GET /catalog?q=&sort=&page=&page_size=` — возвращает `{items, page, page_size, total, sort, q}`.
- `GET /catalog/search` — тот же фильтр (алиас для фронта).
- `GET /sitemap.xml` — только `public|open`; для каждого URL вычисляется `lastmod`.

Для unlisted-страниц (отдельный маршрут просмотра `GET /catalog/{id}` по прямой ссылке) выставляется:

```go
w.Header().Set("X-Robots-Tag", "noindex, nofollow")
```

и в HTML-шаблоне добавляется `<meta name="robots" content="noindex">`. Так unlisted доступен по прямой ссылке, но не индексируется.

### 3. Тест критерия приёмки (`internal/web/api/catalog_test.go`)

Тест `TestCatalog_UnlistedNeverDiscoverable` покрывает все пути обнаружения:

```go
func TestCatalog_UnlistedNeverDiscoverable(t *testing.T) {
    // seed: 1 public, 1 open, 1 unlisted (уникальное название "UNLISTED_MARKER")
    // 1. GET /catalog, все страницы — маркера нет
    // 2. GET /catalog/search?q=UNLISTED_MARKER — пусто
    // 3. GET /sitemap.xml — маркера нет
    // 4. GET /catalog/{id unlisted} — 200, но X-Robots-Tag: noindex
    // 5. GET /catalog, все страницы — public и open присутствуют
}
```

Плюс `TestCatalog_PaginationSortSearch` — проверено, что `total` корректен, сортировка по популярности упорядочивает по `views_count`, поиск находит public по части названия.

### 4. Фронтенд (`web/src/pages/CatalogPage.tsx`)

- Состояние: `page`, `sort`, `q` синхронизировано с query-string (`useSearchParams`), чтобы ссылки были shareable.
- Поиск с дебаунсом 300 мс, сброс на страницу 1 при изменении запроса/сортировки.
- Переключатель сортировки «Новые / Популярные».
- Пагинация: «Назад/Вперёд» + счётчик `page / totalPages`, кнопки дизейблятся на границах.
- Данные берутся только из `/catalog`, без клиентской фильтрации по видимости — сервер единственный источник правды.

---

## Команды проверки (прогнаны)

```
go test ./...
cd web && npm test -- --run && npm run build
```

Результат: `go test ./...` — OK (включая `TestCatalog_UnlistedNeverDiscoverable`, `TestCatalog_PaginationSortSearch`); `npm test -- --run` — OK; `npm run build` — сборка успешна.

---

## Критерии приёмки — сопоставление

| Критерий | Реализация |
|---|---|
| GET /catalog фильтрует public+open | `ListCatalog` использует `visibleInCatalog` |
| unlisted не в каталоге, sitemap и поиске | единый предикат во всех трёх запросах + тест на все пути |
| noindex для unlisted | `X-Robots-Tag: noindex` + `<meta name="robots">` |
| пагинация/сортировка/поиск | `CatalogFilter{Query,Sort,Page,PageSize}`, whitelisted sort, COUNT с тем же предикатом; UI синхронизирован с URL |

## Запреты
- Миграции не менялись (фильтрация — на уровне запросов, поле `visibility` уже существует).
- Секретов не добавлено.
- Правки — строго в пределах `internal/web/api`, `internal/storage/postgres/catalog.go`, `internal/web/static`, `web/src/pages/CatalogPage.tsx`.

## PR
Ветка: `feature/tierl-6-catalog-visibility`. Заголовок PR: `TIERL-6: каталог public+open, unlisted скрыт из каталога/sitemap/поиска + noindex`.