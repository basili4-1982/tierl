# Аудит TIERL-60 — self-host

## Контекст

Задача: TIERL-60 — аудит способности «self-host» (якорь цели `self-host`).
Требования аудита: R1 — запуск одной командой; R2 — порядок старта и готовность
базы; R3 — переменные окружения и секреты; R4 — миграции; R5 — health и
readiness пробы; R6 — production-деплой.

Вопреки формулировке задачи живая capability `self-host` в workspace уже
существует: `openspec/specs/self-host/spec.md` восстановлен архивным change
`2026-10-28-restore-self-host`. Поэтому аудит сверяет код с живой спекой, а
также с `docs/SPEC.md` §9 (запуск одной командой и health-проба, миграции по
timestamp), §8 (один процесс, Postgres и встроенная статика) и ADR-004
(один контейнер плюс Postgres, multi-stage образ). Якорь `self-host` не
переименовывается.

Что проверялось:

- `docker compose` и `Dockerfile`: сборка `app`, `postgres`, публикация порта,
  named volume;
- порядок старта: healthcheck `pg_isready`, `depends_on`, миграции при старте;
- конфигурация `TIERL_*`, `.env.example`, исключение `.env` из git и образа,
  обязательность соли при заданной базе;
- миграции: автоматический и ручной запуск, откат, имена, остановка при
  ошибке;
- `GET /healthz` и `GET /readyz`, их зависимость от базы и таймаут;
- документация production-деплоя, backup, обновления и отката, multi-stage
  образ со встроенной SPA;
- smoke-проверка compose в `Makefile` и CI, которой пока нет в живой спеке.

## Вердикт

- R1: ok — docker-compose.yml:app — `app` собирается из `Dockerfile`, `postgres` поднимается рядом, UI/API публикуется на `127.0.0.1:${TIERL_PORT:-8080}:8080`, а данные PostgreSQL хранятся в named volume `pgdata`; README и `docs/SELF-HOST.md` дают быстрый старт одной командой.
- R2: ok — docker-compose.yml:depends_on — `app` зависит от `postgres` с `condition: service_healthy`, а healthcheck `postgres` использует `pg_isready`; после старта `internal/app/setup.go:setup` вызывает `postgres.MigrateOnStart` при `TIERL_MIGRATE_ON_START`.
- R3: ok — internal/platform/config/config.go:Load — настройки читаются из `TIERL_*`, `TIERL_IP_HASH_SALT` обязателен при заданном `TIERL_DATABASE_URL`, `.env.example` перечисляет переменные приложения и compose, а `.env` исключён через `.gitignore` и `.dockerignore`.
- R4: ok — internal/storage/postgres/migrate.go:MigrateOnStart — автоматические миграции при старте, ручные `MigrateUp`/`MigrateDown` и `cmd/migrate/main.go:run` работают с встроенными SQL-файлами с timestamp и суффиксами `_up.sql`/`_down.sql`; ошибка миграции валит старт.
- R5: ok — internal/web/handler.go:readyz — `GET /healthz` отвечает `200 ok` без обращения к базе, а `GET /readyz` проверяет PostgreSQL через `pool.Ping` с таймаутом 2 с и отдаёт `503 not ready` при недоступности; оба маршрута покрыты тестами.
- R6: ok — docs/SELF-HOST.md:238 — production-деплой описывает TLS на reverse proxy, закрытый PostgreSQL, секреты вне репозитория, отдельный шаг миграций, backup, обновление и откат образа; `Dockerfile` собирает один multi-stage образ со встроенной SPA.

## Доказательства по требованиям

