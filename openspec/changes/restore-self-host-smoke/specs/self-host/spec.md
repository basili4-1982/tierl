# Spec Delta

## Purpose

Восстановить требование к smoke-проверке self-host в CI: образ собирается,
`docker compose` поднимает приложение и PostgreSQL, CI дожидается готовности
базы и проверяет liveness- и readiness-пробы.

## ADDED Requirements

### Requirement: Smoke-проверка self-host в CI

CI SHALL проверять запуск self-host через `docker compose`: собирать образ
`app`, поднимать стек, дожидаться healthy PostgreSQL и проверять `GET /healthz`
и `GET /readyz`. Проверка MUST завершаться ошибкой, если база не стала healthy
или любой из маршрутов не ответил успешно за отведённое время.

#### Scenario: Compose собирается и поднимается

- **WHEN** CI выполняет smoke-проверку self-host
- **THEN** `docker compose config` проходит без ошибок
- **AND** образ `app` собирается из `Dockerfile`
- **AND** `docker compose` запускает `app` и `postgres`

#### Scenario: CI ждёт готовности PostgreSQL

- **WHEN** стек запущен, а PostgreSQL ещё не прошёл healthcheck `pg_isready`
- **THEN** CI не проверяет HTTP-пробы до перехода контейнера в состояние
  `healthy`
- **AND** по истечении отведённого времени проверка завершается ошибкой

#### Scenario: Пробы доступны после запуска

- **WHEN** PostgreSQL healthy, а приложение обслуживает HTTP
- **THEN** `GET /healthz` возвращает `200` с телом `ok`
- **AND** `GET /readyz` возвращает `200` с телом `ok`

#### Scenario: Стек очищается после проверки

- **WHEN** smoke-проверка завершилась успешно или с ошибкой
- **THEN** CI останавливает контейнеры
- **AND** удаляет созданные volume, не оставляя состояние между запусками
