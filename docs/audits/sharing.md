# Аудит TIERL-58 — Шеринг и OG-картинки

## Контекст

Задача: TIERL-58 — аудит способности «Шеринг и OG-картинки» (якорь цели
`sharing`). Требования аудита: R1 — OG/Twitter-метатеги на страницах шеринга;
R2 — серверный PNG-превью 1200x630; R3 — кэширование страниц и превью; R4 —
`noindex` для unlisted; R5 — сохранение SPA fallback.

Вопреки формулировке задачи живая capability `sharing` в workspace уже
существует: `openspec/specs/sharing/spec.md` восстановлен архивным change
`2026-10-28-restore-sharing`. Дополнительно открыт change
`restore-sharing-og-image-url`, который фиксирует канонические URL
`/og/t/<slug>.png` и `/og/r/<id>.png`; его требование пока не входит в живую
спеку. Аудит сверяет код с R1–R5, `docs/SPEC.md` §3/§7/§11 и ADR-004, а также
с OpenAPI и серверными тестами. Якорь `sharing` не переименовывается.

Что проверялось:

- маршруты страниц `/t/{slug}` и `/r/{id}`, метатеги и экранирование;
- маршруты PNG `/t/{slug}/og.png` и `/r/{id}/og.png`, рендер и размер кадра;
- cache-заголовки страниц и PNG, выдача и смысл `ETag`;
- `noindex`/`X-Robots-Tag` и приватный кэш для unlisted, исключение из каталога;
- SPA fallback и зарезервированные серверные префиксы;
- серверные тесты OG-страниц, PNG, кэша и unlisted.

## Вердикт

- R1: ok — internal/web/api/og.go:metaTags — `/t/<slug>` и `/r/<id>` получают серверный HTML с `og:title`, `og:image`, `og:url`, `twitter:card`, описанием и canonical; значения экранируются через `html.EscapeString`.
- R2: ok — internal/web/api/og_render.go:renderOG — обработчики `handleOGTerm`/`handleOGResult` отдают PNG 1200x630 с `Content-Type: image/png`, а для результата рисуется доска тиров из `Tiers`, `Items` и `Placements`.
- R3: gap — `setPageCacheHeaders`, `setOGCacheHeadersFor` и `etag` выставляют cache-заголовки и `ETag`, но повторная валидация не реализована: `If-None-Match` нигде не читается и ответ `304 Not Modified` не отдаётся, хотя требование описывает `ETag` именно для повторной валидации.
- R4: ok — internal/web/api/og.go:setPageCacheHeaders — unlisted получает `<meta name="robots" content="noindex,nofollow">`, `X-Robots-Tag: noindex, nofollow` и `Cache-Control: private, no-store`; каталог отбирает только public+open.
- R5: excess — internal/web/api/og.go:mountShareRoutes — SPA fallback сохранён, но маршруты шеринга дополнительно занимают `/api/v1/og/t/{name}` и `/api/v1/og/r/{name}`, чего R5 с разрешёнными только `/t/`, `/r/` и `/og/` не предусматривает.

## Доказательства по требованиям

### R1 — OG- и Twitter-метатеги на страницах шеринга

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Маршруты страниц | `internal/web/api/og.go:mountShareRoutes`, `internal/web/api/og.go:handleShareTerm`, `internal/web/api/og.go:handleShareResult` | Зарегистрированы `GET /t/{slug}` и `GET /r/{id}`; оба обработчика собирают `shareData` и вызывают `writeSharePage` |
| Набор метатегов | `internal/web/api/og.go:metaTags` | Выводятся `description`, `og:title`, `og:description`, `og:url`, `og:image`, `twitter:card`, `twitter:title`, `twitter:description`, `twitter:image` и `link rel=canonical` |
| Абсолютные canonical и `og:url` | `internal/web/api/og.go:publicBaseURL`, `handleShareTerm`, `handleShareResult` | `CanonicalURL` собирается как `base + "/t/" + slug` или `base + "/r/" + id`; `base` учитывает `X-Forwarded-Proto`/`X-Forwarded-Host` |
| Картинка относится к результату | `internal/web/api/og.go:handleShareResult` | Заголовок результата — `t.Title + " — результат"`, `ImageURL` — `base + "/og/r/" + id + ".png"` |
| Экранирование | `internal/web/api/og.go:metaTags`, `setDocumentTitle`, `standaloneShareHTML` | Пользовательские значения и хост проходят через `html.EscapeString`; проверено `TestShareTermPageAndImage`, `TestOGSharePagesEscapeAllMeta` |
| Серверный HTML, а не пустой SPA | `internal/web/api/og.go:shareHTML`, `internal/web/static/static.go:InjectHead` | Метатеги вставляются в `index.html` перед `</head>`; при отсутствии сборки используется `standaloneShareHTML` |