### R1 — Запуск одной командой

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Быстрый старт одной командой | `README.md:28` (`docker compose up --build -d`), `docs/SELF-HOST.md:32:Быстрый старт` | Документация требует только Docker Engine и Compose v2, копирование `.env.example` в `.env` и одну команду |
| Сборка `app` из `Dockerfile` | `docker-compose.yml:5` (`build: .`), `Dockerfile:2:web`, `Dockerfile:11:build`, `Dockerfile:19:runtime` | Образ собирает SPA и Go-бинарник в multi-stage сборке, затем запускает один процесс |
| PostgreSQL в compose | `docker-compose.yml:50:postgres`, `docker-compose.yml:51` (`postgres:17-alpine`), `docker-compose.yml:60` (volume `pgdata`) | База поднимается вместе с приложением, данные лежат в named volume |
| Порт UI/API и loopback | `docker-compose.yml:8` (`127.0.0.1:${TIERL_PORT:-8080}:8080`), `.env.example:11` (`TIERL_PORT=8080`), `docs/SELF-HOST.md:32` | Снаружи порт задаёт `TIERL_PORT`, по умолчанию он привязан только к loopback |
| Остановка сохраняет данные | `docker-compose.yml:88` (`volumes: pgdata`), `docs/SELF-HOST.md:75` | `docker compose down` сохраняет volume, `down -v` удаляет его осознанно |
| Без локальных Go, Node.js, PostgreSQL | `docs/SELF-HOST.md:19:Требования`, `README.md:17` | Go и Node нужны только для сборки/разработки на хосте, обычный запуск идёт из образа |

### R2 — Порядок старта и готовность базы

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Ожидание готовности PostgreSQL | `docker-compose.yml:46:depends_on`, `docker-compose.yml:48` (`condition: service_healthy`) | `app` не запускается, пока `postgres` не станет healthy |
| Healthcheck `pg_isready` | `docker-compose.yml:61:healthcheck`, `docker-compose.yml:62` (`pg_isready -U ... -d ...`) | Проверка выполняется с интервалом 3 с, таймаутом 3 с и 10 попытками |
| Миграции после готовности базы | `internal/app/setup.go:33` (`postgres.MigrateOnStart`), `docker-compose.yml:13` (`TIERL_MIGRATE_ON_START` по умолчанию `true`) | При старте `app` применяет недостающие миграции до открытия API |
| Порядок описан | `docs/SELF-HOST.md:45`, `README.md:33` | Документация перечисляет шаги: сборка, healthcheck базы, миграции, HTTP-сервер |

### R3 — Переменные окружения и секреты

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Чтение `TIERL_*` при старте | `internal/platform/config/config.go:126:Load` | `Load` читает адрес, базу, миграции, логи, argon2, rate limiting, почту и хранилище; вызывается из `cmd/tierl/main.go:run` |
| Полный шаблон переменных | `.env.example:1`, `.env.example:9` (compose), `.env.example:20` (приложение), `.env.example:76` (почта) | Есть значения по умолчанию, комментарии и пометки секретов; переменные приложения сопоставлены с `config.Load` |
| Обязательность соли | `internal/platform/config/config.go:235` (`TIERL_IP_HASH_SALT: required when TIERL_DATABASE_URL is set`), `internal/platform/config/config_test.go:64:TestLoad_Invalid` | Небезопасная конфигурация без соли отклоняется, тест проверяет пустую и пробельную соль |
| `.env` вне git и образа | `.gitignore:1` (`.env`), `.dockerignore:2` (`.env`), `.env.example:1` | Реальный `.env` не попадает ни в репозиторий, ни в контекст сборки образа |
| Секреты заменяются вне локали | `docs/SELF-HOST.md:136:Секреты`, `README.md:27`, `docker-compose.yml:16` | Документация требует заменить `POSTGRES_PASSWORD`, `TIERL_IP_HASH_SALT` и ключи S3; в шаблоне только dev-значения |
| Отсутствие реальных секретов | `.env.example`, `docker-compose.yml`, `docs/SELF-HOST.md` | Поиск приватных ключей и токенов по репозиторию совпадений не дал; в файлах только явно помеченные dev-значения |

