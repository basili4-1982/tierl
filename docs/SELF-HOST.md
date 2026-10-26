# Self-host и деплой tierl

Документ описывает запуск tierl на своём сервере: Docker Compose, переменные
окружения, миграции, health-пробы, обновления и базовую эксплуатацию.
Требования к продукту — в [`SPEC.md`](SPEC.md), контракт API — в
[`../api/openapi.yaml`](../api/openapi.yaml).

## Что входит в поставку

- `app` — один контейнер: Go-бинарник отдаёт JSON API `/api/v1`, health-пробы
  и встроенную React SPA на одном origin;
- `postgres` — PostgreSQL 17, единственное обязательное внешнее хранилище;
- `mailpit` и `minio` — необязательные сервисы для локальной разработки,
  включаются профилем `tools`.

Один процесс и одна база — минимальная конфигурация. TLS, ограничение доступа
и резервное копирование выполняются на уровне хостинга.

## Требования

- Linux-сервер или локальная машина с Docker Engine и плагином Compose v2;
- 1 CPU и 1–2 ГБ RAM достаточно для MVP; argon2id по умолчанию требует
  около 64 MiB памяти на одновременное хэширование пароля;
- свободные порты `8080` (UI/API) и `5432` (PostgreSQL) либо изменённые
  `TIERL_PORT` / `POSTGRES_PORT`;
- исходящий доступ не требуется, если не используется внешний S3/почтовый
  сервис.

Go 1.25 и Node.js 22 нужны только для сборки/разработки на хосте. Обычный
запуск идёт из готового Docker-образа.

## Быстрый старт

```bash
git clone https://github.com/basili4-1982/tierl.git
cd tierl
cp .env.example .env
# Для локального запуска можно оставить значения по умолчанию.
# Перед публикацией замените POSTGRES_PASSWORD и TIERL_IP_HASH_SALT.
docker compose up --build -d
```

Что происходит:

1. Compose собирает multi-stage образ: фронтенд собирается Node.js и
   встраивается в Go-бинарник.
2. `postgres` запускается с healthcheck `pg_isready`; `app` ждёт его готовности.
3. `app` применяет миграции, потому что `TIERL_MIGRATE_ON_START=true`.
4. HTTP-сервер слушает `:8080`; на хосте порт задаёт `TIERL_PORT`.

