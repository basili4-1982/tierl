# Tasks

## 1. Происхождение работы

- [x] 1.1 Сопоставить TIERL-7 с целью MVP «каталог и шеринг» (`docs/SPEC.md`, разделы 3, 7 и 11) и решением `ADR-004`; результат записан в `proposal.md` и `design.md`
- [x] 1.2 Сверить поведение с контрактом `api/openapi.yaml`: видимость `public`/`unlisted`, публичный результат и отсутствие unlisted в каталоге
- [x] 1.3 Проверить, что код TIERL-7 не является избыточным: скрытие unlisted, OG-метатеги и PNG следуют SPEC и ADR-004

## 2. Восстановление спеки

- [x] 2.1 Добавить capability `sharing` с требованиями к OG/Twitter-метатегам, PNG 1200x630, кэш-заголовкам, `noindex` для unlisted и SPA fallback
- [x] 2.2 Связать требования с существующими маршрутами `/t/<slug>`, `/r/<id>`, `/og/...` и UI шеринга результата

## 3. Сверка кода со спекой

- [x] 3.1 Проверить маршруты и метатеги: `internal/web/api/og.go:mountShareRoutes`, `handleShareTerm`, `handleShareResult`, `metaTags`
- [x] 3.2 Проверить серверный PNG 1200x630: `internal/web/api/og_render.go:renderOG` и `internal/web/api/og.go:handleOGResult`
- [x] 3.3 Проверить загрузку результата для превью: `internal/app/og_results.go:GetOGResult`
- [x] 3.4 Проверить кэш-заголовки и `noindex`: `setOGCacheHeaders`, `setPageCacheHeaders`, `setOGCacheHeadersFor`, `metaTags`
- [x] 3.5 Проверить сохранение SPA fallback: `internal/web/handler.go:NewHandler` и `internal/web/static/static.go:Handler`
- [x] 3.6 Проверить UI шеринга: `web/src/features/result/ResultPage.tsx`, `web/src/features/result/share.ts` и `web/src/routes.tsx`

## 4. Проверки

- [x] 4.1 Записать вердикт аудита в `docs/audits/TIERL-7.md`
- [x] 4.2 Проверить `openspec validate --strict --all --no-interactive` без ошибок

## 5. Follow-up

- [ ] 5.1 Добавить серверные тесты OG-страниц: наличие и экранирование `og:title`/`og:image`/`twitter:card` на `/t/<slug>` и `/r/<id>`, PNG 1200x630 с `Content-Type: image/png`, cache headers/`ETag` и `noindex`/`X-Robots-Tag` для unlisted. Сейчас `internal/web/api/og.go`, `internal/web/api/og_render.go` и `internal/app/og_results.go` не имеют тестов, хотя `ADR-004` этого требует. Код и тесты в границах аудита не меняются.
