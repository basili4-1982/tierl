# Аудит TIERL-56 — Каталог и поиск

## Контекст

Задача: TIERL-56 — аудит способности «Каталог и поиск» (якорь цели `catalog`).
Требования аудита: R1 — каталог только `public` и `open`; R2 — пагинация,
сортировка, поиск; R3 — sitemap; R4 — `unlisted` не находится.

Вопреки формулировке задачи живая capability `catalog` в workspace уже
существует: `openspec/specs/catalog/spec.md` восстановлен архивным change
`2026-10-28-add-catalog`. Поэтому аудит сверяет код с живой спекой, а также с
`docs/SPEC.md` §7 (каталог показывает только `public` и `open`, пагинация,
сортировка, поиск) и §11 (`unlisted` не находится через каталог, sitemap и
поиск), `api/openapi.yaml` (`/catalog`, `/sitemap.xml`) и ADR-004. Якорь
`catalog` не переименовывается.

Что проверялось:

- SQL-фильтр каталога и поиска: `visibility = 'public'`, `status = 'open'`,
  `hidden_at IS NULL`;
- keyset-пагинация, границы `limit`, сортировка `new`/`popular`, поиск `q`;
- sitemap: состав URL, отсутствие `unlisted`, пустой ответ;
- недоступность `unlisted` через каталог, поиск, sitemap и `noindex` страниц;
- UI каталога: поиск, сортировка, следующая страница и фильтрация `unlisted`;
- тесты хранилища, HTTP-слоя, sitemap и сквозного сценария.

## Вердикт

- R1: ok — internal/storage/postgres/catalog.go:ListPublic — SQL отбирает только `visibility = 'public'`, `status = 'open'` и `hidden_at IS NULL`, поэтому `unlisted`, черновики, закрытые и скрытые опросы не попадают ни на одну страницу.
- R2: ok — internal/web/api/catalog.go:parseCatalogParams — `limit` ограничен 20/100, непрозрачный `cursor` проверяется, `sort=new|popular` и `q` 1–100 передаются в `ListPublic`, который делает keyset-пагинацию и поиск по названию/описанию.
- R3: ok — internal/web/api/catalog.go:sitemap — `GET /sitemap.xml` собирает только страницы из `allPublicEntries` (public+open), отдаёт валидный `urlset`, а при пустом каталоге — пустой `urlset`; `unlisted` в sitemap не попадает.
- R4: ok — internal/web/api/og.go:handleShareTerm и handleShareResult — `unlisted` строится по `visibility != public`, получает `X-Robots-Tag: noindex, nofollow` и метатег `robots`, а каталог, поиск и sitemap используют только public+open выборку.

## Доказательства по требованиям

### R1 — каталог только public и open

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Маршрут каталога | `internal/web/api/api.go:144` (`GET /api/v1/catalog`), `internal/web/api/catalog.go:37:listCatalog` | Запрос возвращает `200` и страницу `items`/`next_cursor`; проверено `internal/web/api/catalog_test.go:TestCatalogPage` |
| Фильтр `public`+`open` и скрытых | `internal/storage/postgres/catalog.go:18:ListPublic` | В обоих запросах (new и popular) условие `t.visibility = 'public' AND t.status = 'open' AND t.hidden_at IS NULL` |
| Исключение unlisted, draft, closed, hidden | `internal/storage/postgres/store_test.go:312:TestListPublic` | Создаются 5 public+open и по одному unlisted/draft/closed/hidden; в выдаче только public+open, порядок `[p4 p3 p2 p1 p0]` |
| Сквозная проверка через сервис | `internal/app/mvp_e2e_test.go:438:TestMVPUnlistedNotInCatalog` | После создания public и unlisted в каталоге ровно один public-опрос, unlisted отсутствует |
| Пустой каталог | `internal/web/api/catalog.go:48`, `internal/web/api/catalog_test.go:116:TestCatalogEmpty` | Ответ `200` с `items: []` и `next_cursor: null` |

