# tierl

Сервис тир-листов: автор создаёт опрос с элементами и тирами, участники
раскладывают элементы и видят, насколько их ответ похож на ответы других.
Go-бэкенд отдаёт JSON API и встроенную React SPA, данные хранятся в PostgreSQL.

Проект в стадии MVP. Требования и границы — [`docs/SPEC.md`](docs/SPEC.md),
решения — [`docs/adr/`](docs/adr/), контракт HTTP API — [`api/openapi.yaml`](api/openapi.yaml).
Развёртывание и эксплуатация — [`docs/SELF-HOST.md`](docs/SELF-HOST.md).

## Стек

- Go 1.25, PostgreSQL 17;
- React 19 + Vite, сборка встраивается в Go-бинарник через `embed`;
- Docker Compose для локального запуска, multi-stage `Dockerfile` для образа.

## Быстрый старт с Docker Compose

Нужны Docker Engine с плагином Compose v2 и свободные порты `8080` (UI/API)
и `5432` (PostgreSQL). Для запуска не нужны Go, Node.js или локальный Postgres.

```bash
git clone https://github.com/basili4-1982/tierl.git
cd tierl
cp .env.example .env
# Для локального запуска достаточно значений по умолчанию.
# Перед публикацией сервиса обязательно замените POSTGRES_PASSWORD и TIERL_IP_HASH_SALT.
docker compose up --build -d
```

При старте `app` ждёт готовности PostgreSQL, применяет миграции
(`TIERL_MIGRATE_ON_START=true`) и начинает слушать порт `8080`.

Проверка, что сервис поднялся:

```bash
curl -fsS http://localhost:8080/healthz   # ok — процесс жив
curl -fsS http://localhost:8080/readyz    # ok — база доступна
docker compose ps                         # app и postgres в состоянии running/healthy
docker compose logs -f app                # JSON-логи, строка "http server started"
```

Откройте <http://localhost:8080> — это SPA. JSON API доступен по префиксу
`/api/v1`, например <http://localhost:8080/api/v1/tierlists/slug>.

Остановить сервис:

```bash
docker compose down       # сохранить данные PostgreSQL
docker compose down -v    # удалить контейнеры и volume с данными
```

### Текущее состояние

Контракт API шире текущей реализации: в сборке уже есть health-пробы,
статика и маршруты `/api/v1/tierlists`, часть auth/submissions/results/catalog
ещё в разработке. Поэтому отдельные страницы UI могут работать на моках и
обращаться к ещё не реализованным эндпоинтам.

### Необязательные сервисы

Mailpit и MinIO не запускаются по умолчанию. Они поднимаются профилем `tools`:

```bash
docker compose --profile tools up -d
```

- Mailpit: SMTP `localhost:1025`, веб-интерфейс <http://localhost:8025>;
- MinIO: S3 API `localhost:9000`, консоль <http://localhost:9001>
  (логин/пароль по умолчанию `tierl` / `tierl-secret` — только для разработки).

## Конфигурация

