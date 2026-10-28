# Proposal

## Why

TIERL-9 (PR 55) заявляет E2E-сценарии MVP: регистрация → подтверждение email →
создание → шеринг → ответ гостя → результат, а также unlisted вне каталога и
повторную отправку по контракту. Файл `internal/app/mvp_e2e_test.go` есть, но
сквозной путь через публичный API не реализован: подтверждение email делается
прямым `UPDATE users`, ответы гостей вставляются через `Store.Save`, результат
считается вызовом `similarity.ThinkLikeYou`, а повторная отправка проверяется
на уровне хранилища, а не контрактом `POST`/`PUT`.

Требование зафиксировано в `docs/SPEC.md` (разделы 1, 4 и 11) и
`api/openapi.yaml` (маршруты `/auth/verify-email`, `/tierlists/{slug}`,
`/tierlists/{slug}/submissions`, `/tierlists/{slug}/submissions/mine`,
`/results/{id}`), но канонической capability в OpenSpec нет. Из-за этого у
TIERL-9 нет живой цепочки «цель → спека → тест». Изменение восстанавливает
требование и выносит исправление теста в задачи; поведение продакшен-кода не
меняется.

## What Changes

- Добавляется capability `mvp-e2e` с требованием к сквозному тесту MVP через
  публичные HTTP-маршруты, без ручных изменений бизнес-состояния в БД.
- Фиксируются E2E-проверки unlisted вне каталога и повторной отправки по
  контракту (`409 already_submitted` на повторный `POST`, `200` и тот же
  `result_id` на `PUT /submissions/mine`).
- В `tasks.md` выносятся задачи на замену прямых вызовов БД/хранилища
  публичными API-вызовами в `internal/app/mvp_e2e_test.go`.
- Фиксируется, что тест должен быть зелёным на чистой схеме после миграций.
- **BREAKING**: нет. Продакшен-код, контракт, applied-миграции и существующие
  тесты в этом change не меняются.

## Capabilities

### New Capabilities

- `mvp-e2e`: сквозная приёмочная проверка пути автора и гостя через публичный
  API, включая unlisted вне каталога и повторную отправку по контракту.

Новое имя `mvp-e2e` обосновано тем, что критерий готовности MVP
(`docs/SPEC.md`, раздел 11) пересекает сразу `accounts`, `tierlists`,
`sharing`, `submissions`, `results` и `catalog`: ни один из существующих
якорей не покрывает весь путь. Дублировать одно требование в шести capability
хуже, чем завести один сквозной якорь приёмки.

### Modified Capabilities

Нет: требования `accounts`, `tierlists`, `sharing`, `submissions`, `results` и
`catalog` не переписываются; этот change проверяет их совместную работу.

## Impact

- Спека: новый `openspec/specs/mvp-e2e/spec.md` после архивации.
- Тест (задачи на реализацию, не в этом change): `internal/app/mvp_e2e_test.go`.
- Контекст: `internal/app/integration_test.go` (чистая БД и миграции),
  `internal/web/api` (существующие маршруты ответов и результатов),
  `docs/SPEC.md`, `api/openapi.yaml`.
- Аудит: вердикт `gap` в `docs/audits/TIERL-9.md`; избыточного кода не найдено,
  удаление не требуется.
- Связанные follow-up: `restore-email-verification` (5.1) и `restore-catalog`
  (2.4) закрывают части этого сквозного требования.
