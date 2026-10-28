# Аудит TIERL-10 — README, self-host и деплой

## Контекст

Задача: «README, self-host и деплой» (PR 59). Заявлено: полностью описанный
self-host — быстрый старт, `docker compose`, переменные окружения, миграции,
`/healthz` и production-деплой; новый человек поднимает сервис одной командой,
а в репозитории нет секретов. Файлы-результат: `README.md` и
`docs/SELF-HOST.md`; контекст: `docker-compose.yml`, `.env.example`,
`Makefile`, `Dockerfile`, `docs/SPEC.md`.

Происхождение требования восстанавливается: `docs/SPEC.md`, раздел 9
(«Запуск одной командой: `docker compose up`, проверка — `GET /healthz`
отвечает 200», автоматические или явные миграции) и раздел 8 (Go JSON API +
React SPA, Postgres, статика в бинарнике); `docs/adr/ADR-004-architecture.md`
(простой деплой: один контейнер плюс Postgres, multi-stage `Dockerfile`,
`embed` статики). Канонической capability в OpenSpec не было, поэтому оформлен
change `restore-self-host` с новой capability `self-host`.

## Вердикт

- TIERL-10: ok — docker-compose.yml:app; README и docs/SELF-HOST.md описывают быстрый старт, compose, env, миграции, healthz и деплой, а код это подтверждает.

## Доказательства по пунктам описания

| Заявлено | Где реализовано | Что подтверждает |
|---|---|---|
| README как точка входа | `README.md:1` (введение), `README.md:11` (стек), `README.md:17` (быстрый старт), `README.md:72` (конфигурация), `README.md:95` (миграции), `README.md:170` (документация) | README объясняет назначение сервиса, требования, запуск, конфигурацию и ссылается на `docs/SPEC.md`, ADR, контракт и `docs/SELF-HOST.md` |
| Self-host документ | `docs/SELF-HOST.md:1` (`# Self-host и деплой tierl`), `docs/SELF-HOST.md:32` (быстрый старт), `docs/SELF-HOST.md:82` (переменные окружения), `docs/SELF-HOST.md:147` (миграции), `docs/SELF-HOST.md:200` (healthcheck), `docs/SELF-HOST.md:238` (production-деплой) | Отдельный документ покрывает поставку, требования, запуск, env, миграции, пробы, TLS/reverse proxy, обновление, откат, backup и диагностику |
| Запуск одной командой | `README.md:28` (`docker compose up --build -d`), `docs/SELF-HOST.md:40`, `docker-compose.yml:4` (сервис `app`), `docker-compose.yml:50` (сервис `postgres`) | Пользователь копирует `.env.example` в `.env` и поднимает `app` + `postgres`; Go, Node и локальный Postgres не нужны |
| Compose собирает и связывает сервисы | `docker-compose.yml:5` (`build: .`), `docker-compose.yml:46` (`depends_on: postgres: condition: service_healthy`), `docker-compose.yml:61` (healthcheck `pg_isready`), `docker-compose.yml:88` (`volumes: pgdata`, `miniodata`) | `app` стартует только после готовности PostgreSQL; данные сохраняются в named volume; необязательные Mailpit/MinIO включаются профилем `tools` |
| `.env.example` и описание env | `.env.example:1`, `.env.example:9` (порты и PostgreSQL), `.env.example:20` (приложение), `.env.example:38` (argon2id), `.env.example:76` (SMTP/почта), `docs/SELF-HOST.md:82` (таблицы env) | Шаблон перечисляет `POSTGRES_*`, `TIERL_*`, почту и хранилище с комментариями и значениями по умолчанию; в документе есть таблицы переменных приложения и Compose |
| Конфигурация читается из env | `internal/platform/config/config.go:Load`, `internal/platform/config/config.go:235` (`TIERL_IP_HASH_SALT: required when TIERL_DATABASE_URL is set`), `internal/platform/config/config.go:loadMail`, `internal/platform/config/config.go:loadStorage` | Приложение читает `TIERL_*` при старте, валидирует значения и требует соль хэширования IP при включённой БД — как описано в README и `.env.example` |
| Миграции автоматически при старте | `internal/app/setup.go:setup` → `postgres.MigrateOnStart`, `internal/storage/postgres/migrate.go:MigrateOnStart`, `internal/storage/postgres/migrate.go:MigrateUp`, `docker-compose.yml:11` (`TIERL_MIGRATE_ON_START`) | При `TIERL_MIGRATE_ON_START=true` (по умолчанию в compose) `app` применяет недостающие миграции до открытия API; ошибка миграции валит старт |
| Миграции вручную | `cmd/migrate/main.go:run` (`migrate up|down`), `Makefile:migrate-up`, `Makefile:migrate-down`, `Makefile:build-migrate`, `migrations/embed.go:FS` | Есть отдельный бинарник и make-цели; SQL-миграции встроены в бинарник через `embed`, имена — timestamp + `_up.sql`/`_down.sql` |
| `/healthz` и `/readyz` | `internal/web/handler.go:NewHandler` (`GET /healthz`, `GET /readyz`), `internal/web/handler.go:healthz`, `internal/web/handler.go:readyz`, `internal/app/app_test.go:TestRun_ServesAndShutsDown`, `internal/web/handler_test.go:TestHealthz`, `internal/web/handler_test.go:TestReadyz` | `/healthz` — liveness `200 ok` без БД; `/readyz` — readiness с таймаутом 2 с, `503 not ready` при недоступной БД; оба покрыты тестами |
| Health-проверки в документации | `README.md:37`, `docs/SELF-HOST.md:200` (таблица `/healthz` и `/readyz`), `docs/SELF-HOST.md:224` (пример Kubernetes-проб), `Makefile:health` | Документация даёт `curl`-проверки, объясняет разницу liveness/readiness и показывает настройку проб |
| Multi-stage Dockerfile | `Dockerfile:2` (стадия `web`, `npm ci` + `npm run build`), `Dockerfile:11` (стадия `build`, `go build`), `Dockerfile:19` (distroless runtime), `Dockerfile:22` (`USER nonroot:nonroot`), `docs/adr/ADR-004-architecture.md` | Образ собирает SPA, встраивает её в Go-бинарник и запускает один процесс от непривилегированного пользователя |
| Production-деплой | `docs/SELF-HOST.md:238` (production), `docs/SELF-HOST.md:243` (TLS/reverse proxy), `docs/SELF-HOST.md:258` (рекомендуемая конфигурация), `docs/SELF-HOST.md:280` (обновление и откат), `docs/SELF-HOST.md:290` (backup/restore) | Описаны TLS на reverse proxy, закрытый PostgreSQL, отдельный шаг миграций, секреты вне репозитория, backup/restore и откат образа |
| Без секретов в репозитории | `.gitignore:1` (`.env`), `.env.example:1` (только dev-значения), `docs/SELF-HOST.md:136` (раздел «Секреты»), `README.md:27` (замена пароля и соли вне локали), `README.md:84` (`.env` не должен попадать в git) | Реальный `.env` исключён из git; в `.env.example` только явно помеченные dev-значения; документация требует заменить `POSTGRES_PASSWORD`, `TIERL_IP_HASH_SALT` и ключи S3 |

