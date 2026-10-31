# Proposal

## Why

Аудит TIERL-60 подтвердил, что живая capability `self-host` уже существует
(`openspec/specs/self-host/spec.md`, архивный change
`2026-10-28-restore-self-host`) и все шесть проверяемых требований
реализованы: запуск одной командой, ожидание готовности PostgreSQL, описание
переменных окружения и секретов, автоматические и ручные миграции,
`/healthz`/`/readyz` и production-деплой. Вердикты `ok` с путями и символами
записаны в `docs/audits/self-host.md`.

При этом аудит нашёл пробел спеки, а не реализации. В репозитории есть
smoke-проверка self-host: `make smoke` (`Makefile:63`) и шаг CI
`smoke test docker compose` (`.github/workflows/ci.yml:71`). Она собирает
образ, поднимает compose, ждёт healthy PostgreSQL, проверяет `200` от
`/healthz` и `/readyz` и всегда удаляет контейнеры и volume. Эта работа
появилась из follow-up TIERL-31 архивного change, но в живой спеке
`self-host` нормативного требования к ней нет, поэтому сверка работы не видит
её. Документация `README.md` и `docs/SELF-HOST.md` также не упоминает
`make smoke`.

## What Changes

- В capability `self-host` добавляется требование «Smoke-проверка self-host»:
  `make smoke` и CI поднимают compose из чистого состояния, дожидаются
  здорового PostgreSQL и `200` от `/healthz` и `/readyz`, после чего всегда
  удаляют контейнеры и volume.
- Существующие требования к запуску одной командой, порядку старта,
  переменным окружения и секретам, миграциям, health/ready-пробам и
  production-деплою не переписываются.
- В `tasks.md` добавляется follow-up на описание `make smoke` в `README.md` и
  `docs/SELF-HOST.md`.
- Поведение кода, тестов, HTTP-маршрутов, контракта и applied-миграций в этом
  change не меняется; правки кода не требуются.

## Capabilities

### New Capabilities

Нет. Живая capability `self-host` уже описывает запуск, конфигурацию,
миграции, пробы и production-деплой; новая способность не вводится.

### Modified Capabilities

- `self-host`: добавляется требование к smoke-проверке compose, чтобы уже
  реализованная проверка была видна сверке и имела проверяемый контракт.
  Якорь `self-host` сохраняется без переименования.

## Impact

- Спека: `openspec/specs/self-host/spec.md` после архивации получит требование
  «Smoke-проверка self-host».
- Документация (follow-up, без правок в границах аудита):
  `README.md:150` (таблица команд) и `docs/SELF-HOST.md` (раздел запуска или
  эксплуатации).
- Код и конфигурация (только проверка соответствия, без изменения поведения):
  `docker-compose.yml`, `Dockerfile`, `.env.example`, `.gitignore`,
  `.dockerignore`, `Makefile:smoke`, `.github/workflows/ci.yml`,
  `internal/platform/config/config.go:Load`,
  `internal/storage/postgres/migrate.go:MigrateOnStart`,
  `internal/web/handler.go:readyz`.
- Аудит: `docs/audits/self-host.md` фиксирует `ok` по R1–R6, gap спеки
  capability и отсутствие excess.
