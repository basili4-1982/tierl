# Аудит TIERL-11 — Ответы, результат и статистика

## Контекст

Задача: TIERL-11 (PR 61) — эндпоинты отправки ответа, результата и статистики с
границами: неполный ответ, минимум 3 элемента, один ответ на сессию,
распределение по тирам, «мало данных» при <20 ответов и отсутствие
идентификаторов. Критерий — контракт и тесты на границы.

Источники требования: `docs/SPEC.md` §4 «Ответ» (неполный ответ, минимум 3
элемента, один ответ на сессию, анонимность), §5 «Метрика сходства»
(`min_overlap`, `min_answers = 20`, распределение по тирам, похожие участники
без идентификации), §6 (rate limiting на отправку ответов);
`docs/adr/ADR-001-similarity-metric.md`, `docs/adr/ADR-002-anonymous-answers.md`
(один ответ на анонимную сессию, только агрегаты) и `api/openapi.yaml`
(маршруты `/tierlists/{slug}/submissions`, `/submissions/mine`,
`/results/{id}`, `/results/{id}/stats`).

Канонической capability в OpenSpec не было: в `openspec/specs` есть только
`author-profile` и `item-images`, поэтому оформлен change
`restore-submissions-results` с якорями `submissions` и `results`.

## Вердикт

- TIERL-11: gap — api/openapi.yaml:403 требует «ответы по всем элементам», хотя SPEC §4 и `SubmissionInput` (api/openapi.yaml:1040) допускают неполный ответ; в `internal/web/api/submissions_test.go` и `internal/tierlist/submissions_test.go` нет граничного теста частичного ответа (3 из 4+ элементов).

## Что заявлено и что найдено

### Есть в коде

- Маршруты отправки, результата и статистики зарегистрированы:
  `internal/web/api/api.go:173` (`POST /tierlists/{slug}/submissions`),
  `:174` (`PUT /tierlists/{slug}/submissions/mine`), `:175`
  (`GET /tierlists/{slug}/submissions/mine`), `:176` (`GET /results/{id}`),
  `:177` (`GET /results/{id}/stats`).
- Отправка ответа: `internal/web/api/submissions.go:76:createSubmission`
  создаёт анонимную сессию (`anonSession`), вызывает
  `internal/tierlist/submissions.go:77:Submit` и отвечает `201` с
  `submissionDTO`. Замена ответа — `submissions.go:97:replaceSubmission`
  (`replace=true`), чтение своего ответа — `submissions.go:118:getMySubmission`.
- Минимум 3 элемента: `internal/tierlist/submissions.go:16:MinPlacements`
  (`domain.MinItems = 3`), проверка в
  `internal/web/api/submissions.go:64:decodeSubmission` (400
  `bad_request` до выдачи cookie) и в `Submit`
  (`len(in.Placements) < MinPlacements`). Контракт:
  `api/openapi.yaml:1040` (`minItems: 3`). Тесты:
  `internal/web/api/submissions_contract_test.go:20:TestSubmissionRejectsInvalidPlacements`
  (0/1/2 размещения, дубликаты, чужие item/tier) и
  `internal/tierlist/submissions_test.go:31:TestSubmitRequiresThreePlacements`.
- Один ответ на сессию: уникальность `(tierlist_id, session_id)` в
  `migrations/20261022100300_create_submissions.up.sql:10`, upsert
  `internal/storage/postgres/answers.go:49`, а
  `Submit` возвращает `AlreadySubmittedError` с `result_id`. Контракт:
  `api/openapi.yaml:400` (`409 already_submitted`, `details.result_id`).
  Тесты: `internal/web/api/submissions_test.go:76:TestSubmissionEndpoints`
  (повторный POST → 409, PUT → 200 с тем же `id`) и
  `internal/tierlist/submissions_test.go:41:TestSubmitOnePerSessionAndReplace`.
- Распределение по тирам:
  `internal/storage/postgres/answers.go:Distributions` считает счётчики по
  `item_tier_counts`, `internal/tierlist/submissions.go:283:Stats` строит
  `ItemStats`/`TierCount` с процентом от общего числа ответов,
  `internal/web/api/results.go:67:toResultStatsDTO` отдаёт `items[].distribution`.
  Контракт: `api/openapi.yaml:523` и схемы `ItemDistribution`/`TierCount`.
  Тесты: `assertTierDistribution` в
  `internal/web/api/submissions_contract_test.go` и
  `internal/web/api/submissions_test.go:145:TestResultStatsBoundariesThroughAPI`.