Все настройки приложения читаются из переменных окружения с префиксом
`TIERL_`. Полный список с комментариями и значениями по умолчанию —
[`.env.example`](.env.example); подробное описание — в
[`docs/SELF-HOST.md`](docs/SELF-HOST.md#переменные-окружения).

Docker Compose читает `.env` из корня репозитория. Внутри контейнера адрес
базы собирается из `POSTGRES_USER`, `POSTGRES_PASSWORD` и `POSTGRES_DB`
(внутренний порт всегда `5432`), поэтому менять пароль нужно в
`POSTGRES_PASSWORD`. `POSTGRES_PORT` задаёт только порт на хосте.
`TIERL_DATABASE_URL` из `.env.example` предназначен для запуска приложения
на хосте (например, через `make run`).

Важно:

- `TIERL_IP_HASH_SALT` обязателен, если задан `TIERL_DATABASE_URL`. Это не
  пароль, а соль для хэширования IP при rate limiting: используйте длинную
  случайную строку (`openssl rand -hex 32`) и не меняйте её без необходимости;
- `.env` содержит секреты и не должен попадать в git (он уже в `.gitignore`);
- для production включайте TLS на reverse proxy и не публикуйте PostgreSQL
  наружу.

## Миграции

Миграции лежат в [`migrations/`](migrations/) и встроены в бинарник.
Способы применения:

```bash
# Вариант 1: автоматически при старте контейнера (по умолчанию в compose)
docker compose up -d --build

# Вариант 2: вручную с хоста, база из compose на localhost:5432
make migrate-up

# Откатить все миграции (разрушительно!)
make migrate-down
```

`make migrate-up` использует `DATABASE_URL`, по умолчанию
`postgres://tierl:tierl@localhost:5432/tierl?sslmode=disable`. Если вы меняли
`POSTGRES_*` или `POSTGRES_PORT` в `.env`, передайте соответствующий URL. Для
другой базы:

```bash
make migrate-up DATABASE_URL='postgres://user:pass@host:5432/tierl?sslmode=require'
```

Эквивалент без make: `TIERL_DATABASE_URL=... go run ./cmd/migrate up`.
Применённые миграции не редактируются — изменения оформляются новой миграцией
с timestamp-именем. Перед откатом или обновлением production-базы сделайте
резервную копию.

## Локальная разработка

Понадобятся Go 1.25, Node.js 22 и Docker (только для PostgreSQL).

```bash
# 1. База
docker compose up -d postgres

# 2. Переменные окружения для процесса на хосте
export TIERL_DATABASE_URL='postgres://tierl:tierl@localhost:5432/tierl?sslmode=disable'
export TIERL_IP_HASH_SALT='dev-only-ip-hash-salt-change-me'

# 3. Схема и запуск бэкенда на :8080
make migrate-up
make run

# 4. Фронтенд с hot reload на :5173 (/api проксируется на :8080)
make web-install
cd web && npm run dev
```

Чтобы Go отдавал собранный фронтенд на `:8080`, выполните `make web-build`
и перезапустите `make run`. Если `internal/web/static/dist` пуст, страница
вернёт `frontend is not built: run make web-build`.

## Команды

| Команда | Что делает |
|---|---|
| `make up` / `make down` | `docker compose up --build` / `down` |
| `make ps` / `make logs` | состояние контейнеров / логи `app` |
| `make health` | запросы к `/healthz` и `/readyz` |
| `make migrate-up` / `make migrate-down` | применить / откатить миграции |
| `make run` | запустить Go-сервер локально |
| `make build` | собрать бинарник в `bin/tierl` |
| `make build-migrate` | собрать бинарник миграций в `bin/migrate` |
| `make test` | `go test -race ./...` |
| `make cover` | покрытие `./internal/...` с порогом `COVER_MIN` (52, только растёт) |
| `make fmt` / `make lint` / `make vet` | форматирование и линтеры |
| `make web-install` / `make web-build` / `make web-test` | фронтенд: зависимости / сборка / проверки |

Проверка без Docker: `go test ./...`. Интеграционные тесты PostgreSQL
используют testcontainers и без Docker пропускаются; в CI выставлен
`TIERL_REQUIRE_DOCKER=1`, поэтому там они обязательны.

## Документация

- [`docs/SPEC.md`](docs/SPEC.md) — требования MVP;
- [`docs/SELF-HOST.md`](docs/SELF-HOST.md) — self-host, деплой, env, миграции, health;
- [`docs/adr/`](docs/adr/) — архитектурные решения;
- [`api/README.md`](api/README.md) — работа с OpenAPI-контрактом;
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — процесс разработки;
- [`SECURITY.md`](SECURITY.md) — сообщение об уязвимостях.

## Лицензия

MIT — см. [`LICENSE`](LICENSE).
