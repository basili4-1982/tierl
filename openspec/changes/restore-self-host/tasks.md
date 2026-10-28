# Tasks

## 1. Происхождение работы

- [x] 1.1 Сопоставить TIERL-10 с нефункциональным требованием MVP (`docs/SPEC.md`, раздел 9) и решением `ADR-004`; результат записан в `proposal.md` и `design.md`
- [x] 1.2 Проверить, что заявленные артефакты есть в коде и документации: `README.md`, `docs/SELF-HOST.md`, `docker-compose.yml`, `.env.example`, `Makefile`, `Dockerfile`, `/healthz`, `/readyz` и миграции
- [x] 1.3 Классифицировать работу как необходимую: избыточных артефактов и мёртвого кода в границах аудита не найдено, удаление не требуется

## 2. Восстановление спеки

- [x] 2.1 Добавить capability `self-host` с требованиями к запуску одной командой, env и секретам, миграциям, health/ready-пробам и production-деплою
- [x] 2.2 Обосновать новое имя capability: ни один якорь цели (`accounts`, `tierlists`, `submissions`, `results`, `catalog`, `reports`, `sharing`, `author-profile`, `item-images`) не описывает развёртывание и эксплуатацию
- [x] 2.3 Связать требования с существующими `docker compose`, `TIERL_*`, `GET /healthz`, `GET /readyz`, `make migrate-up` и `cmd/migrate`

## 3. Сверка кода со спекой

- [x] 3.1 Проверить быстрый старт и compose: `README.md:28`, `docs/SELF-HOST.md:40`, `docker-compose.yml:app`, `docker-compose.yml:postgres`, `depends_on` и healthcheck `pg_isready`
- [x] 3.2 Проверить env и секреты: `.env.example`, `internal/platform/config/config.go:Load`, проверка обязательного `TIERL_IP_HASH_SALT`, `.gitignore` (`.env`)
- [x] 3.3 Проверить миграции: `internal/app/setup.go:setup`, `internal/storage/postgres/migrate.go:MigrateOnStart`, `MigrateUp`, `cmd/migrate/main.go:run`, `migrations/embed.go:FS`
- [x] 3.4 Проверить пробы: `internal/web/handler.go:NewHandler`, `healthz`, `readyz`; тесты `internal/web/handler_test.go:TestHealthz`, `TestReadyz`, `internal/app/app_test.go:TestRun_ServesAndShutsDown`
- [x] 3.5 Проверить multi-stage образ и production-деплой: `Dockerfile`, `docs/SELF-HOST.md:238` (TLS/reverse proxy, обновление, backup)
- [x] 3.6 Записать вердикт `ok` с доказательствами в `docs/audits/TIERL-10.md`

## 4. Проверки

- [x] 4.1 Проверить `openspec validate restore-self-host --strict` без ошибок
- [x] 4.2 Проверить `openspec validate --strict --all --no-interactive` без ошибок

## 5. Follow-up

- [ ] 5.1 Добавить smoke-тест `docker compose up` в CI: собрать образ, дождаться healthy PostgreSQL, проверить `GET /healthz` и `GET /readyz`. Код и тесты в границах аудита не меняются.
- [ ] 5.2 После ревью архивировать изменение, чтобы capability `self-host` попала в `openspec/specs/self-host/spec.md`.
