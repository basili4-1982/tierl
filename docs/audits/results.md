# Аудит TIERL-52 — Результат и метрика сходства

## Контекст

Задача: TIERL-52 — аудит способности «Результат и метрика сходства» (якорь
цели `results`). Требования аудита: R1 — результат по идентификатору; R2 —
распределение ответов по тирам; R3 — порог «мало данных»; R4 — анонимность
публичной статистики.

В отличие от формулировки задачи, живая capability `results` в workspace уже
существует: `openspec/specs/results/spec.md` восстановлен архивным change
`2026-10-28-restore-submissions-results`. Поэтому аудит сверяет код с живой
спекой и `docs/SPEC.md` §4–§5, ADR-001, ADR-002 и `api/openapi.yaml`, а не
создаёт способность заново. Якорь `results` не переименовывается.

Что проверялось:

- маршруты `GET /api/v1/results/{id}` и `GET /api/v1/results/{id}/stats`;
- сервис результата и статистики, генерация публичного id, распределение по
  тирам, порог «мало данных», формула сходства;
- состав публичной выдачи на наличие `session_id`, `user_id`, email и IP;
- тесты API, сервиса, метрики и UI результата;
- происхождение требований в `docs/SPEC.md`, ADR-001/002 и OpenAPI.

## Вердикт

- R1: ok — internal/web/api/results.go:getResult.Returns результат по `GET /api/v1/results/{id}` без регистрации: отдаёт `submission` и `tierlist`, неизвестный id даёт `404 not_found`, публичный id — случайный UUID v4.
- R2: ok — internal/tierlist/submissions.go:Stats.Distributes отдаёт по каждому элементу `item_id`, `label` и все тиры с `tier_id`, `count`, `percent`; неразмещённый элемент не получает размещения.
- R3: ok — internal/tierlist/submissions.go:Stats.Gates статистику порогом `min_submissions = 20`: при меньшем числе `insufficient_data = true`, `like_you_percent = null`, `similar`/`dissimilar` пусты; при 20+ считает долю и отдаёт не более 5 похожих и 5 непохожих.
- R4: ok — internal/web/api/results.go:toResultStatsDTO.Aggregates копирует в ответ только агрегаты и публичные id результатов; `session_id`, `user_id`, email и IP участников в статистику не попадают.

## Доказательства по требованиям

### R1 — результат по идентификатору

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Маршрут и доступ без регистрации | `internal/web/api/api.go:178` (`GET /api/v1/results/{id}`), `internal/web/api/results.go:51:getResult` | Маршрут зарегистрирован без auth-middleware; обработчик получает `viewer` необязательно и вызывает `Service.Result` |
| Ответ `200` с `submission` и `tierlist` | `internal/web/api/results.go:9:resultDTO`, `internal/web/api/results.go:51:getResult`, `internal/web/api/submissions.go:52:toSubmissionDTO`, `internal/web/api/tierlists.go:93:toTierlistDTO` | `resultDTO` содержит ровно `submission` и `tierlist`; `submission.id` — id результата, `tierlist.slug` — исходный опрос |
| Неизвестный id — `404 not_found` | `internal/tierlist/submissions.go:207:Result`, `internal/storage/postgres/answers.go:165:SubmissionByID`, `internal/web/api/api.go:writeServiceError` | `SubmissionByID` возвращает `ErrNotFound` на `pgx.ErrNoRows`; сервис пробрасывает его, API отдаёт `404 not_found` |
| Непредсказуемый публичный id | `internal/tierlist/service.go:537:newID` | id — UUID v4 из `crypto/rand.Reader` (`s.rand`), а не порядковый номер и не id сессии |
| Нет `session_id`, `user_id`, email, IP | `internal/web/api/results.go:51:getResult`, `internal/web/api/submissions.go:28:submissionDTO`, `internal/web/api/tierlists.go:77:tierlistDTO` | `submissionDTO` не содержит `SessionID`; `SubmissionRecord.SessionID` остаётся в сервисе и не маппится в JSON. `tierlist.author.id` — публичный id автора опроса, а не идентификатор участника или сессии |
| Тесты | `internal/web/api/submissions_test.go:81:TestSubmissionEndpoints`, `internal/web/api/submissions_contract_test.go:110:TestReplaceUpdatesStatsAndKeepsIdentifiersPrivate` | Проверены `200`, совпадение `submission.id` с запрошенным id, `tierlist.slug`, `404` для `missing` и отсутствие `session_id`/`user_id` в теле |