## Тестовое покрытие

- `internal/web/handler_test.go:TestHealthz`, `TestHealthz_MethodNotAllowed` и
  `TestReadyz` проверяют liveness, `405` и readiness с `503`.
- `internal/app/app_test.go:TestRun_ServesAndShutsDown` поднимает сервер и
  проверяет `GET /healthz`; `internal/app/integration_test.go:TestSetup_RealDatabase`
  и `TestRun_WithDatabaseGracefulClose` проверяют `/readyz` на реальной
  PostgreSQL, включая `503` после закрытия пула.
- `cmd/migrate/main_test.go:TestRunArgs` проверяет аргументы CLI и ошибку без
  `TIERL_DATABASE_URL`.
- Ограничение: автоматического smoke-теста `docker compose up` (сборка образа,
  ожидание Postgres, применение миграций и `curl /healthz`) нет; CI собирает
  образ (`docker build`), но не поднимает compose. Это остаточный риск, а не
  отсутствие заявленного кода; follow-up вынесен в
  `openspec/changes/restore-self-host/tasks.md` (5.1).

## Решения

1. **Требование было и исчезло из OpenSpec — восстановить.** Создан change
   `restore-self-host` с новой capability `self-host`: запуск одной командой,
   переменные окружения и секреты, миграции, health/ready-пробы и
   production-деплой. Поведение кода не меняется.
2. **Имя capability `self-host` — новое, с обоснованием.** Ни один из якорей
   целей (`accounts`, `tierlists`, `submissions`, `results`, `catalog`,
   `reports`, `sharing`, `author-profile`, `item-images`) не описывает
   эксплуатацию и развёртывание сервиса; это не продуктовая возможность, а
   способ поставки. Обоснование записано в
   `openspec/changes/restore-self-host/proposal.md` и `design.md`.
3. **Gap в коде: нет.** README, `docs/SELF-HOST.md`, compose, `.env.example`,
   Makefile, Dockerfile, обработчики `/healthz`/`/readyz`, миграции и
   развёртывание найдены; заявленные требования выполнены, поэтому общий
   вердикт `ok`.
4. **Excess: нет.** Compose-профиль `tools` (Mailpit/MinIO), S3-конфигурация и
   почтовые переменные следуют `docs/SPEC.md` (разделы 6 и 8) и
   `ADR-004`; отдельного кода без требования в границах TIERL-10 не найдено,
   удалять нечего.
5. **Тестовый gap: вынесен как задача на реализацию.** В
   `openspec/changes/restore-self-host/tasks.md` добавлен follow-up 5.1 на
   smoke-тест compose; код и тесты в границах этого аудита не меняются.
6. **Эскалация человеку не требуется:** происхождение требования
   восстанавливается по SPEC (разделы 8 и 9) и ADR-004, реализация подтверждена
   чтением кода.

## Границы

Код, тесты, контракт и applied-миграции не изменялись. Изменены только
`docs/audits/TIERL-10.md` и файлы `openspec/changes/restore-self-host/`.