- «Мало данных» при <20 ответов:
  `internal/similarity/similarity.go:16:DefaultMinAnswers = 20`,
  `internal/tierlist/submissions.go:20:MinSubmissions`,
  `Stats` выставляет `InsufficientData`, `MinSubmissions`, а
  `LikeYouPercent` остаётся `nil` (`internal/tierlist/submissions.go:331`,
  `:338`, `:369`). Контракт: `api/openapi.yaml:529`
  (`min_submissions`, `insufficient_data`).
  Тесты на границу 19/20:
  `internal/web/api/submissions_contract_test.go:240:TestResultStatsInsufficientDataThreshold`,
  `internal/web/api/submissions_test.go:145:TestResultStatsBoundariesThroughAPI`,
  `internal/tierlist/submissions_test.go:78:TestResultAndStatsBoundaries`.
- Отсутствие идентификаторов в публичной выдаче:
  `internal/web/api/submissions.go:toSubmissionDTO` не содержит `session_id`,
  `internal/web/api/results.go:67:toResultStatsDTO` копирует только агрегаты.
  Тесты проверяют отсутствие `session_id`/`user_id` в ответе, результате и
  статистике:
  `internal/web/api/submissions_contract_test.go:110:TestReplaceUpdatesStatsAndKeepsIdentifiersPrivate`
  и `:285`.
- Результат по id: `internal/web/api/results.go:51:getResult` →
  `internal/tierlist/submissions.go:Result`; контракт `api/openapi.yaml:504`.
  Тест `internal/web/api/submissions_test.go:76` проверяет `200` и `404` для
  отсутствующего результата.

### Gap

- **Неполный ответ не закреплён контрактом и граничным тестом.** SPEC §4
  прямо допускает неполный ответ: неразмещённые элементы пропускаются и не
  участвуют в сравнении. Код это позволяет: `Submit` проверяет только
  минимум 3 размещения, `internal/tierlist/submissions.go:161:validatePlacements`
  допускает подмножество элементов, а `similarity` сравнивает пересечение.
  Но описание `POST /tierlists/{slug}/submissions` в
  `api/openapi.yaml:403` говорит «Нужны ответы по всем элементам опроса», что
  противоречит `SubmissionInput` (`api/openapi.yaml:1040`: «неразмещённые
  считаются пропущенными... Минимум 3 размещения»). Ни
  `internal/web/api/submissions_test.go`, ни
  `internal/tierlist/submissions_test.go` не создают опрос с 4+ элементами и
  не отправляют частичный ответ из 3 элементов: все существующие тесты
  используют опрос ровно из 3 элементов (`createBody`,
  `internal/web/api/api_test.go:79`), поэтому граница «неполный ответ»
  остаётся непроверенной. Это ровно тот пункт, который требует критерий
  «контракт и тесты на границы».

### Excess

- Избыточного кода в границах TIERL-11 не найдено: маршруты, сервисные
  методы, распределение и статистика выводятся из SPEC §4–§5, ADR-001 и
  ADR-002; удалять код не требуется. Публичные `result_id` в
  `similar`/`dissimilar` описаны контрактом (`SimilarResult`) и не являются
  `session_id`/`user_id`; это существующее контрактное решение, а не
  отдельная возможность вне требования.

## Решения

1. **gap → change на восстановление требования и задачи на исправление
   артефактов.** Создан `openspec/changes/restore-submissions-results` с
   capability `submissions` и `results`. В `tasks.md` вынесены задачи:
   исправить противоречие в `api/openapi.yaml:403` на «минимум 3 размещённых
   элемента, остальные пропускаются» и добавить граничный тест частичного
   ответа (3 из 4+ элементов) на уровне сервиса и API. Код и тесты в границах
   аудита не меняются.
2. **Способности названы по существующим якорям.** Отправка ответа и правило
   «один ответ на сессию/неполный ответ/минимум 3» — это `submissions`;
   результат, распределение, «мало данных» и анонимность агрегатов — это
   `results`. Новое имя не вводилось.
3. **Excess: нет.** Задача на удаление кода не создаётся.
4. **Эскалация человеку не требуется.** Происхождение требования
   восстанавливается по `docs/SPEC.md` §4–§5, ADR-001/002 и
   `api/openapi.yaml`.

## Тестовое окружение

`go test ./internal/tierlist ./internal/web/api` проходит в этом окружении
(`ok` для обоих пакетов). Тесты, требующие Postgres/Docker, находятся в других
пакетах и здесь не запускались; поведение кода аудит не меняет.

## Границы

Код, тесты, контракт и applied-миграции не изменялись. Изменены только
`docs/audits/TIERL-11.md` и файлы
`openspec/changes/restore-submissions-results/`.