### R2 — распределение ответов по тирам

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Распределение в `GET /results/{id}/stats` | `internal/web/api/api.go:179`, `internal/web/api/results.go:100:getResultStats`, `internal/tierlist/submissions.go:283:Stats` | Обработчик вызывает `Stats`, который читает `Distributions` и собирает `ResultStats.Items` |
| `item_id`, `label` и все тиры | `internal/tierlist/submissions.go:344:Stats`, `internal/storage/postgres/answers.go:124:Distributions`, `internal/web/api/results.go:20:itemDistributionDTO` | SQL делает `CROSS JOIN tiers` и `LEFT JOIN item_tier_counts`, поэтому каждый элемент получает запись по каждому тиру даже с нулём; `label` берётся из `itemIndex` |
| `count` и `percent` | `internal/tierlist/submissions.go:360:Stats`, `internal/web/api/results.go:31:resultStatsDTO` | `count` — из агрегата `item_tier_counts`; `percent = round(count*1000/total)/10`, где `total = len(all)` — число ответов на опрос |
| Неразмещённый элемент не считается размещённым | `internal/storage/postgres/answers.go:17:Save`, `internal/storage/postgres/answers.go:124:Distributions` | Счётчики увеличиваются только по `placements`; неразмещённый элемент отсутствует в `placements`, поэтому его `count` остаётся нулевым |
| Тесты | `internal/tierlist/submissions_test.go:78:TestResultAndStatsBoundaries`, `internal/web/api/submissions_test.go:214:TestPartialSubmissionThroughAPI`, `internal/web/api/submissions_contract_test.go:240:TestResultStatsInsufficientDataThreshold` | Проверены все тиры, точные `count`/`percent`, а также 4 элемента при 3 размещениях: у неразмещённого элемента все счётчики нулевые |

### R3 — порог «мало данных»

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Поля `total_submissions`, `min_submissions`, `insufficient_data` | `internal/tierlist/submissions.go:270:ResultStats`, `internal/tierlist/submissions.go:330:Stats`, `internal/web/api/results.go:31:resultStatsDTO` | Поля заполняются из числа ответов и `MinSubmissions`; маппятся в JSON без потерь |
| Порог по умолчанию `20` | `internal/similarity/similarity.go:16:DefaultMinAnswers`, `internal/tierlist/submissions.go:20:MinSubmissions` | `MinSubmissions = similarity.DefaultMinAnswers = 20`; `Stats` возвращает его в `min_submissions` |
| Меньше порога: «мало данных», `null`, пустые списки | `internal/similarity/similarity.go:100:ThinkLikeYou`, `internal/tierlist/submissions.go:338:Stats`, `internal/tierlist/submissions.go:369:Stats` | При `len(others)+1 < 20` `ThinkLikeYou` выставляет `NotEnoughData`; `Stats` не заполняет `LikeYouPercent` и возвращается до построения `similar`/`dissimilar`, оставляя их пустыми |
| 20+: доля по порогу и не более 5+5 | `internal/similarity/similarity.go:57:Similarity`, `internal/similarity/similarity.go:100:ThinkLikeYou`, `internal/tierlist/submissions.go:382:Stats` | Доля — `Matched/Compared` по парам со сходством `>= Threshold`; ранжирование сортирует по убыванию, берёт до 5 сверху и до 5 снизу, запрошенный результат исключён из `others` |
| Тесты границы 19/20 | `internal/tierlist/submissions_test.go:78:TestResultAndStatsBoundaries`, `internal/web/api/submissions_test.go:150:TestResultStatsBoundariesThroughAPI`, `internal/web/api/submissions_contract_test.go:240:TestResultStatsInsufficientDataThreshold` | Проверены 19 ответов: `insufficient_data = true`, `min_submissions = 20`, `like_you_percent = null`, пустые списки; 20 ответов: `insufficient_data = false`, `like_you_percent = 100`, по 5 похожих и непохожих, запрошенный результат не в `similar` |
| Тесты метрики | `internal/similarity/similarity_test.go:TestSimilarity`, `TestSimilarityProperties`, `TestThinkLikeYou`, `TestThinkLikeYouThresholdBoundary`, `TestThinkLikeYouProperties` | Покрыты формула, симметричность, диапазон `[0,1]`, одинаковые/крайние ответы, малое пересечение и граница порога |