Порты `app` и `postgres` в compose привязаны к `127.0.0.1`, поэтому по
умолчанию они недоступны из сети. Для доступа с другого компьютера
используйте SSH-туннель (`ssh -L 8080:127.0.0.1:8080 user@server`) или
reverse proxy с TLS из раздела [Production-деплой](#production-деплой).

Проверка:

```bash
curl -fsS http://localhost:8080/healthz   # ok
curl -fsS http://localhost:8080/readyz    # ok — PostgreSQL доступен
docker compose ps                         # postgres healthy, app running
docker compose logs -f app                # JSON-логи, "http server started"
```

Откройте `http://localhost:8080`. API доступен по `/api/v1`, например
`http://localhost:8080/api/v1/tierlists/<slug>`.

Остановка:

```bash
docker compose down       # данные в volume pgdata сохраняются
docker compose down -v    # удалить также volume с базой (необратимо)
```

### Текущее состояние

Контракт API опережает реализацию: в текущей сборке работают health/ready,
статика и маршруты `/api/v1/tierlists`; auth, submissions, results и catalog
ещё находятся в разработке. Для локальной разработки фронтенда используются
моки. Не считайте `api/openapi.yaml` описанием уже полностью доступного API.

## Переменные окружения

Приложение читает переменные `TIERL_*` при старте. Полный шаблон с
комментариями — [`.env.example`](../.env.example). Значения по умолчанию
приведены для пустой переменной, если не указано иное.

### Приложение

| Переменная | По умолчанию | Обязательна | Назначение |
|---|---|---|---|
| `TIERL_HTTP_ADDR` | `:8080` | нет | Адрес и порт HTTP-сервера внутри контейнера/процесса. |
| `TIERL_DATABASE_URL` | пусто | нет | PostgreSQL URL. Если пусто, JSON API отключён, доступны только статика и пробы. |
| `TIERL_MIGRATE_ON_START` | `false` | нет | Применять миграции при старте. В compose по умолчанию `true`. |
| `TIERL_LOG_LEVEL` | `info` | нет | `debug`, `info`, `warn` или `error`; логи JSON в stdout. |
| `TIERL_SHUTDOWN_TIMEOUT` | `10s` | нет | Таймаут graceful shutdown по SIGTERM/SIGINT. |
| `TIERL_IP_HASH_SALT` | пусто | да, если задан `TIERL_DATABASE_URL` | Соль для хэша IP в rate limiting. Секрет, менять только осознанно. |
| `TIERL_ARGON2_MEMORY_KIB` | `65536` | нет | Память argon2id в KiB на одно хэширование. |
| `TIERL_ARGON2_ITERATIONS` | `3` | нет | Число проходов argon2id. |
| `TIERL_ARGON2_PARALLELISM` | `2` | нет | Число потоков argon2id. |
| `TIERL_RATELIMIT_CREATE_TIERLIST_BURST` | `10` | нет | Сколько созданий тир-листа разрешено подряд с одного IP. |
| `TIERL_RATELIMIT_CREATE_TIERLIST_INTERVAL` | `30s` | нет | Время пополнения одного токена rate limiter. |
| `TIERL_STORAGE_TYPE` | `fs` | нет | `fs` или `s3` (см. раздел о медиа). |
| `TIERL_STORAGE_FS_PATH` | `./data/media` | нет | Каталог для `fs`-хранилища. |
| `TIERL_STORAGE_PUBLIC_URL` | `/media` | нет | Базовый URL, по которому отдаются картинки. |
| `TIERL_S3_ENDPOINT` | пусто | при `s3` | S3 endpoint в формате `host:port`, без схемы. |
| `TIERL_S3_BUCKET` | пусто | при `s3` | Имя bucket. |
| `TIERL_S3_ACCESS_KEY` | пусто | при `s3` | Access key. Секрет. |
| `TIERL_S3_SECRET_KEY` | пусто | при `s3` | Secret key. Секрет. |
| `TIERL_S3_REGION` | пусто | нет | Регион S3. |
| `TIERL_S3_USE_SSL` | `false` | нет | Использовать HTTPS для S3. |

Если `TIERL_DATABASE_URL` пуст, сервис поднимает фронтенд и health-пробы, но
`/api/v1` отключён. `/readyz` в этом режиме не проверяет базу и отвечает `ok`.

### Только для Docker Compose

Эти переменные не читает Go-приложение; их использует `docker-compose.yml`.

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `TIERL_PORT` | `8080` | Порт хоста для UI/API. |
| `POSTGRES_PORT` | `5432` | Порт хоста для PostgreSQL. |
| `POSTGRES_USER` | `tierl` | Пользователь БД. |
| `POSTGRES_PASSWORD` | `tierl` | Пароль БД. Обязательно замените вне локальной среды. |
| `POSTGRES_DB` | `tierl` | Имя БД. |
| `MINIO_ROOT_USER` | `tierl` | Логин MinIO из профиля `tools`. |
| `MINIO_ROOT_PASSWORD` | `tierl-secret` | Пароль MinIO из профиля `tools`. |

Compose собирает `TIERL_DATABASE_URL` для контейнера из `POSTGRES_*`.
Поэтому при смене пароля меняйте `POSTGRES_PASSWORD`, а не URL. Значение
`TIERL_DATABASE_URL` в `.env.example` нужно для запуска приложения на хосте
(`make run`). Если в пароле есть символы `@`, `:`, `/`, URL-кодируйте их или
используйте простой случайный пароль без спецсимволов.

### Секреты

- `.env` уже в `.gitignore`; не коммитьте его и не печатайте значения в логи.
- `TIERL_IP_HASH_SALT`: `openssl rand -hex 32`. Соль должна быть одинаковой
  между перезапусками, иначе ключи rate limiting начнут считаться заново.
- `POSTGRES_PASSWORD`: длинный случайный пароль; для managed PostgreSQL
  используйте выдаваемые провайдером учётные данные.
- `TIERL_S3_ACCESS_KEY` / `TIERL_S3_SECRET_KEY`: права только на нужный bucket.
- Ограничьте доступ к `.env` (`chmod 600 .env`) и к каталогу с резервными
  копиями.

## Миграции

SQL-файлы лежат в [`../migrations/`](../migrations/) и встроены в бинарник.
Имена — timestamp UTC плюс `_up.sql` / `_down.sql`. Уже применённые миграции
не редактируются: любое изменение схемы — новая пара файлов.

### Автоматически при старте

В compose `TIERL_MIGRATE_ON_START=true`. При каждом запуске `app` применяет
только недостающие миграции; если схема актуальна, это no-op. Если миграция
падает, приложение завершается с ошибкой и не начинает обслуживать запросы.

Подходит для одного инстанса и локального запуска. Для нескольких реплик
одновременный старт с миграциями может конфликтовать, поэтому лучше вынести
миграции в отдельный шаг деплоя.

### Вручную

Из корня репозитория на хосте (нужен Go 1.25, база должна быть доступна):

```bash
# База из compose на localhost:5432
make migrate-up

# Другая база
make migrate-up DATABASE_URL='postgres://user:pass@db-host:5432/tierl?sslmode=require'

# Или напрямую, без make
TIERL_DATABASE_URL='postgres://user:pass@db-host:5432/tierl?sslmode=require' \
  go run ./cmd/migrate up

# Для release-шага можно собрать отдельный бинарник (миграции уже внутри)
make build-migrate
TIERL_DATABASE_URL='postgres://user:pass@db-host:5432/tierl?sslmode=require' \
  ./bin/migrate up
```

`make migrate-down` / `go run ./cmd/migrate down` / `./bin/migrate down`
откатывают **все** применённые миграции. Это разрушительная операция,
предназначенная для разработки; в production используйте её только после
резервной копии и понимания потери данных.

Перед обновлением production:

1. сделайте резервную копию БД;
2. примените миграции отдельным шагом;
3. запустите новую версию приложения;
4. проверьте `/healthz` и `/readyz`.

Если в новой версии нужна обратная совместимость со старой схемой, сначала
выпускайте расширяющие миграции, затем код, и только потом удаляющие
миграции в следующем релизе.

## Healthcheck и проверка готовности

В приложении две HTTP-пробы:

| Путь | Смысл | Ответ |
|---|---|---|
| `GET /healthz` | liveness: процесс жив и обслуживает HTTP. Не зависит от БД. | `200 ok` |
| `GET /readyz` | readiness: PostgreSQL отвечает на ping с таймаутом 2 секунды. Без настроенной БД проверка пропускается. | `200 ok` или `503 not ready` |

Проверка с хоста:

```bash
curl -i http://localhost:8080/healthz
curl -i http://localhost:8080/readyz
```

В Docker Compose у `app` нет встроенного `HEALTHCHECK`: итоговый образ
distroless не содержит shell и curl. Используйте `/healthz` и `/readyz`
извне — вручную, через reverse proxy, systemd или orchestrator. У `postgres`
healthcheck есть, и `app` не стартует, пока база не готова.

Пример для Kubernetes:

```yaml
livenessProbe:
  httpGet: { path: /healthz, port: 8080 }
  initialDelaySeconds: 5
  periodSeconds: 10
readinessProbe:
  httpGet: { path: /readyz, port: 8080 }
  initialDelaySeconds: 5
  periodSeconds: 5
```

`/readyz` возвращает `503`, если база недоступна; это сигнал убрать инстанс из
балансировки, но не перезапускать процесс. `/healthz` остаётся `200`, пока
процесс жив.

## Production-деплой

Минимальная схема: reverse proxy с TLS → контейнер `app` → PostgreSQL.
PostgreSQL не должен быть доступен из интернета.

### TLS и reverse proxy

Пример для Caddy (автоматический HTTPS):

```caddyfile
tierl.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Для nginx проксируйте на `http://127.0.0.1:8080` и передавайте заголовки
`Host`, `X-Forwarded-For`, `X-Forwarded-Proto`. Не публикуйте порт приложения
и порт PostgreSQL в публичную сеть: привязывайте их к loopback или внутренней
сети Docker.

### Рекомендуемая конфигурация

- образ собирайте из зафиксированного коммита/тега, а не из плавающего
  `latest`; храните предыдущий тег для отката;
- `TIERL_MIGRATE_ON_START=false`, миграции — отдельным шагом деплоя;
- `POSTGRES_PASSWORD`, `TIERL_IP_HASH_SALT` и ключи S3 — из секрет-хранилища
  или `.env` с правами `600`, не из репозитория;
- `TIERL_DATABASE_URL` с `sslmode=require` (или `verify-full` с CA), если база
  не в локальной сети;
- TLS завершается на reverse proxy; наружу открыты только 80/443;
- включите `restart: unless-stopped` (или аналог в вашем orchestrator) для
  `app` и `postgres`;
- ограничьте ресурсы контейнера с учётом argon2id: при 64 MiB на хэш и
  параллельных запросах пик памяти растёт;
- логи собирайте из stdout (JSON); персональные данные и секреты в логи не
  пишутся;
- настройте резервное копирование и мониторинг `/readyz`/`/healthz`.

Rate limiting создания тир-листов хранится в памяти процесса. При нескольких
репликах лимит действует отдельно на каждую; для строгого общего лимита нужен
внешний Redis/БД (в текущей версии не реализовано).

### Обновление и откат

1. Сделайте `pg_dump` (см. ниже).
2. Соберите/скачайте новый образ.
3. Примените миграции.
4. Перезапустите `app`, проверьте `/readyz`.
5. При проблеме верните предыдущий образ. Откат схемы автоматически не
   выполняется: если новая миграция несовместима со старым кодом, может
   понадобиться восстановление из резервной копии.

## Данные и резервные копии

PostgreSQL хранится в named volume `pgdata`. Удаление `docker compose down -v`
уничтожает базу.

Резервная копия (для значений по умолчанию; при своих `POSTGRES_*` замените
пользователя и имя БД):

```bash
docker compose exec -T postgres pg_dump -U tierl -d tierl --format=custom > tierl.dump
```

Восстановление:

```bash
docker compose exec -T postgres pg_restore -U tierl -d tierl --clean --if-exists < tierl.dump
```

Проверяйте копии на тестовой базе и храните их вне сервера.

### Медиа и S3

В текущей сборке загрузка картинок через HTTP ещё не подключена: конфиг
`TIERL_STORAGE_*` / `TIERL_S3_*` читается и валидируется, но эндпоинтов
медиа пока нет. Когда медиа появится:

- для `fs` смонтируйте отдельный volume в `TIERL_STORAGE_FS_PATH` и включите
  его в резервное копирование;
- для `s3` используйте отдельный bucket с минимальными правами и версией
  объектов; ключи не храните в образе.

## Необязательные сервисы

Профиль `tools` не влияет на обычный запуск:

```bash
docker compose --profile tools up -d
```

- Mailpit — SMTP `localhost:1025`, UI <http://localhost:8025>. Предназначен
  только для разработки; письма никуда не отправляются.
- MinIO — S3 API `localhost:9000`, консоль <http://localhost:9001>. Учётные
  данные по умолчанию `tierl` / `tierl-secret`, меняйте их вне локальной среды
  через `MINIO_ROOT_USER` / `MINIO_ROOT_PASSWORD`.

В production почта и S3 должны быть внешними сервисами с TLS и резервным
копированием.

## Диагностика

| Симптом | Что проверить |
|---|---|
| `docker compose up` падает на сборке | Достаточно ли места/памяти; доступен ли Docker Hub; логи `docker compose build`. |
| Порт занят | Измените `TIERL_PORT` / `POSTGRES_PORT` в `.env` или освободите порт. |
| `app` завершается с `migrate:` | Ошибка SQL или недоступна БД; смотрите `docker compose logs app` и права пользователя БД. |
| `/readyz` отдаёт `503` | PostgreSQL не запущен/недоступен, неверный `POSTGRES_PASSWORD` или URL; проверьте `docker compose ps` и логи. |
| `/healthz` отдаёт `ok`, но UI пустой | Фронтенд не собран в образе; пересоберите `docker compose build --no-cache app`. |
| `frontend is not built` при `make run` | Выполните `make web-build` и перезапустите процесс. |
| `TIERL_IP_HASH_SALT: required` | Задайте соль при непустом `TIERL_DATABASE_URL`. |
| Изменения в `.env` не применились | Перезапустите: `docker compose up -d --force-recreate`; compose не перечитывает env у работающего контейнера. |
| Нет писем / нет S3 | Это необязательные сервисы; для локальной проверки поднимите профиль `tools`. |

## Ссылки

- [`../README.md`](../README.md) — быстрый старт и разработка;
- [`SPEC.md`](SPEC.md) — требования MVP;
- [`adr/`](adr/) — архитектура, auth, анонимность;
- [`../SECURITY.md`](../SECURITY.md) — сообщение об уязвимостях.
