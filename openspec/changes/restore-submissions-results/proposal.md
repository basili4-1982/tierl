# Proposal

## Why

TIERL-11 (PR 61) добавил отправку ответа, получение результата и статистику:
`POST /api/v1/tierlists/{slug}/submissions`, `PUT`/`GET
/tierlists/{slug}/submissions/mine`, `GET /api/v1/results/{id}` и
`GET /api/v1/results/{id}/stats`. Код и тесты покрывают минимум 3 элемента,
один ответ на сессию, распределение по тирам, «мало данных» при <20 ответов и
отсутствие `session_id`/`user_id` в публичной выдаче. Происхождение работы
подтверждают `docs/SPEC.md` §4–§5, `docs/adr/ADR-001-similarity-metric.md`,
`docs/adr/ADR-002-anonymous-answers.md` и `api/openapi.yaml`.

При этом в OpenSpec нет capability `submissions` и `results`: требования
остались только в документе, ADR и контракте. Поэтому у TIERL-11 нет живой
цепочки «цель → спека», а расхождение контракта с правилом неполного ответа
негде проверить. Изменение восстанавливает требования и выносит исправление
контракта и недостающий граничный тест в задачи; поведение продакшен-кода не
меняется.

## What Changes

- Добавляется capability `submissions`: ответ на опрос без регистрации,
  минимум 3 размещённых элемента, неполный ответ, один ответ на анонимную
  сессию и замена ответа с сохранением `result_id`.
- Добавляется capability `results`: результат по id, статистика с
  распределением по тирам, порогом `min_answers = 20`, «мало данных» и
  отсутствием идентификаторов участников в публичных агрегатах.
- Фиксируется происхождение работы по `docs/SPEC.md` §4–§5, ADR-001/002 и
  `api/openapi.yaml`.
- В `tasks.md` выносятся задачи на исправление противоречия
  `api/openapi.yaml:403` («ответы по всем элементам») и на граничный тест
  неполного ответа (3 из 4+ элементов).
- **BREAKING**: нет. Поведение HTTP-маршрутов, схема БД и applied-миграции не
  меняются.

## Capabilities

### New Capabilities

- `submissions`: анонимная отправка ответа, минимум 3 размещения, неполный
  ответ, один ответ на сессию и замена с сохранением результата.
- `results`: получение результата по id и анонимная статистика с распределением
  по тирам, порогом «мало данных» и отсутствием идентификаторов.

### Modified Capabilities

Нет: `sharing` описывает страницы `/r/{id}` и OG-превью, `mvp-e2e` — сквозную
проверку пути, а не контракт ответов и статистики.

## Impact

- Спека: новые `openspec/specs/submissions/spec.md` и
  `openspec/specs/results/spec.md` после архивации.
- Код (только проверка соответствия, без изменения поведения):
  `internal/web/api/submissions.go`, `internal/web/api/results.go`,
  `internal/web/api/api.go`, `internal/tierlist/submissions.go`,
  `internal/similarity/similarity.go`, `internal/storage/postgres/answers.go`,
  миграция `20261022100300_create_submissions`.
- Контракт: `api/openapi.yaml`, разделы `/tierlists/{slug}/submissions`,
  `/tierlists/{slug}/submissions/mine`, `/results/{id}` и
  `/results/{id}/stats`; исправление описания неполного ответа вынесено в
  задачи.
- Тесты: `internal/web/api/submissions_test.go`,
  `internal/web/api/submissions_contract_test.go`,
  `internal/tierlist/submissions_test.go`; граничный тест неполного ответа
  вынесен в задачи.
- Аудит: вердикт `gap` в `docs/audits/TIERL-11.md` (контракт и тест неполного
  ответа); избыточного кода не найдено, удаление не требуется.
