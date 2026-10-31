# Design

## Context

Аудит TIERL-60 сверил живую capability `self-host` с кодом и документацией.
Compose собирает `app` из `Dockerfile`, поднимает `postgres` с healthcheck
`pg_isready` и ждёт его готовности (`docker-compose.yml:app`,
`docker-compose.yml:depends_on`). Конфигурация читается из `TIERL_*`, а соль
хэширования IP обязательна при заданной базе
(`internal/platform/config/config.go:Load`). Миграции встроены в бинарник и
применяются при старте или через `cmd/migrate`/make
(`internal/storage/postgres/migrate.go:MigrateOnStart`). Пробы
`/healthz` и `/readyz` реализованы в `internal/web/handler.go`. Production-
деплой, backup и откат описаны в `docs/SELF-HOST.md`.

Живая спека не описывает уже существующую smoke-проверку: цель
`make smoke` (`Makefile:63`) и шаг CI `smoke test docker compose`
(`.github/workflows/ci.yml:71`) собирают и поднимают compose, проверяют
healthy PostgreSQL и `200` от `/healthz` и `/readyz`. Работа появилась из
follow-up TIERL-31 архивного change `2026-10-28-restore-self-host`, но в
нормативном описании capability отсутствует.

## Goals / Non-Goals

**Goals:**

- зафиксировать в `self-host` проверяемый контракт smoke-проверки compose,
  чтобы уже реализованная работа была видна сверке;
- сохранить требования R1–R6 живой спеки без переписывания;
- вынести описание `make smoke` в документацию отдельной задачей;
- не менять поведение кода в границах аудита.

**Non-Goals:**

- реализовывать новую smoke-проверку или менять `Makefile`/CI;
- менять compose, `Dockerfile`, `.env.example`, миграции, обработчики проб,
  тесты или OpenAPI;
- переписывать существующие требования и переименовывать якорь `self-host`;
- дублировать требования R1–R6 в дельте.

## Decisions

1. **Capability `self-host`, а не новая.** Smoke-проверка проверяет именно
   запуск и эксплуатацию сервиса, то есть существующий якорь `self-host`.
   Новое имя не вводится.
2. **Добавляется одно новое требование, старые не переписываются.**
   «Smoke-проверка self-host» уточняет проверку R1/R2/R5, не заменяя их.
3. **Проверка должна быть воспроизводимой с чистого состояния.** Требование
   фиксирует сборку образа, поднятие compose, ожидание healthy PostgreSQL и
   `200` от обеих проб, а не конкретные номера строк скриптов.
4. **Очистка обязательна.** Smoke-проверка MUST удалять контейнеры и volume
   при любом исходе, чтобы не оставлять состояние и данные между прогонами;
   это соответствует `trap cleanup EXIT` в `Makefile:63` и CI.
5. **Реализация остаётся задачей на документацию, а не на код.** В границах
   TIERL-60 запрещено менять код и документацию вне `docs/audits/` и
   `openspec/changes/`, поэтому упоминание `make smoke` в `README.md` и
   `docs/SELF-HOST.md` вынесено в `tasks.md`.

## Risks / Trade-offs

- [Smoke-проверка требует Docker и сети] → это отдельная make-цель и CI-шаг;
  обычный `docker compose up` и юнит-тесты от неё не зависят.
- [Smoke-проверка может конфликтовать с занятыми портами] → CI и `make smoke`
  используют `TIERL_PORT`/`POSTGRES_PORT`; требование фиксирует проверку
  контракта, а не конкретные порты.
- [Документация отстаёт от кода] → follow-up задача в `tasks.md`; сам аудит
  фиксирует расхождение, не меняя код.

## Migration Plan

Изменение документационное и не требует миграций. После архивации требование
попадёт в `openspec/specs/self-host/spec.md`; откат — удаление change до
архивации. Поведение сервиса не меняется.