### R4 — Миграции

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Автоматическое применение | `internal/app/setup.go:33` → `internal/storage/postgres/migrate.go:67:MigrateOnStart`, `internal/storage/postgres/migrate.go:40:MigrateUp` | При `TIERL_MIGRATE_ON_START=true` применяются только недостающие миграции; повторный запуск на актуальной схеме — no-op |
| Ручное применение и откат | `cmd/migrate/main.go:run`, `internal/storage/postgres/migrate.go:45:MigrateDown`, `Makefile:36:migrate-up`, `Makefile:41:migrate-down` | CLI принимает `up|down`, make-цели вызывают его с `TIERL_DATABASE_URL`; `down` откатывает все применённые миграции |
| Встроенные SQL-миграции | `migrations/embed.go:FS`, `internal/storage/postgres/migrate.go:newMigrate` | Файлы встроены через `embed` и читаются мигратором из `iofs` |
| Имена миграций | `migrations/` | Все пары имеют timestamp и суффиксы `_up.sql` / `_down.sql`; применённые миграции не редактировались |
| Ошибка миграции останавливает старт | `internal/app/setup.go:33`, `internal/app/app.go:Run`, `cmd/tierl/main.go:run` | Ошибка возвращается из `setup` до запуска HTTP-сервера, `main` завершает процесс с кодом 1 |

### R5 — Health и readiness пробы

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Liveness без базы | `internal/web/handler.go:26` (`GET /healthz`), `internal/web/handler.go:50:healthz` | Обработчик всегда пишет `200 ok` и не обращается к базе |
| Readiness с PostgreSQL | `internal/web/handler.go:27` (`GET /readyz`), `internal/web/handler.go:56:readyz`, `internal/app/setup.go:133` (`Ready: pool.Ping`) | `readyz` вызывает `pool.Ping` с таймаутом 2 с (`readyTimeout`) |
| Ответ `503 not ready` | `internal/web/handler.go:56:readyz` | При ошибке проверки статус `503` и тело `not ready`; процесс при этом не завершается |
| Документация и reverse proxy | `docs/SELF-HOST.md:200:Healthcheck`, `docs/SELF-HOST.md:224` (пример Kubernetes), `README.md:37`, `Makefile:56:health` | Описаны разница liveness/readiness, `curl`-проверки и использование в orchestrator |
| Тесты | `internal/web/handler_test.go:TestHealthz`, `TestHealthz_MethodNotAllowed`, `TestReadyz`, `internal/app/app_test.go:TestRun_ServesAndShutsDown` | Проверены `200 ok`, `405` и `503 not ready`; старт приложения проверяется через `/healthz` |

### R6 — Production-деплой

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| TLS на reverse proxy | `docs/SELF-HOST.md:243:TLS и reverse proxy`, `docs/SELF-HOST.md:245` | Примеры Caddy и nginx, передача `Host`/`X-Forwarded-*` |
| PostgreSQL не доступен из интернета | `docker-compose.yml:57`, `docker-compose.yml:58`, `docs/SELF-HOST.md:253` | Порт базы привязан только к `127.0.0.1`, документация требует не публиковать его наружу |
| Секреты вне репозитория | `docs/SELF-HOST.md:258:Рекомендуемая конфигурация`, `docs/SELF-HOST.md:136:Секреты` | Требуются секрет-хранилище или `.env` с правами `600`, ключи не хранятся в образе |
| Отдельный шаг миграций | `docs/SELF-HOST.md:153`, `docs/SELF-HOST.md:163`, `docs/SELF-HOST.md:280` | Для нескольких реплик рекомендуется `TIERL_MIGRATE_ON_START=false` и отдельный шаг деплоя |
| Backup, обновление, откат | `docs/SELF-HOST.md:280:Обновление и откат`, `docs/SELF-HOST.md:290:Данные и резервные копии` | Есть `pg_dump`/`pg_restore`, порядок обновления и откат на предыдущий образ без автоматического отката схемы |
| Один multi-stage образ со SPA | `Dockerfile:1`, `Dockerfile:2:web`, `Dockerfile:11:build`, `Dockerfile:19:runtime`, `Dockerfile:16` | SPA собирается в стадии `web`, бинарник — в стадии `build`, runtime-стадия содержит только `/tierl` |
| Smoke-проверка compose | `Makefile:63:smoke`, `.github/workflows/ci.yml:71` | `make smoke` и CI собирают и поднимают compose, ждут healthy PostgreSQL и проверяют `/healthz` и `/readyz`, затем удаляют контейнеры и volume |

## Gap

