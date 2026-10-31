# Tasks

## 1. Аудит TIERL-56

- [x] 1.1 Проверить R1: SQL-фильтр `public`+`open` и исключение unlisted/draft/closed/hidden в `internal/storage/postgres/catalog.go:ListPublic`
- [x] 1.2 Проверить R2: `limit`/`cursor`, сортировка `new`/`popular`, поиск `q` в `internal/web/api/catalog.go:parseCatalogParams` и `ListPublic`
- [x] 1.3 Проверить R3: `GET /sitemap.xml`, состав только public+open, пустой валидный `urlset` в `internal/web/api/catalog.go:sitemap`
- [x] 1.4 Проверить R4: unlisted не в каталоге, поиске и sitemap; страницы `/t/<slug>` и `/r/<id>` отдают `noindex`
- [x] 1.5 Записать вердикты `ok` с доказательствами и отсутствие gap/excess в коде в `docs/audits/catalog.md`

## 2. Восстановление требования

- [x] 2.1 Добавить в capability `catalog` требование «Sitemap публичных опросов» с маршрутом `GET /sitemap.xml`, составом только public+open, абсолютными URL и пустым `urlset`
- [x] 2.2 Сохранить существующие требования каталога, поиска и `noindex` без переписывания и не переименовывать якорь `catalog`
- [x] 2.3 Убедиться, что дельта использует `## ADDED Requirements`, а не MODIFIED

## 3. Follow-up вне вердикта R1–R4

- [ ] 3.1 Уточнить описание `/catalog` в `api/openapi.yaml` до `public`+`open` без изменения схемы и поведения кода

## 4. Проверки

- [x] 4.1 Выполнить `go mod download`
- [x] 4.2 Запустить `go test ./internal/storage/postgres ./internal/web/api ./internal/app` с фильтром по каталогу, sitemap и unlisted
- [x] 4.3 Проверить строки вердиктов R1–R4 в `docs/audits/catalog.md`
- [x] 4.4 Проверить `openspec validate --strict --all --no-interactive`
- [x] 4.5 Подтвердить, что код, тесты, контракт и applied-миграции не менялись