### R2 — Серверный PNG-превью 1200x630

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Маршруты PNG | `internal/web/api/og.go:mountShareRoutes` | Зарегистрированы `GET /t/{slug}/og.png`, `GET /r/{id}/og.png`, а также канонические `/og/t/{name}` и `/og/r/{name}` |
| Размер и тип | `internal/web/api/og_render.go:ogW`, `ogH`, `renderOG`, `png.Encode` | Кадр создаётся как `image.Rect(0, 0, 1200, 630)`; заголовок `Content-Type: image/png` задаёт `setOGCacheHeaders` |
| Результат показывает доску | `internal/web/api/og.go:handleOGResult`, `internal/web/api/og_render.go:drawBoard` | В `OGSpec` передаются `t.Tiers`, `t.Items` и `res.Placements`; `drawBoard` группирует элементы по тирам участника |
| Данные результата в production | `internal/app/og_results.go:ogResultReader.GetOGResult`, `internal/storage/postgres/tierlists.go:loadStructure` | Результат читает `submissions` и `placements`, а тир-лист загружается вместе с тирами и элементами |
| Неизвестный результат | `internal/web/api/og.go:handleOGResult`, `handleOGTerm` | При ошибке чтения или нечитаемом статусе отдаётся `http.NotFound` |
| Тесты размера | `internal/web/api/og_test.go:assertOGImage`, `internal/web/api/og_share_regression_test.go:TestOGSharePNGFollowsImageURL` | PNG декодируется, проверяются `Content-Type: image/png` и размер 1200x630 |

### R3 — Кэширование страниц и превью

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Публичный кэш страниц | `internal/web/api/og.go:setPageCacheHeaders` | Публичная страница получает `Cache-Control: public, max-age=300, s-maxage=3600` |
| Публичный кэш PNG | `internal/web/api/og.go:setOGCacheHeaders` | PNG получает `Cache-Control: public, max-age=86400, s-maxage=604800, immutable` |
| ETag | `internal/web/api/og.go:setOGCacheHeadersFor`, `etag` | Для PNG выставляется детерминированный ETag из `UpdatedAt` и id/slug; тесты проверяют непустой и стабильный ETag |
| Приватный кэш unlisted | `internal/web/api/og.go:setPageCacheHeaders`, `setOGCacheHeadersFor` | Для unlisted `Cache-Control` переопределяется на `private, no-store` |
| Повторная валидация по ETag | — | Не реализована: поиск по `If-None-Match`, `If-None` и `NotModified` не находит кода; заголовок `ETag` только выдаётся, но не сравнивается с запросом, поэтому `304` не возвращается |
| Тесты `304` | — | `assertOGImage` и `TestOGSharePNGFromImageURLEtagIsStable` проверяют наличие и стабильность ETag, но не условный запрос и не `304` |

### R4 — noindex для unlisted

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Метатег robots | `internal/web/api/og.go:metaTags` | При `shareData.Unlisted` выводится `<meta name="robots" content="noindex,nofollow">` |
| Заголовок `X-Robots-Tag` | `internal/web/api/og.go:setPageCacheHeaders`, `setOGCacheHeadersFor` | Unlisted-страница и unlisted-PNG получают `X-Robots-Tag: noindex, nofollow` |
| Приватный кэш | `internal/web/api/og.go:setPageCacheHeaders`, `setOGCacheHeadersFor` | `Cache-Control` для unlisted — `private, no-store`, без `public` |
| Признак unlisted | `internal/web/api/og.go:handleShareTerm`, `handleShareResult` | `Unlisted: t.Visibility != domain.VisibilityPublic`; проверено `TestShareUnlistedTermAndResult` |
| Unlisted вне каталога | `internal/storage/postgres/catalog.go:ListPublic` | SQL-фильтр `t.visibility = 'public' AND t.status = 'open' AND t.hidden_at IS NULL`; проверено `TestListPublic` и `TestMVPUnlistedNotInCatalog` |

### R5 — Сохранение SPA fallback

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Монтирование share-маршрутов | `internal/web/handler.go:NewHandler` | `/t/`, `/r/` и `/og/` монтируются в корневой mux только при наличии `ShareHandler` |
| SPA fallback для клиентских путей | `internal/web/static/static.go:Handler`, `serveIndex` | Неизвестный путь без расширения, например `/profile`, отдаёт `index.html`; проверено `TestHandler` |
| Зарезервированные префиксы | `internal/web/static/static.go:reserved`, `isReserved` | `/api`, `/healthz` и `/og` не отдают SPA; неизвестный путь под ними получает `404` |
| Лишние share-маршруты | `internal/web/api/og.go:mountShareRoutes` | Помимо `/t/`, `/r/` и `/og/` регистрируются `GET /api/v1/og/t/{name}` и `GET /api/v1/og/r/{name}`; это расширяет занимаемые шерингом префиксы за пределы R5 |

