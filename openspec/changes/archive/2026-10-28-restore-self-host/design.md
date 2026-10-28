# Design

## Context

Self-host уже работает и описан. `docker-compose.yml` поднимает `app` и
`postgres`, ждёт healthcheck PostgreSQL и передаёт `TIERL_MIGRATE_ON_START=true`.
`Dockerfile` собирает SPA стадией Node.js, встраивает её в Go-бинарник и кладёт
его в distroless-образ. `internal/web/handler.go:NewHandler` регистрирует
`GET /healthz` и `GET /readyz`, `internal/app/setup.go:setup` вызывает
`postgres.MigrateOnStart`, а `cmd/migrate/main.go:run` даёт явные команды
`up`/`down`. Переменные окружения читает
`internal/platform/config/config.go:Load`.

Документация лежит в `README.md` (быстрый старт, конфигурация, миграции,
команды) и `docs/SELF-HOST.md` (поставка, требования, env, секреты, миграции,
health, production-деплой, backup, диагностика). `.env.example` содержит полный
шаблон, `.env` исключён в `.gitignore`.

OpenSpec этого поведения не описывает: `docs/SPEC.md` (разделы 8 и 9) и
`ADR-004` есть, канонической capability нет. Аудит восстанавливает требования,
а не переписывает реализацию.

## Goals / Non-Goals

**Goals:**

- зафиксировать запуск одной командой, env и секреты, миграции,
  health/ready-пробы и production-деплой как требования OpenSpec;
- связать требования с существующими `README.md`, `docs/SELF-HOST.md`,
  `docker-compose.yml`, `.env.example`, `Makefile`, `Dockerfile` и кодом проб и
  миграций;
- подтвердить соответствие кода и документации чтением реализации и
  существующими тестами.

**Non-Goals:**

- менять `docker-compose.yml`, `Dockerfile`, `Makefile`, `.env.example` или
  переменные окружения;
- менять `/healthz`, `/readyz`, механизм миграций или applied-миграции;
- добавлять новые сервисы в поставку или описывать деплой в конкретном
  облаке.

## Decisions

1. **Capability `self-host`, новое имя.** Якоря `accounts`, `tierlists`,
   `submissions`, `results`, `catalog`, `reports`, `sharing`,
   `author-profile`, `item-images` описывают продуктовые возможности;
   развёртывание и эксплуатация — отдельная нефункциональная цель из
   `docs/SPEC.md`, раздел 9. Имя `self-host` взято из `docs/SELF-HOST.md`.
2. **Спека фиксирует наблюдаемое поведение, а не структуру файлов.**
   Требования ссылаются на `docker compose`, переменные `TIERL_*`,
   `GET /healthz`, `GET /readyz`, `make migrate-up` и `migrate up|down`; имена
   пакетов и точные строки документации остаются деталями реализации.
3. **Секреты описаны как требование, а не как значения.** В репозитории
   допустим только `.env.example` с явно dev-значениями; реальный `.env`,
   пароль PostgreSQL, соль `TIERL_IP_HASH_SALT` и ключи S3 MUST NOT попадать в
   git и логи. Это соответствует разделу 6 `docs/SPEC.md` и
   `docs/SELF-HOST.md` (раздел «Секреты»).
4. **Миграции описаны в двух режимах.** Автоматически при старте через
   `TIERL_MIGRATE_ON_START` и явно через `cmd/migrate`/`make migrate-up`;
   applied-миграции не редактируются, имена — timestamp.
5. **Код не удаляется.** Происхождение подтверждено `docs/SPEC.md` (разделы 8,
   9) и `ADR-004`; избыточных артефактов в границах TIERL-10 нет.

## Risks / Trade-offs

- [Нет smoke-теста `docker compose up` в CI] → остаточный риск регрессии
  compose-файла и образа; follow-up на тест вынесен в `tasks.md`, поведение в
  этом изменении не меняется.
- [Автоматические миграции при нескольких репликах могут конфликтовать] →
  документация уже рекомендует `TIERL_MIGRATE_ON_START=false` и отдельный шаг
  для production; требование фиксирует оба режима.
- [Applied-миграции нельзя менять] → изменение только документационное,
  существующие миграции остаются без правок.

## Migration Plan

Изменение не требует миграции данных. После архивации требования попадут в
`openspec/specs/self-host/spec.md`; откат — удаление нового change до
архивации, поведение сервиса не меняется.