### R2 — пагинация, сортировка, поиск

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Разбор и валидация параметров | `internal/web/api/catalog.go:71:parseCatalogParams` | Неизвестный `sort`, пустой/длинный `q`, нечисловой `limit`, курсор длиннее 512 символов дают `400`; проверено `internal/web/api/catalog_test.go:TestCatalogBadRequests` |
| Границы размера страницы | `internal/tierlist/store.go` (`DefaultPageSize = 20`, `MaxPageSize = 100`), `internal/storage/postgres/catalog.go:129:normalizeLimit`, `internal/web/api/catalog.go:100` | `limit=0` приводится к 20, `limit=1000` — к 100; проверено `TestCatalogClampsPageSize` и `TestListPublic` |
| Keyset-пагинация по курсору | `internal/storage/postgres/catalog.go:18:ListPublic`, `encodeCursor`, `decodeCursor` | Страницы идут без повторов и пропусков, неверный или чужой для сортировки курсор даёт `ErrBadCursor`; проверено `TestListPublic` и `TestListPublicPopularAndSearch` |
| Сортировка `new` | `internal/storage/postgres/catalog.go:18:ListPublic` | `ORDER BY t.created_at DESC, t.id DESC`, по умолчанию при пустом `sort`; порядок проверен `TestListPublic` |
| Сортировка `popular` | `internal/storage/postgres/catalog.go:18:ListPublic`, `encodePopularCursor` | `ORDER BY c.submissions_count DESC, c.created_at DESC, c.id DESC`; при равном числе ответов работает tie-breaker, unlisted с большим числом ответов не появляется; проверено `TestListPublicPopularAndSearch` |
| Неизвестная сортировка | `internal/web/api/catalog.go:71:parseCatalogParams`, `internal/storage/postgres/catalog.go:18:ListPublic` | HTTP-слой и хранилище возвращают `ErrBadSort`, который маппится в `400` (`internal/web/api/api.go:275`); проверено `TestCatalogBadRequests` |
| Поиск по названию и описанию | `internal/storage/postgres/catalog.go:18:ListPublic`, `escapeLike` | `ILIKE` по `title` и `description`, `%`/`_` экранируются; поиск `Go`, `systems`, `100%`, `%`, `_` проверен `TestListPublicPopularAndSearch` |
| Поиск только public+open | `internal/storage/postgres/catalog.go:18:ListPublic` | Поиск применяется внутри выборки с фильтром visibility/status/hidden; `q=Secret` и `q=unlisted` не находят unlisted |
| UI каталога | `web/src/pages/CatalogPage.tsx:13:CatalogPage`, `web/src/features/catalog/api.ts:15:fetchCatalog` | Сортировка, поиск и следующая страница по `next_cursor` отправляются в API; проверено `web/src/pages/CatalogPage.test.tsx` |
| Неверный курсор | `internal/storage/postgres/catalog.go:160:decodeCursor`, `internal/web/api/api.go:275` | Испорченный курсор даёт `400 bad_request`; проверено `TestCatalogBadRequests` и `TestListPublic` |

### R3 — sitemap

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Маршрут `/sitemap.xml` | `internal/web/api/catalog.go:151:sitemap`, `internal/web/api/api.go:145`, `internal/web/handler.go:35` | Маршрут зарегистрирован в API и смонтирован в корне; проверено `TestSitemapMountedAtRoot` |
| Состав sitemap | `internal/web/api/catalog.go:130:allPublicEntries`, `:151:sitemap` | Обход всех страниц `ListPublic` и вывод `<loc>base + "/t/" + slug</loc>`; проверено `TestSitemapOnlyPublicPages` |
| Только public+open | `internal/storage/postgres/catalog.go:18:ListPublic` | Sitemap не имеет отдельного источника данных и наследует фильтр каталога |
| Отсутствие unlisted | `internal/web/api/catalog_test.go:179:TestSitemapOnlyPublicPages`, `:253:TestCatalogAndSitemapWithService` | Проверяется, что в XML нет `unlisted` и slug unlisted-опроса; проверено и на реальном сервисе |
| Пустой sitemap | `internal/web/api/catalog.go:151:sitemap`, `internal/web/api/catalog_test.go:224:TestSitemapEmpty` | Ответ `200` с валидным пустым `<urlset>` |
| Валидный XML и заголовки | `internal/web/api/catalog.go:151:sitemap` | `xml.Header`, `Content-Type: application/xml; charset=utf-8`, `Cache-Control: public, max-age=300`; XML проверяется `xml.Unmarshal` в тестах |

### R4 — unlisted не находится

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Unlisted не в каталоге | `internal/storage/postgres/catalog.go:18:ListPublic` | Фильтр `visibility = 'public'`; проверено `TestListPublic`, `TestMVPUnlistedNotInCatalog`, `TestCatalogAndSitemapWithService` |
| Unlisted не в поиске | `internal/storage/postgres/catalog.go:18:ListPublic` | Поиск идёт только по `title`/`description` внутри public+open выборки, slug не участвует; `TestListPublicPopularAndSearch` проверяет `Secret`/`unlisted` |
| Unlisted не в sitemap | `internal/web/api/catalog.go:130:allPublicEntries`, `:151:sitemap` | Sitemap строится из того же каталога; проверено `TestSitemapOnlyPublicPages` и `TestCatalogAndSitemapWithService` |
| Unlisted по ссылке отдаётся с `noindex` | `internal/web/api/og.go:154:handleShareTerm`, `:181:handleShareResult`, `:72:setPageCacheHeaders`, `:339:metaTags` | `Unlisted: t.Visibility != public`; `X-Robots-Tag: noindex, nofollow` и `<meta name="robots" content="noindex,nofollow">`; проверено `TestShareUnlistedTermAndResult` |
| Публичная страница без `noindex` | `internal/web/api/og.go:72:setPageCacheHeaders`, `:339:metaTags` | Для public не выставляется `X-Robots-Tag` и не выводится `robots: noindex`; проверено `TestShareTermPageAndImage` и `TestShareResultPageAndImage` |
| UI не показывает unlisted даже при плохом ответе | `web/src/pages/CatalogPage.tsx:8:isCatalogVisible` | Дополнительный клиентский фильтр `visibility === 'public' && status === 'open'`; проверено `web/src/pages/CatalogPage.test.tsx` |

