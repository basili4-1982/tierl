# Аудит TIERL-38 — дефект TIERL-30: тесты OG-картинки шли по старому URL

## Контекст

Задача: TIERL-38 (PR 102) — дефект, найденный ревьюером при проверке TIERL-30
«Серверные тесты OG-страниц и PNG» (коммит
`58e74779812efaf459ea83e377f6b30881ac48cb`). Заявлено: в `og.go` фактическая
ссылка `og:image` изменена на `/og/t/<slug>.png` и `/og/r/<id>.png`, но
`TestSharePNG` и `TestSharePNG_ETagIsStable` по-прежнему запрашивают
`/t/<slug>/og.png` и `/r/<id>/og.png`, поэтому не подтверждают, что
публикуемая OG-ссылка отдаёт PNG 1200x630 с нужными заголовками.

Цепочка требования: `docs/SPEC.md` §7 (OG/Twitter-теги, PNG 1200x630 на
сервере), §3 (`noindex` для unlisted) и §11 (unlisted вне поиска);
`docs/adr/ADR-004-architecture.md` (серверная подстановка метатегов, PNG
1200x630, тесты метатегов). Канонический адрес превью в OpenSpec не был
зафиксирован: `openspec/specs/sharing/spec.md` называет только алиасы
`/t/<slug>/og.png` и `/r/<id>/og.png`. Поэтому восстановлено требование в
capability `sharing` (change `restore-sharing-og-image-url`), без изменения
поведения кода.

## Вердикт

- TIERL-38: ok — internal/web/api/og.go:handleShareTerm — `og:image` публикует абсолютный `/og/t/<slug>.png` (для результата `/og/r/<id>.png`), а тесты `TestShareTermPageAndImage` и `TestOGSharePNGFollowsImageURL` извлекают URL из `og:image` и запрашивают именно его; дефект TIERL-30 закрыт.

## Доказательства по пунктам описания

| Заявлено | Где реализовано | Чем подтверждается |
|---|---|---|
| `og:image` указывает на `/og/t/<slug>.png` и `/og/r/<id>.png` | `internal/web/api/og.go:154:handleShareTerm`, `:175` (`ImageURL: base + "/og/t/" + slug + ".png"`), `:181:handleShareResult`, `:203` (`ImageURL: base + "/og/r/" + id + ".png"`), `:339:metaTags` | `metaTags` выводит `shareData.ImageURL` в `og:image` (`og.go:351`) и `twitter:image` (`:359`); URL собирается от `publicBaseURL`, то есть абсолютный |
| Новые URL отдают PNG | `internal/web/api/og.go:55:mountShareRoutes`, `:58-59` (`GET /og/t/{name}`, `GET /og/r/{name}`), `:83:handleOGTerm`, `:116:handleOGResult` | Обработчики снимают суффикс `.png` и рендерят PNG через `renderOG` (`internal/web/api/og_render.go:63`); старые алиасы `/t/{slug}/og.png` и `/r/{id}/og.png` остаются зарегистрированными (`og.go:62-63`) |
| Тесты идут по URL из `og:image` | `internal/web/api/og_test.go:274:assertOGPageImage` | `assertOGPageImage` читает `og:image` со страницы (`og_test.go:279`) и запрашивает полученный `imageURL` (`:283`), а не старый путь; так работают `TestShareTermPageAndImage` (`:291`), `TestShareResultPageAndImage` (`:306`) и `TestShareUnlistedTermAndResult` (`:316`) |
| Регрессия закреплена отдельно | `internal/web/api/og_share_regression_test.go:117:regOGImageTarget`, `:134:TestOGSharePNGFollowsImageURL` | `regOGImageTarget` извлекает `og:image`, проверяет абсолютный хост и возвращает `RequestURI`; `TestOGSharePNGFollowsImageURL` запрашивает этот адрес для опроса и результата, public и unlisted |
| PNG 1200x630 и заголовки | `internal/web/api/og_test.go:234:assertOGImage`, `internal/web/api/og.go:66:setOGCacheHeaders`, `:302:setOGCacheHeadersFor`, `:313:etag` | Тест проверяет `Content-Type: image/png`, `ETag`, cache headers, `X-Robots-Tag` для unlisted и размер `1200x630` (`og_test.go:268-269`); `TestOGSharePNGFromImageURLEtagIsStable` (`og_share_regression_test.go:195`) проверяет стабильность `ETag` по URL из `og:image` |
| Старые тесты `TestSharePNG`/`TestSharePNG_ETagIsStable` больше не запрашивают старый адрес | `internal/web/api/og_test.go`, `internal/web/api/og_share_regression_test.go` | Функций с такими именами в коде нет; их роль выполняют `TestShareTermPageAndImage`/`TestShareResultPageAndImage` и `TestOGSharePNGFollowsImageURL`/`TestOGSharePNGFromImageURLEtagIsStable`, которые берут адрес из `og:image` |

## Gap

Gap в коде по заявленному дефекту нет. Описанная проблема «тесты запрашивают
старые `/t/<slug>/og.png` и `/r/<id>/og.png` вместо ссылки из `og:image`» в
текущем коде отсутствует: `assertOGPageImage` и
`TestOGSharePNGFollowsImageURL` запрашивают именно опубликованный
`og:image`. Старые маршруты остаются совместимыми алиасами и не подменяют
проверку канонической ссылки.

Gap спецификации: канонические URL `/og/t/<slug>.png` и `/og/r/<id>.png` и
требование идти в тестах по ссылке со страницы не были зафиксированы в
`openspec/specs/sharing/spec.md` (там только старые алиасы). Это восстановлено
change'ем `restore-sharing-og-image-url`, поведение кода не меняется.

## Excess

Excess в границах TIERL-38 нет. Маршруты `/og/...` следуют требованию к
публикуемой OG-ссылке, а `/t/<slug>/og.png` и `/r/<id>/og.png` — действующему
требованию `sharing` о PNG-превью и совместимости. Кода, который не следует ни
из какого требования и должен быть удалён, в рамках этого дефекта не найдено.

## Решения

- **Кодовый gap/excess не найден.** Задача на реализацию или удаление кода не
  создаётся: исправление тестов уже в репозитории.
- **Gap спецификации → change на восстановление требования.** Создан
  `openspec/changes/restore-sharing-og-image-url` с capability `sharing`:
  канонический URL `og:image`/`twitter:image`, PNG 1200x630 с cache- и
  `noindex`-правилами и проверка изображения по ссылке со страницы.
- **Способность названа по существующему якорю `sharing`.** Новое имя не
  вводится: дефект относится к серверной отдаче OG-превью и шерингу.
- **Эскалация человеку не требуется:** происхождение требования
  восстанавливается по `docs/SPEC.md`, `ADR-004` и текущей спеке `sharing`.

## Проверки

- `go mod download` — зависимости проекта загружены.
- `go test ./internal/web/api -run 'TestShare|TestOGShare|TestOG' -count=1` —
  пакет с OG-страницами, PNG, cache headers и `noindex` проходит
  (`ok code.sarnas.ru/tierl/internal/web/api`).
- `openspec validate --strict --all --no-interactive` — проверено ниже по
  итогам изменения.
- Docker для этих unit-тестов не нужен; фактический прогон на PostgreSQL в
  границах TIERL-38 не требуется.

## Границы

Код, тесты, контракт и applied-миграции не изменялись. Изменены только
`docs/audits/TIERL-38.md` и файлы
`openspec/changes/restore-sharing-og-image-url/`.
