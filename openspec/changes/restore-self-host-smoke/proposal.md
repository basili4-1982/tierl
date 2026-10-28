# Proposal

## Why

TIERL-31 (PR 96) закрыл пункт 5.1 аудита `TIERL-10`: в CI появился smoke-тест
`docker compose up` — сборка образа, ожидание healthy PostgreSQL, проверка
`GET /healthz` и `GET /readyz`. Работа уже слита в
`.github/workflows/ci.yml` (джоба `docker`) и воспроизводится через
`Makefile:smoke`.

Требование происходит из `docs/SPEC.md`, раздел 9: запуск одной командой
`docker compose up` с проверкой `GET /healthz`, а также проверки в CI. Живая
capability `self-host` описывает запуск compose, порядок старта и
health/readiness-пробы, но не требует их smoke-проверки в CI. Пункт 5.1
архивированного change `restore-self-host` был единственным местом, где это
требование было зафиксировано, поэтому у TIERL-31 нет живой цепочки
«цель → спека». Изменение восстанавливает требование, не меняя поведение кода.

## What Changes

- В capability `self-host` добавляется требование к smoke-проверке self-host в
  CI: CI собирает образ, поднимает `docker compose`, дожидается healthy
  PostgreSQL и проверяет `GET /healthz` и `GET /readyz`.
- Фиксируется происхождение работы: `docs/SPEC.md` §9, живая capability
  `self-host` и пункт 5.1 архивированного change `restore-self-host`.
- Поведение приложения, HTTP-маршрутов, схемы БД и applied-миграций не
  меняется; код и тесты не редактируются.

## Capabilities

### New Capabilities

Нет.

### Modified Capabilities

- `self-host`: добавлено требование к CI smoke-проверке запуска compose и проб
  `/healthz`/`/readyz`. Остальные требования capability не меняются.

Используется существующий якорь цели `self-host`: smoke-тест проверяет запуск
одной командой, готовность PostgreSQL и health/readiness-пробы, то есть
поведение, уже относящееся к self-host. Новое имя capability не требуется.

## Impact

- Спека: `openspec/specs/self-host/spec.md` получит новое требование после
  архивации изменения.
- Код и тесты: только сверка, без изменений —
  `.github/workflows/ci.yml`, `Makefile`, `docker-compose.yml`,
  `internal/web/handler.go`.
- Контракт API, схема БД и миграции: без изменений.
- Аудит: вердикт `ok` в `docs/audits/TIERL-31.md`; gap и excess в коде не
  найдены.
