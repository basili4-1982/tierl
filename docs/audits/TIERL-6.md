# Аудит TIERL-6 — работа без спеки

## Вердикты

- TIERL-6: gap — заявленный `internal/web/api/catalog.go` и маршрут `GET /api/v1/catalog` отсутствуют, а `web/src/pages/CatalogPage.tsx` остаётся заглушкой; сортировка `popular`, поиск `q` и sitemap не реализованы.

## Что заявлено и что найдено

Задача TIERL-6 заявляет каталог из `public`+`open` опросов с пагинацией,
сортировкой по новизне/популярности, поиском и исключением `unlisted` из
каталога, sitemap и поиска с отдачей `noindex`. Файлы-результат:
`internal/web/api/catalog.go`, `internal/web/api/api.go`,
`web/src/pages/CatalogPage.tsx`.

### Есть в коде

- Исключение непубличных опросов на уровне хранилища:
  `internal/storage/postgres/catalog.go:15` (`Store.ListPublic`) фильтрует
  `visibility = 'public' AND status = 'open' AND hidden_at IS NULL`; это
  подтверждено `internal/storage/postgres/store_test.go:312` (`TestListPublic`)
  и `internal/app/mvp_e2e_test.go:304` (`TestMVPUnlistedNotInCatalog`).
- Keyset-пагинация по `(created_at, id)` и курсор:
  `internal/storage/postgres/catalog.go:15`, `internal/tierlist/store.go:102`
  (`PageParams`), `internal/storage/postgres/catalog.go:70`
  (`encodeCursor`).
- Порядок «сначала новые»: `ORDER BY t.created_at DESC, t.id DESC` в
  `internal/storage/postgres/catalog.go:44`.
- `noindex` для unlisted на страницах `/t/<slug>` и `/r/<id>`:
  `internal/web/api/og.go:72` (`setPageCacheHeaders`) и
  `internal/web/api/og.go:339` (`metaTags`), включаются в
  `internal/web/api/og.go:154` (`handleShareTerm`).
- Контракт `GET /api/v1/catalog` с `sort`, `q`, `cursor`, `limit` объявлен:
  `api/openapi.yaml:560` (`/catalog`, `operationId: listCatalog`).

### Чего нет

- Нет файла `internal/web/api/catalog.go` и обработчика каталога.
- В `internal/web/api/api.go:104` (`New`) среди зарегистрированных маршрутов
  (блок регистрации, `internal/web/api/api.go:143–178`) нет
  `GET /api/v1/catalog`; запрос попадёт в заглушку `Prefix+"/"` и вернёт
  `404`.
- `sort=new|popular` не реализован: в `tierlist.PageParams`
  (`internal/tierlist/store.go:102`) нет поля сортировки, а `ListPublic`
  всегда сортирует по `created_at DESC`; сортировки по числу ответов нет.
- Поиск `q` не реализован: параметра нет в `PageParams`, в SQL
  `internal/storage/postgres/catalog.go` нет фильтра по названию/описанию.
- UI каталога отсутствует: `web/src/pages/CatalogPage.tsx:4` —
  заглушка «Каталог опросов скоро появится», не вызывает API и не содержит
  пагинации, сортировки или поиска.
- Sitemap отсутствует в коде: `sitemap` встречается только в документации
  (`docs/SPEC.md`, `docs/adr/ADR-004-architecture.md`, `api/openapi.yaml`), нет
  ни маршрута, ни генератора.
- Нет тестов уровня API и UI для каталога: проверки покрывают только
  `Store.ListPublic` и исключение unlisted на уровне хранилища; нет проверок
  `sort=popular`, `q`, маршрута `/api/v1/catalog` и страницы каталога.

## Решения

- **gap → change на восстановление требования и задачи на реализацию.**
  Оформлен `openspec/changes/restore-catalog`: capability `catalog` (якорь цели
  `catalog`) восстанавливает требование из `docs/SPEC.md`, раздел 7 и 11,
  `api/openapi.yaml` (`/catalog`) и `ADR-004`; в `tasks.md` вынесены задачи на
  отсутствующий обработчик, сортировку, поиск, sitemap, UI и тесты.
- **excess не найден.** Реализованные части каталога (фильтр `public`+`open`,
  пагинация, `noindex`) следуют из требования, поэтому задача на удаление кода
  не создаётся.
- Происхождение работы восстанавливается по `docs/SPEC.md`, `api/openapi.yaml`
  и `ADR-004`; эскалация человеку не требуется.
