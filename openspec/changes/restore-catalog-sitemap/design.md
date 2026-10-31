# Design

## Context

Живая capability `catalog` уже содержит требования R1–R4: только
`public`+`open` в каталоге, пагинация с `limit`/`cursor`, сортировка
`new`/`popular`, поиск по названию/описанию и исключение unlisted из каталога,
sitemap и индексации. Они пришли из архивного change
`2026-10-28-add-catalog` и совпадают с реализацией:

- `internal/storage/postgres/catalog.go:ListPublic` фильтрует
  `visibility = 'public'`, `status = 'open'`, `hidden_at IS NULL` и делает
  keyset-пагинацию для `new` и `popular`, а также `ILIKE`-поиск;
- `internal/web/api/catalog.go:parseCatalogParams` валидирует `limit`,
  `cursor`, `sort` и `q`, а `:listCatalog` отдаёт страницу каталога;
- `internal/web/api/catalog.go:sitemap` обходит все страницы каталога через
  `allPublicEntries` и выводит `<loc>base + "/t/" + slug</loc>`;
- `internal/web/api/og.go:setPageCacheHeaders` и `metaTags` отдают
  `noindex` для unlisted-страниц и не трогают public.

Пробел находится в спеке, а не в коде: R3 «sitemap» в живой спеке существует
только как запрет попадания unlisted. Положительный контракт — маршрут
`GET /sitemap.xml`, состав только public+open, абсолютные URL и валидный пустой
`urlset` — реализован, но нормативно не описан.

## Goals / Non-Goals

**Goals:**

- зафиксировать в `catalog` отдельное требование к sitemap публичных опросов;
- не менять поведение кода, тестов, маршрутов, схемы контракта и
  applied-миграций;
- вынести расхождение описания `/catalog` в OpenAPI в отдельную follow-up
  задачу.

**Non-Goals:**

- переписывать существующие требования каталога, поиска и `noindex`;
- вводить новую capability или переименовывать якорь `catalog`;
- менять формат `urlset`, состав sitemap или заголовки ответа.

## Decisions

1. **Capability `catalog`, а не новая.** Sitemap — часть публичного каталога и
   публичных страниц `/t/<slug>`. Якорь `catalog` сохраняется.
2. **Новое требование через `ADDED`.** Существующее требование «Unlisted вне
   индексации» не переписывается: оно отвечает за запрет unlisted, а новое
   требование описывает положительный контракт sitemap.
3. **Источник состава — каталог.** Отдельной SQL-выборки для sitemap нет:
   `allPublicEntries` переиспользует `ListPublic`, поэтому фильтр
   `public`+`open` и исключение `hidden_at` не расходятся с каталогом.
4. **Пустой sitemap — валидный XML и `200`.** Это уже реализовано и покрыто
   `TestSitemapEmpty`; в спеке фиксируется как нормативное поведение.
5. **Описание OpenAPI — follow-up, не gap кода.** Уточнение текста
   `/catalog` не влияет на поведение R1–R4 и не меняется в этом аудите.

## Risks / Trade-offs

- [Требование может разойтись с реализацией] → сценарии ссылаются на уже
  существующие `sitemap`, `allPublicEntries` и тесты
  `TestSitemapOnlyPublicPages`/`TestSitemapEmpty`.
- [Добавление требования выглядит как изменение поведения] → в change явно
  указано, что код, тесты, контракт и миграции не меняются; архивация только
  дополняет живую спеку.

## Migration Plan

Изменение документационное и не требует миграций. После архивации требование
«Sitemap публичных опросов» попадёт в `openspec/specs/catalog/spec.md`;
откат — удаление change до архивации. Поведение сервиса не меняется.