### R4 — анонимность публичной статистики

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| В статистике только агрегаты | `internal/web/api/results.go:31:resultStatsDTO`, `internal/web/api/results.go:67:toResultStatsDTO` | DTO содержит `total_submissions`, `insufficient_data`, `min_submissions`, `like_you_percent`, `similarity_threshold`, `items`, `similar`, `dissimilar`; поля сессии и пользователя отсутствуют |
| Нет `session_id`, `user_id`, email, IP | `internal/web/api/results.go:67:toResultStatsDTO`, `internal/tierlist/submissions.go:283:Stats` | Явный комментарий и код маппинга копируют только агрегаты; `SubmissionRecord.SessionID` не попадает в ответ, email/IP в статистике не читаются |
| Похожие/непохожие — агрегаты без идентификации | `internal/tierlist/submissions.go:264:SimilarResult`, `internal/web/api/results.go:67:toResultStatsDTO` | В списках только публичный `result_id` и `similarity`; нет сессии, аккаунта, email или IP |
| Распределение остаётся агрегированным | `internal/storage/postgres/answers.go:124:Distributions`, `internal/tierlist/submissions.go:344:Stats` | Ответ строится из счётчиков `item_tier_counts`, а не из перечня участников |
| Тесты | `internal/web/api/submissions_test.go:214:TestPartialSubmissionThroughAPI`, `internal/web/api/submissions_contract_test.go:110:TestReplaceUpdatesStatsAndKeepsIdentifiersPrivate` | Проверяется отсутствие `session_id`, `user_id`, `email`, `ip_address`, `client_ip`, `127.0.0.1` и `::1` в теле результата и статистики |

## Gap

Gap в коде по R1–R4 не найден: все четыре требования реализованы и покрыты
тестами.

Найден **gap спеки**, а не реализации: живая `results` не описывает
нормативную формулу метрики сходства и `min_overlap = 3` (они есть только в
`docs/SPEC.md` §5 и ADR-001), а поле `similarity_threshold`, которое реально
возвращает `GET /api/v1/results/{id}/stats` и требует `api/openapi.yaml`, не
названо в требовании о пороге. Из-за этого у части способности «Результат и
метрика сходства» нет полной цепочки «цель → спека».

Отдельно зафиксировано расхождение контракта: пример в
`api/openapi.yaml` для `/results/{id}/stats` содержит `min_submissions: 10`,
хотя нормативное значение и код — `20`. На поведение API это не влияет, но
пример вводит в заблуждение.

## Excess

Excess в границах TIERL-52 не найден. Поля `similarity_threshold`,
`similar`/`dissimilar` и `percent` не являются кодом без требования: они
следуют `docs/SPEC.md` §5 («число и порог возвращает API», «наиболее и
наименее похожие участники»), ADR-001 и `api/openapi.yaml` (схема
`ResultStats`). `tierlist.author.id` в ответе результата относится к
публичному представлению опроса (capability `tierlists`/`sharing`), а не к
идентификатору участника. Удалять код не требуется.

## Решения

1. **Gap спеки → change на восстановление требования.** Создан change
   `restore-results-metric` с capability `results` (якорь не переименован).
   В `specs/results/spec.md` добавлено требование «Метрика сходства и порог»:
   формула ADR-001, `min_overlap = 3`, порог `0.80` и публикуемое поле
   `similarity_threshold`. Существующие R1–R4 не переписываются.
2. **Gap кода отсутствует → задача на реализацию не создаётся.** R1, R2, R3 и
   R4 уже реализованы; изменение поведения кода не требуется.
3. **Excess отсутствует → задача на удаление кода не создаётся.** Соседние
   поля и агрегаты принадлежат требованиям `results`, `tierlists` и `sharing`,
   а не избыточному коду.
4. **Расхождение примера OpenAPI вынесено в задачу.** В
   `restore-results-metric/tasks.md` добавлен follow-up: привести пример
   `min_submissions` к `20` и сверить `similarity_threshold`/`maxItems: 5` без
   изменения кода, тестов и применяемых миграций.
5. **Способность названа по якорю `results`.** Новое имя не вводится: метрика
   сходства — часть той же цели «Результат и метрика сходства», что и публичная
   статистика.

## Проверки

- `f=docs/audits/results.md; test -f "$f"` и grep по строкам вердикта R1–R4 —
  файл и все четыре строки присутствуют.
- `go mod download` — зависимости Go загружены; `go test ./internal/similarity
  ./internal/tierlist ./internal/web/api` — пакеты прошли, поведение кода не
  менялось.
- `openspec validate --strict --all --no-interactive` — 14 passed, 0 failed.

## Границы

Код, тесты, контракт и применённые миграции не изменялись. Изменены только
`docs/audits/results.md` и файлы
`openspec/changes/restore-results-metric/`.
