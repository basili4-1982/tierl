# Аудит TIERL-31 — Smoke-тест docker compose в CI

## Контекст

Задача: TIERL-31 (PR 96) — доработка из аудита `TIERL-10`, change
`restore-self-host`, пункт 5.1. Заявлено: добавить в CI smoke-тест
`docker compose up` — собрать образ, дождаться healthy PostgreSQL, проверить
`GET /healthz` и `GET /readyz`. Код и тесты в границах аудита не меняются.

Происхождение требования восстанавливается: `docs/SPEC.md`, раздел 9
(линтеры и проверки проходят в CI; запуск одной командой `docker compose up`,
проверка — `GET /healthz` отвечает 200), а также живая capability `self-host`
(запуск одной командой, порядок старта и готовность базы, health/readiness
пробы). Пункт 5.1 архивированного change `restore-self-host` прямо породил
TIERL-31, но в живой спеке `self-host` требования к CI smoke-проверке нет.
Поэтому оформлен change `restore-self-host-smoke`, который добавляет это
требование в существующую capability `self-host`, не меняя поведение кода.

## Вердикт

- TIERL-31: ok — .github/workflows/ci.yml:jobs.docker; джоба `docker` проверяет compose, собирает образ, поднимает стек, ждёт healthy PostgreSQL и проверяет `GET /healthz` и `GET /readyz`.

## Доказательства по пунктам описания

| Заявлено | Где реализовано | Что подтверждает |
|---|---|---|
| Smoke-тест `docker compose up` в CI | `.github/workflows/ci.yml:58` (`docker:` job), `.github/workflows/ci.yml:71` (`smoke test docker compose`), `.github/workflows/ci.yml:85` (`docker compose up -d`) | Отдельная джоба CI запускает стек на портах `TIERL_PORT=18080` и `POSTGRES_PORT=15432`, чтобы не конфликтовать с раннером (`.github/workflows/ci.yml:61-64`) |
| Собрать образ | `.github/workflows/ci.yml:67` (`validate compose file`), `.github/workflows/ci.yml:69` (`build image`), `.github/workflows/ci.yml:70` (`docker compose build`) | До smoke-теста CI валидирует `docker compose config --quiet` и собирает образ через `docker compose build` |
| Дождаться healthy PostgreSQL | `.github/workflows/ci.yml:87-99` | CI получает контейнер `postgres`, опрашивает `docker inspect -f '{{.State.Health.Status}}'`, ждёт `healthy` до 60 попыток с интервалом 2 с и падает по таймауту. Healthcheck задан в `docker-compose.yml:61-65` (`pg_isready`), а `app` зависит от `service_healthy` (`docker-compose.yml:46-48`) |
| Проверить `GET /healthz` | `.github/workflows/ci.yml:101-115` (`for endpoint in healthz readyz`), `.github/workflows/ci.yml:103` (`curl -fsS .../healthz`) | CI ждёт успешный `curl` до 90 попыток и затем повторно выводит тело ответа. Маршрут зарегистрирован в `internal/web/handler.go:26` (`GET /healthz`), обработчик — `internal/web/handler.go:50:healthz` |
| Проверить `GET /readyz` | `.github/workflows/ci.yml:101-115`, `.github/workflows/ci.yml:103` (`curl -fsS .../readyz`) | Тот же цикл проверяет readiness; маршрут — `internal/web/handler.go:27` (`GET /readyz`), обработчик — `internal/web/handler.go:56:readyz`, timeout 2 с (`internal/web/handler.go:13`) |
| Очистка после smoke-теста | `.github/workflows/ci.yml:74-82` | `trap cleanup EXIT` при ошибке печатает логи, затем всегда выполняет `docker compose down -v --remove-orphans`, не оставляя контейнеры и volume |
| Локальное воспроизведение | `Makefile:63:smoke` | `make smoke` повторяет сборку, ожидание healthy PostgreSQL и обе проверки `/healthz`/`/readyz`; это тот же сценарий, а не отдельная возможность |
| Тесты обработчиков проб | `internal/web/handler_test.go:11:TestHealthz`, `internal/web/handler_test.go:41:TestReadyz` | Юнит-тесты подтверждают `200 ok` для liveness и readiness, включая `503 not ready` при недоступной зависимости |

## Gap

Gap в коде нет: джоба CI, сборка образа, ожидание healthy PostgreSQL и обе
HTTP-проверки присутствуют, а `/healthz` и `/readyz` реально зарегистрированы
в корневом обработчике.

Gap в живой спеке: требование «smoke-тест `docker compose up` в CI» было
только в пункте 5.1 архивированного change `restore-self-host` и в
`docs/SPEC.md` §9, но не попало в `openspec/specs/self-host/spec.md`. Это
требование восстановлено change'ем `restore-self-host-smoke` (capability
`self-host`), чтобы у TIERL-31 появилась цепочка «цель → спека».

## Excess

Excess нет. `Makefile:63:smoke` дублирует CI-сценарий для локального запуска,
но не добавляет отдельного поведения и следует тому же требованию; удалять
код не требуется.

## Замечания по проверке

- В этом окружении Docker недоступен (`docker: command not found`), поэтому
  `docker compose up` и `make smoke` фактически не запускались. Проверка
  выполнена чтением CI-конфигурации, `docker-compose.yml`, обработчиков
  `/healthz`/`/readyz` и их юнит-тестов. Это ограничение окружения, а не
  дефект реализации.
- `openspec validate --strict --all --no-interactive` проходит после
  добавления change `restore-self-host-smoke`.

## Решения

1. **Gap/excess в коде не найдены.** Заявленный smoke-тест уже реализован в
   `.github/workflows/ci.yml` и воспроизводится через `Makefile:smoke`; задача
   на реализацию или удаление кода не создаётся.
2. **Требование отсутствовало в живой спеке — восстановлено change'ем.**
   Создан `openspec/changes/restore-self-host-smoke` с изменением capability
   `self-host` (якорь цели `self-host`): smoke-проверка в CI собирает образ,
   дожидается healthy PostgreSQL и проверяет `GET /healthz` и `GET /readyz`.
   Новое имя capability не вводится: self-host уже покрывает запуск compose и
   пробы, а smoke-тест проверяет именно их.
3. **Эскалация человеку не требуется:** происхождение подтверждается
   `docs/SPEC.md` §9, живой спекой `self-host` и пунктом 5.1 архивированного
   change `restore-self-host`.

## Границы

Код, тесты, контракт и применённые миграции не изменялись. Изменены
`docs/audits/TIERL-31.md` и файлы
`openspec/changes/restore-self-host-smoke/`.
