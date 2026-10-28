# Proposal

## Why

TIERL-10 (PR 59) добавил документацию и артефакты для запуска tierl на своём
сервере: `README.md`, `docs/SELF-HOST.md`, `docker-compose.yml`,
`.env.example`, `Makefile` и `Dockerfile`. Код и документация уже реализуют
быстрый старт одной командой, описание переменных окружения, миграции,
`/healthz`/`/readyz` и production-деплой. Происхождение работы подтверждают
`docs/SPEC.md` (раздел 9 — запуск одной командой и health-проба; раздел 8 —
один процесс, Postgres и встроенная статика) и
`docs/adr/ADR-004-architecture.md` (один контейнер плюс Postgres, multi-stage
образ).

При этом в OpenSpec нет capability для self-host и деплоя: требование осталось
только в `docs/SPEC.md` и ADR. Поэтому у TIERL-10 нет живой цепочки
«цель → спека», и соответствие артефактов требованию нельзя проверить через
OpenSpec. Изменение восстанавливает эту цепочку, не меняя поведение кода.

## What Changes

- Добавляется capability `self-host` с требованиями к запуску одной командой,
  переменным окружения и секретам, миграциям, health/ready-пробам и
  production-деплою.
- Фиксируется, что источник работы — нефункциональное требование MVP
  (`docs/SPEC.md`, раздел 9) и архитектурное решение `ADR-004`.
- Поведение приложения, HTTP-маршрутов, схемы БД и applied-миграций не
  меняется.

## Capabilities

### New Capabilities

- `self-host`: воспроизводимый запуск и эксплуатация tierl — `docker compose`,
  документированные env и секреты, автоматические и ручные миграции,
  health/ready-пробы, production-деплой с TLS, backup и откатом.

### Modified Capabilities

Нет: раздел 9 `docs/SPEC.md` задаёт нефункциональные требования ко всему
сервису; изменение вводит для них отдельную capability и не переопределяет
требования `accounts`, `tierlists`, `submissions`, `results`, `catalog`,
`reports`, `sharing`, `author-profile` или `item-images`.

## Новое имя capability

`self-host` не совпадает ни с одним якорем цели (`accounts`, `tierlists`,
`submissions`, `results`, `catalog`, `reports`, `sharing`, `author-profile`,
`item-images`), потому что эти якоря описывают продуктовые возможности, а
TIERL-10 — способ поставки и эксплуатации сервиса. Название взято из
`docs/SELF-HOST.md` и задачи TIERL-10.

## Impact

- Спека: новый `openspec/specs/self-host/spec.md` после архивации.
- Документация: `README.md`, `docs/SELF-HOST.md` — только проверка
  соответствия, без правок содержания.
- Конфигурация: `docker-compose.yml`, `.env.example`, `Makefile`, `Dockerfile`
  — только проверка соответствия, без изменения поведения.
- Код: `internal/web/handler.go`, `internal/app/setup.go`,
  `internal/storage/postgres/migrate.go`, `cmd/migrate/main.go`,
  `internal/platform/config/config.go` — только проверка соответствия.
- Миграции: applied-миграции не затрагиваются.
- Аудит: вердикт `ok` в `docs/audits/TIERL-10.md`; избыточного кода, который
  следовало бы удалить, в границах задачи не найдено.
