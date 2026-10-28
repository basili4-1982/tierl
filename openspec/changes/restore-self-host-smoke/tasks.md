# Tasks

## 1. Происхождение работы

- [x] 1.1 Сопоставить TIERL-31 с `docs/SPEC.md` §9, живой capability `self-host` и пунктом 5.1 архивированного change `restore-self-host`; результат записан в `proposal.md` и `design.md`
- [x] 1.2 Проверить, что заявленный smoke-тест есть в коде: `.github/workflows/ci.yml:jobs.docker`, `.github/workflows/ci.yml:smoke test docker compose`, `Makefile:smoke`
- [x] 1.3 Классифицировать работу как необходимую: gap и excess в коде не найдены, удаление кода не требуется

## 2. Восстановление спеки

- [x] 2.1 Добавить в capability `self-host` требование к smoke-проверке в CI: сборка образа, healthy PostgreSQL, `GET /healthz` и `GET /readyz`
- [x] 2.2 Использовать существующий якорь цели `self-host`; новое имя capability не вводить, так как проверяется запуск compose и пробы self-host
- [x] 2.3 Связать требование с существующими `.github/workflows/ci.yml`, `docker-compose.yml`, `Makefile:smoke` и `internal/web/handler.go`

## 3. Сверка кода со спекой

- [x] 3.1 Проверить сборку и запуск: `.github/workflows/ci.yml:67` (`docker compose config`), `:70` (`docker compose build`), `:85` (`docker compose up -d`)
- [x] 3.2 Проверить ожидание готовности PostgreSQL: `.github/workflows/ci.yml:87-99`, `docker-compose.yml:61-65` (`pg_isready`), `docker-compose.yml:46-48` (`service_healthy`)
- [x] 3.3 Проверить пробы: `internal/web/handler.go:26` (`GET /healthz`), `:27` (`GET /readyz`), `:50:healthz`, `:56:readyz`, тесты `internal/web/handler_test.go:TestHealthz` и `TestReadyz`
- [x] 3.4 Проверить HTTP-проверки и очистку в CI: `.github/workflows/ci.yml:101-115`, `:74-82`
- [x] 3.5 Записать вердикт `ok` с доказательствами в `docs/audits/TIERL-31.md`

## 4. Проверки

- [x] 4.1 Проверить `openspec validate restore-self-host-smoke --strict` без ошибок
- [x] 4.2 Проверить `openspec validate --strict --all --no-interactive` без ошибок
