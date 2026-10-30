# Tasks

## 1. Аудит TIERL-38

- [x] 1.1 Сопоставить дефект TIERL-38 с кодом: `internal/web/api/og.go:mountShareRoutes`, `handleShareTerm`, `handleShareResult`, `metaTags`
- [x] 1.2 Проверить, что тесты берут адрес из `og:image`: `internal/web/api/og_test.go:assertOGPageImage` и `internal/web/api/og_share_regression_test.go:TestOGSharePNGFollowsImageURL`
- [x] 1.3 Записать вердикт `ok` в `docs/audits/TIERL-38.md`

## 2. Восстановление требования

- [x] 2.1 Добавить в capability `sharing` требование к каноническому URL `/og/t/<slug>.png` и `/og/r/<id>.png` с PNG 1200x630, cache-заголовками и `noindex` для unlisted
- [x] 2.2 Зафиксировать, что серверные тесты запрашивают изображение по URL из `og:image`, а старые алиасы остаются совместимыми

## 3. Проверки

- [x] 3.1 Убедиться, что поведение кода, тестов, контракта и миграций не изменилось
- [x] 3.2 Проверить `openspec validate --strict --all --no-interactive` без ошибок
