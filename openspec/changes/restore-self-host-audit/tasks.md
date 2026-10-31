# Tasks

## 1. Аудит TIERL-60

- [x] 1.1 Проверить R1: `docker-compose.yml:app`, `build: .`, порт `TIERL_PORT` на loopback, named volume `pgdata`, `README.md:28` и `docs/SELF-HOST.md:32`
- [x] 1.2 Проверить R2: `docker-compose.yml:depends_on` с `condition: service_healthy`, healthcheck `pg_isready`, `internal/app/setup.go:setup` и `postgres.MigrateOnStart`
- [x] 1.3 Проверить R3: `internal/platform/config/config.go:Load`, обязательность `TIERL_IP_HASH_SALT` при `TIERL_DATABASE_URL`, `.env.example`, `.gitignore` и `.dockerignore`
- [x] 1.4 Проверить R4: `internal/storage/postgres/migrate.go:MigrateOnStart`, `MigrateUp`, `MigrateDown`, `cmd/migrate/main.go:run`, `migrations/embed.go:FS` и имена миграций
- [x] 1.5 Проверить R5: `internal/web/handler.go:healthz`, `readyz`, `readyTimeout`, `Ready: pool.Ping`, тесты `TestHealthz` и `TestReadyz`
- [x] 1.6 Проверить R6: multi-stage `Dockerfile`, `docs/SELF-HOST.md:238` (TLS/reverse proxy, секреты, отдельные миграции, backup, обновление и откат), `make smoke` и CI
- [x] 1.7 Записать вердикты R1–R6 с доказательствами, gap спеки capability и отсутствие excess в `docs/audits/self-host.md`
- [x] 1.8 Сверить живую capability `self-host` и архивный change `2026-10-28-restore-self-host`, чтобы не дублировать существующие требования

## 2. Восстановление требования

- [x] 2.1 Добавить в capability `self-host` требование «Smoke-проверка self-host»: сборка образа, поднятие compose, ожидание healthy PostgreSQL, `200` от `/healthz` и `/readyz`, обязательная очистка
- [x] 2.2 Сохранить существующие требования R1–R6 без переписывания и не переименовывать якорь `self-host`

## 3. Follow-up по документации

- [ ] 3.1 Описать `make smoke` и шаг CI smoke-проверки в `README.md` (таблица команд) и `docs/SELF-HOST.md` (раздел запуска или эксплуатации), не меняя код и поведение

## 4. Проверки

- [x] 4.1 Выполнить `go mod download`
- [x] 4.2 Запустить целевые тесты конфигурации, проб и запуска приложения; интеграционные тесты с Docker пропущены, потому что Docker в окружении недоступен
- [x] 4.3 Проверить строки вердиктов R1–R6 в `docs/audits/self-host.md`
- [x] 4.4 Проверить `openspec validate --strict --all --no-interactive`
- [x] 4.5 Подтвердить, что код, тесты, контракт и applied-миграции не менялись