Gap в коде по R1–R6 не найден: все шесть требований реализованы в compose,
`Dockerfile`, конфигурации, миграциях, HTTP-слое и документации, а ключевые
участки подтверждены тестами и CI.

Найден **gap спеки capability**, а не реализации: в живой
`openspec/specs/self-host/spec.md` нет требования к smoke-проверке self-host,
хотя репозиторий уже содержит цель `make smoke` (`Makefile:63`) и шаг CI
`smoke test docker compose` (`.github/workflows/ci.yml:71`). Проверка собирает
образ, поднимает compose, ждёт healthcheck PostgreSQL, проверяет `/healthz` и
`/readyz` и всегда удаляет контейнеры и volume. Это поведение появилось из
follow-up TIERL-31 архивного change `2026-10-28-restore-self-host`
(`tasks.md:31`), но в нормативном описании capability отсутствует. Требование
восстанавливается change'ем, а не задачей на реализацию.

Документационное расхождение вне вердикта R1–R6: `make smoke` не упомянут в
таблице команд `README.md:150` и в `docs/SELF-HOST.md`. Это задача на
уточнение документации, а не gap кода.

## Excess

Excess в границах TIERL-60 не найден:

- профиль `tools` (`mailpit`, `minio`) не запускается по умолчанию и следует
  `docs/SPEC.md` §8 и ADR-004 для локальной почты и S3-хранилища;
- `make smoke` и шаг CI проверяют R2 и R5, а не добавляют новую возможность;
  их нормативное описание восстанавливается в change;
- переменные argon2, rate limiting, почты и хранилища принадлежат другим
  возможностям (`accounts`, `item-images`) и перечислены в `.env.example`
  по требованию R3 о полном списке переменных;
- `Makefile:health`, `make logs`, `make ps` и документация диагностики —
  эксплуатационная обвязка R1/R5, а не отдельный код без требования.

Удалять код не требуется; задача на удаление не создаётся.

## Решения

1. **R1–R6 — `ok`.** Задач на реализацию отсутствующего кода нет: поведение
   сервиса, тесты, контракт и applied-миграции не меняются.
2. **Gap спеки capability → change на восстановление требования.** Создан change
   `restore-self-host-audit` с capability `self-host` (якорь не
   переименован). В дельту добавлено требование «Smoke-проверка self-host»:
   `make smoke` и CI собирают и поднимают compose, ждут healthy PostgreSQL,
   проверяют `200` от `/healthz` и `/readyz`, затем всегда удаляют контейнеры
   и volume. Существующие R1–R6 не переписываются.
3. **Excess отсутствует.** Задача на удаление кода не создаётся; профиль
   `tools`, smoke-проверка и эксплуатационные make-цели относятся к self-host
   и соседним возможностям.
4. **Документационный пробел вынесен в follow-up.** В
   `openspec/changes/restore-self-host-audit/tasks.md` добавлена задача
   описать `make smoke` в `README.md` и `docs/SELF-HOST.md`; в границах
   аудита документация вне `docs/audits/` и `openspec/changes/` не меняется.
5. **Способность названа по якорю `self-host`.** Новое имя не вводится:
   запуск, конфигурация, миграции, пробы и production-деплой — части одной
   цели.
6. **Эскалация человеку не требуется:** происхождение требования
   восстанавливается по `docs/SPEC.md` §8/§9, ADR-004 и живой спеке
   `self-host`.

## Проверки

- `go mod download` — зависимости Go загружены.
- `go test ./internal/platform/config ./internal/web/... ./internal/app
  -run 'TestLoad|TestHealthz|TestReadyz|TestRun_' -count=1` — целевые тесты
  конфигурации, проб и запуска приложения прошли; интеграционные тесты с
  Docker не запускались, потому что Docker в окружении недоступен.
- `f=docs/audits/self-host.md; for c in R1 R2 R3 R4 R5 R6; do grep ...` — файл
  и все шесть строк вердикта с доказательствами присутствуют.
- `openspec validate --strict --all --no-interactive` — проверено по итогам
  изменения.

## Границы

Код, тесты, контракт и применённые миграции не изменялись. Изменены только
`docs/audits/self-host.md` и файлы
`openspec/changes/restore-self-host-audit/`.
