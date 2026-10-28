# Аудит TIERL-7 — OG-картинка и шеринг результата

## Контекст

Задача: OG-картинка и шеринг результата (PR 56). Заявлено: OG/Twitter-теги на
`/t/<slug>` и `/r/<id>`, PNG 1200x630 результата, рендер на сервере, `noindex`
для unlisted, кэш-заголовки, сохранённый SPA fallback.

Происхождение требования восстанавливается: `docs/SPEC.md` (раздел 3 —
`noindex` для unlisted; раздел 7 — OG/Twitter и PNG 1200x630; раздел 11 —
unlisted не попадает в поиск), `docs/adr/ADR-004-architecture.md` (серверная
подстановка `og:*`/`twitter:*`, PNG 1200x630, `noindex`, тесты метатегов) и
`api/openapi.yaml` (видимость `public`/`unlisted`, публичный результат).
Канонической capability в OpenSpec не было, поэтому оформлен change
`restore-sharing` с capability `sharing`.

## Вердикт

- TIERL-7: ok — internal/web/api/og.go:handleShareTerm.OK: серверные страницы `/t/<slug>` и `/r/<id>` отдают OG/Twitter-теги, результату рендерится PNG 1200x630, заданы cache headers и `noindex` для unlisted, SPA fallback сохранён; отдельный тестовый gap описан ниже.

## Доказательства по пунктам описания

| Заявлено | Где реализовано | Что подтверждает |
|---|---|---|
| OG/Twitter-теги на `/t/<slug>` | `internal/web/api/og.go:mountShareRoutes`, `internal/web/api/og.go:handleShareTerm`, `internal/web/api/og.go:metaTags` | Маршрут `GET /t/{slug}` отдаёт HTML с `og:title`, `og:image`, `og:url`, `twitter:card`, `twitter:title`, `twitter:image` и canonical |
| OG/Twitter-теги на `/r/<id>` | `internal/web/api/og.go:handleShareResult`, `internal/web/api/og.go:metaTags`, `internal/app/og_results.go:GetOGResult` | Маршрут `GET /r/{id}` подставляет метатеги из результата участника; значения экранируются `html.EscapeString` |
| Подключение к корневому handler без поломки SPA | `internal/web/handler.go:NewHandler`, `internal/web/static/static.go:Handler`, `internal/web/static/static.go:reserved` | `/t/`, `/r/` и `/og/` монтируются только при наличии `ShareHandler`; остальные пути по-прежнему отдают `index.html` через SPA fallback |
| PNG 1200x630 результата на сервере | `internal/web/api/og_render.go:ogW`, `internal/web/api/og_render.go:ogH`, `internal/web/api/og_render.go:renderOG`, `internal/web/api/og.go:handleOGResult` | Сервер рисует изображение 1200x630 и отдаёт его как `image/png`; для результата дополнительно рисуется доска тиров (`drawBoard`) |
| Кэш-заголовки | `internal/web/api/og.go:setOGCacheHeaders`, `internal/web/api/og.go:setOGCacheHeadersFor`, `internal/web/api/og.go:setPageCacheHeaders`, `internal/web/api/og.go:etag` | PNG кэшируется `public, max-age=86400, s-maxage=604800, immutable` и получает `ETag`; публичные страницы — `public, max-age=300, s-maxage=3600` |
| `noindex` для unlisted | `internal/web/api/og.go:setPageCacheHeaders`, `internal/web/api/og.go:metaTags`, `internal/web/api/og.go:setOGCacheHeadersFor` | Unlisted-страница получает `<meta name="robots" content="noindex,nofollow">` и `X-Robots-Tag`, кэш — `private, no-store`; то же для OG-картинки |
| UI шеринга результата | `web/src/features/result/ResultPage.tsx:onShare`, `web/src/features/result/share.ts:shareLink`, `web/src/routes.tsx` | Кнопка использует `navigator.share`, иначе копирует ссылку `/r/<id>`; маршруты `/t/:slug` и `/r/:id` есть в SPA |
| Тесты шеринга | `web/src/features/result/share.test.ts`, `web/src/features/result/ResultPage.test.tsx`, `internal/web/static/static_test.go:TestHandler` | Покрыты клиентский share/copy и SPA fallback; серверных тестов OG-метатегов, PNG, cache headers и `noindex` нет |

## Тестовый gap

`ADR-004` требует тесты подстановки метатегов, включая экранирование
пользовательского текста. В коде таких тестов нет: `internal/web/api/og.go`,
`internal/web/api/og_render.go` и `internal/app/og_results.go` не имеют
`*_test.go`. Не проверены автоматически:

- наличие и экранирование `og:title`/`og:image`/`twitter:card` на `/t/<slug>`
  и `/r/<id>`;
- размер PNG 1200x630 и `Content-Type: image/png`;
- cache headers и `ETag`;
- `noindex`/`X-Robots-Tag` и `private, no-store` для unlisted.

Это пробел в тестовом покрытии, а не отсутствие заявленной реализации:
обработчики, маршруты, рендер и UI присутствуют. Follow-up вынесен в
`openspec/changes/restore-sharing/tasks.md` (5.1); код и тесты в границах
аудита не меняются.

## Решения

1. **Требование было и исчезло из OpenSpec — восстановить.** Создан change
   `restore-sharing` с capability `sharing`: OG/Twitter-теги, серверный PNG
   1200x630, cache headers, `noindex` для unlisted и сохранение SPA fallback.
   Поведение кода не меняется.
2. **Gap в runtime-коде: нет.** Все заявленные обработчики, маршруты, рендер,
   UI и защита от индексации найдены; основной вердикт — `ok`.
3. **Тестовый gap: вынесен как задача на реализацию.** В change добавлен
   follow-up 5.1 на серверные тесты метатегов, PNG, кэша и `noindex` по
   требованию `ADR-004`; в этом аудите тесты не добавляются.
4. **Excess: нет.** OG-страницы, PNG и SPA-шеринг следуют `docs/SPEC.md`
   (разделы 3, 7, 11) и `ADR-004`; избыточного кода, не вытекающего из
   требования, не найдено, удалять нечего.
5. **Эскалация человеку не требуется:** происхождение требования
   восстанавливается по SPEC, ADR-004 и контракту.

## Границы

Код, тесты, контракт и applied-миграции не изменялись. Изменены только
`docs/audits/TIERL-7.md` и файлы `openspec/changes/restore-sharing/`.
