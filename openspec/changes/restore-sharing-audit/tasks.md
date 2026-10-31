# Tasks

## 1. Аудит TIERL-58

- [x] 1.1 Проверить R1: маршруты `/t/{slug}` и `/r/{id}`, `og:*`/`twitter:*`, canonical и экранирование в `internal/web/api/og.go:handleShareTerm`, `handleShareResult`, `metaTags`
- [x] 1.2 Проверить R2: PNG 1200x630, `Content-Type: image/png`, доска тиров результата в `internal/web/api/og_render.go:renderOG`, `drawBoard`, `internal/web/api/og.go:handleOGTerm`, `handleOGResult`
- [x] 1.3 Проверить R3: cache-заголовки и ETag в `internal/web/api/og.go:setPageCacheHeaders`, `setOGCacheHeadersFor`, `etag`; зафиксировать отсутствие обработки `If-None-Match` и `304`
- [x] 1.4 Проверить R4: `noindex`, `X-Robots-Tag`, `private, no-store` и исключение unlisted из каталога в `internal/web/api/og.go` и `internal/storage/postgres/catalog.go:ListPublic`
- [x] 1.5 Проверить R5: SPA fallback и зарезервированные префиксы в `internal/web/handler.go:NewHandler` и `internal/web/static/static.go:Handler`; найти API-префиксные OG-маршруты как excess
- [x] 1.6 Записать вердикты R1–R5 с доказательствами, gap R3 и excess R5 в `docs/audits/sharing.md`
- [x] 1.7 Сверить живую capability `sharing` и открытый change `restore-sharing-og-image-url`, чтобы не дублировать требование канонического URL

## 2. Восстановление требования R3

- [x] 2.1 Добавить в capability `sharing` требование «Условная валидация OG-превью по ETag» с ответами `304`/`200` и сохранением cache- и `noindex`-заголовков
- [x] 2.2 Сохранить требования R1–R5 без переписывания и не переименовывать якорь `sharing`
- [ ] 2.3 Реализовать обработку `If-None-Match` в `internal/web/api/og.go:handleOGTerm`, `handleOGResult`: при совпадении с текущим ETag отдавать `304` без рендера и без тела, сохраняя cache- и `X-Robots-Tag`-заголовки
- [ ] 2.4 Добавить регрессионные тесты: совпадающий `If-None-Match` даёт `304` без тела, несовпадающий — `200` с PNG 1200x630; проверить канонические маршруты, алиасы и unlisted с `private, no-store` и `noindex`

## 3. Excess R5

- [ ] 3.1 Удалить `GET /api/v1/og/t/{name}` и `GET /api/v1/og/r/{name}` из `internal/web/api/og.go:mountShareRoutes`
- [ ] 3.2 Проверить, что после удаления `/t/`, `/r/`, `/og/` и SPA fallback работают как раньше, а ссылок на API-префиксные OG-маршруты не осталось

## 4. Проверки

- [x] 4.1 Выполнить `go mod download`
- [x] 4.2 Запустить `go test ./internal/web/... ./internal/app ./internal/tierlist ./internal/storage/postgres -count=1`
- [x] 4.3 Проверить строки вердиктов R1–R5 в `docs/audits/sharing.md`
- [x] 4.4 Проверить `openspec validate --strict --all --no-interactive`
- [x] 4.5 Подтвердить, что код, тесты, контракт и applied-миграции не менялись