## Gap

Gap в коде R3: `ETag` выдаётся, но не используется для повторной валидации.
В `internal/web/api/og.go` нет чтения `If-None-Match`, нет сравнения с текущим
ETag и нет ответа `304 Not Modified`; `handleOGTerm` и `handleOGResult` всегда
рендерят PNG и пишут `200`. Живая спека `sharing` прямо связывает ETag с
повторной валидацией, поэтому реализация требования неполная. Серверные тесты
`assertOGImage` и `TestOGSharePNGFromImageURLEtagIsStable` закрепляют только
наличие и стабильность заголовка, но не условный запрос.

Gap спеки: канонические URL `/og/t/<slug>.png` и `/og/r/<id>.png`, которые
код публикует в `og:image`/`twitter:image`, не входят в живую
`openspec/specs/sharing/spec.md`; они уже описаны открытым change
`restore-sharing-og-image-url`. Этот пробел не дублируется новым change.

## Excess

- `internal/web/api/og.go:mountShareRoutes` — маршруты
  `GET /api/v1/og/t/{name}` и `GET /api/v1/og/r/{name}` не следуют ни R1–R5,
  ни `docs/SPEC.md` §7, ни ADR-004, ни `api/openapi.yaml`. R5 разрешает
  шерингу занимать только `/t/`, `/r/` и `/og/`, поэтому API-префиксные
  алиасы — лишний код. Решение: задача на удаление в change.

Другого excess в границах TIERL-58 не найдено: `/sitemap.xml` относится к
capability `catalog`, fallback на `standaloneShareHTML` нужен при отсутствии
собранной статики, а поля `og:site_name`, `og:locale`, `og:image:*`,
`twitter:title` и `twitter:image` — стандартное раскрытие требования об
OG/Twitter-метатегах, а не отдельная возможность.

## Решения

1. **R1, R2, R4 — `ok`.** Задач на реализацию нет: страницы, PNG 1200x630,
   `noindex` и исключение unlisted из каталога уже реализованы и покрыты
   тестами; поведение кода не меняется.
2. **R3 gap → change на восстановление требования и задача на реализацию.**
   Создан `openspec/changes/restore-sharing-audit` с capability `sharing`
   (якорь не переименован). В дельту добавлено требование «Условная валидация
   OG-превью по ETag»: при совпадении `If-None-Match` с текущим ETag
   возвращается `304` без тела, при несовпадении — `200` с PNG; cache- и
   `noindex`-заголовки сохраняются. Задача на реализацию и тест `304` вынесены
   в `tasks.md`, сам код в границах аудита не меняется.
3. **R5 excess → задача на удаление кода.** В `tasks.md` вынесено удаление
   API-префиксных маршрутов `/api/v1/og/t/{name}` и `/api/v1/og/r/{name}` из
   `mountShareRoutes` с проверкой, что `/t/`, `/r/`, `/og/` и SPA fallback
   работают как раньше. Существующее требование R5 не переписывается.
4. **Gap спеки канонического URL уже закрывается отдельным change.** Созданный
   ранее `restore-sharing-og-image-url` добавляет в `sharing` требование к
   `/og/t/<slug>.png` и `/og/r/<id>.png`; новый change его не дублирует.
5. **Способность названа по якорю `sharing`.** Новое имя не вводится:
   шеринг, OG-метатеги, PNG-превью и SPA fallback относятся к одной цели.
6. **Эскалация человеку не требуется:** происхождение требования
   восстанавливается по `docs/SPEC.md` §3/§7/§11, ADR-004 и живой спеке
   `sharing`.

## Проверки

- `go mod download` — зависимости Go загружены.
- `go test ./internal/web/... ./internal/app ./internal/tierlist
  ./internal/storage/postgres -count=1` — все перечисленные пакеты прошли,
  поведение кода не менялось.
- `f=docs/audits/sharing.md` и grep по строкам вердикта R1–R5 — файл и все
  пять строк с доказательствами присутствуют.
- `openspec validate --strict --all --no-interactive` — проверено по итогам
  изменения.

## Границы

Код, тесты, контракт и применённые миграции не изменялись. Изменены только
`docs/audits/sharing.md` и файлы
`openspec/changes/restore-sharing-audit/`.