## Gap

Gap в коде по R1–R4 не найден: все четыре требования реализованы в
`internal/storage/postgres/catalog.go`, `internal/web/api/catalog.go`,
`internal/web/api/og.go` и `web/src/pages/CatalogPage.tsx` и подтверждены
тестами.

Найден **gap спеки R3**, а не реализации: живая `catalog` содержит
требование «Unlisted вне индексации», где sitemap упомянут только негативно
(`unlisted` MUST NOT попадать в sitemap). В ней нет отдельного нормативного
описания положительного контракта sitemap: маршрута `GET /sitemap.xml`,
обязательного состава только public+open, абсолютных URL `/t/<slug>` и
валидного пустого `urlset` при отсутствии опросов. Код это поведение уже
реализует (`internal/web/api/catalog.go:sitemap`,
`internal/web/api/catalog.go:allPublicEntries`), поэтому требование
восстанавливается change'ем, а не задачей на реализацию.

Вне вердикта R1–R4 есть расхождение контракта: описание `/catalog` в
`api/openapi.yaml:625` говорит «Только `public` и не закрытые удалением
опросы», хотя код и живая спека требуют `public`+`open` и исключают закрытые
и скрытые модерацией. Это задача на уточнение документации, не gap кода.

## Excess

Excess в границах TIERL-56 не найден:

- UI-фильтр `isCatalogVisible` дублирует серверный фильтр, но следует
  требованию «в каталоге только public+open» как защита от некорректного
  ответа, а не добавляет новую возможность;
- `Cache-Control` и заголовки sitemap — стандартное оформление публичного XML,
  не отдельная пользовательская способность;
- fallback `listPublic`/`readableTierlist` на `Store` используется тестами и
  общими страницами, а не расширяет R1–R4;
- поля `items_count`, `submissions_count`, `cover_url`, автор и `created_at`
  каталога следуют `api/openapi.yaml` и карточке каталога, а не лишнему коду.

Удалять код не требуется; задача на удаление не создаётся.

## Решения

1. **R1, R2, R3, R4 — `ok`.** Задач на реализацию отсутствующего кода нет:
   поведение сервиса, тесты, контракт и applied-миграции не меняются.
2. **Gap спеки R3 → change на восстановление требования.** Создан
   `openspec/changes/restore-catalog-sitemap` с capability `catalog`:
   добавляется требование «Sitemap публичных опросов» с маршрутом
   `GET /sitemap.xml`, составом только public+open, абсолютными URL и пустым
   валидным `urlset`. Существующие требования каталога не переписываются.
3. **Excess отсутствует.** Задача на удаление кода не создаётся; соседние
   поля и заголовки относятся к контракту каталога и sitemap.
4. **Расхождение описания OpenAPI вынесено в follow-up.** В change добавлена
   задача уточнить описание `/catalog` до `public`+`open` без изменения кода и
   контрактной схемы.
5. **Способность названа по якорю `catalog`.** Новое имя не вводится:
   каталог, поиск и sitemap — части цели «Каталог и поиск».
6. **Эскалация человеку не требуется:** происхождение требования
   восстанавливается по `docs/SPEC.md` §7/§11, `api/openapi.yaml` и ADR-004.

## Проверки

- `go mod download` — зависимости Go загружены.
- `go test ./internal/storage/postgres ./internal/web/api ./internal/app
  -run 'TestListPublic|TestCatalog|TestSitemap|TestShare|TestMVPUnlisted'`
  — целевые тесты каталога, поиска, sitemap и `noindex` прошли.
- `f=docs/audits/catalog.md; for c in R1 R2 R3 R4; do grep ...` — все четыре
  строки вердикта с доказательствами присутствуют.
- `openspec validate --strict --all --no-interactive` — проверено ниже по
  итогам изменения.

## Границы

Код, тесты, контракт и применённые миграции не изменялись. Изменены только
`docs/audits/catalog.md` и файлы
`openspec/changes/restore-catalog-sitemap/`.
