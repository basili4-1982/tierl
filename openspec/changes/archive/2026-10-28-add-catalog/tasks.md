# Tasks

## 1. Спека и происхождение

- [x] 1.1 Зафиксировать требование `catalog` из `docs/SPEC.md` (разделы 7 и 11), `api/openapi.yaml` (`/catalog`) и `ADR-004`; проверка — `openspec validate add-catalog --strict`
- [x] 1.2 Сверить существующие части с требованием: `Store.ListPublic` фильтрует `public`+`open`, keyset-пагинация и `noindex` для unlisted уже есть; проверка — `internal/storage/postgres/catalog.go` и `internal/web/api/og.go`
- [x] 1.3 Записать вердикт `gap` с доказательствами в `docs/audits/TIERL-6.md`; проверка — строка `- TIERL-6: gap — …` и ссылки на файлы/символы

## 2. Хранилище каталога

- [x] 2.1 Добавить `Sort` и `Query` в `tierlist.PageParams` и прокинуть их в `ListPublic`; проверка — `go test ./internal/tierlist/...`
- [x] 2.2 Реализовать сортировку `popular` по числу ответов с детерминированным tie-breaker и расширить курсор под сортировку; проверка — новый тест в `internal/storage/postgres/store_test.go` на порядок и переход страниц
- [x] 2.3 Реализовать поиск `q` по названию и описанию с экранированием `%`/`_` и фильтром только `public`+`open`; проверка — тест поиска в `internal/storage/postgres/store_test.go`
- [x] 2.4 Проверить, что unlisted не попадает ни в `new`, ни в `popular`, ни в поиск; проверка — расширенный `TestListPublic`/`TestMVPUnlistedNotInCatalog`

## 3. HTTP-слой каталога

- [x] 3.1 Создать `internal/web/api/catalog.go` с обработчиком `GET /api/v1/catalog` и валидацией `sort`, `q`, `limit`, `cursor`; проверка — новый `internal/web/api/catalog_test.go` на `200` и `400`
- [x] 3.2 Зарегистрировать маршрут в `New` (`internal/web/api/api.go`) без изменения существующих маршрутов; проверка — тест запроса `/api/v1/catalog` возвращает `200`, а не `404`
- [x] 3.3 Добавить `GET /sitemap.xml`, отдающий только public+open URL и валидный пустой `urlset`; проверка — тест, что unlisted slug в ответе отсутствует
- [x] 3.4 Проверить, что ответ каталога соответствует схеме `TierlistPage` и не раскрывает unlisted; проверка — контрактный тест `internal/web/api`

## 4. UI каталога

- [x] 4.1 Добавить `web/src/features/catalog/api.ts` с типизированным вызовом `GET /catalog`; проверка — `npm run typecheck` в `web/`
- [x] 4.2 Заменить заглушку `web/src/pages/CatalogPage.tsx` списком с сортировкой, поиском, следующей страницей и состояниями загрузки/пустоты/ошибки; проверка — тест `CatalogPage` на MSW
- [x] 4.3 Проверить, что UI не показывает unlisted; проверка — тест с public и unlisted в ответе мока
- [x] 4.4 Проверить линтер и сборку фронтенда; проверка — `npm run lint && npm run build` в `web/`

## 5. Интеграционные проверки

- [x] 5.1 Прогнать `go test -race ./...` и убедиться, что каталог и sitemap не ломают другие маршруты
- [x] 5.2 Прогнать `openspec validate add-catalog --strict` без ошибок

## Workflow follow-up

- После ревью и реализации архивировать изменение, чтобы capability `catalog` попала в `openspec/specs/catalog/spec.md`.
