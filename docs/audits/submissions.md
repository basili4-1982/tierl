# Аудит TIERL-59 — Прохождение опроса

## Контекст

Задача: TIERL-59 — аудит способности «Прохождение опроса» (якорь цели
`submissions`). Требования аудита: R1 — приём ответа на опрос; R2 — минимум три
размещения и неполный ответ; R3 — один ответ на анонимную сессию.

В формулировке задачи сказано, что живой спеки `submissions` нет. Фактически
она уже есть: `openspec/specs/submissions/spec.md` восстановлен архивным change
`2026-10-28-restore-submissions-results`, и его требования дословно совпадают с
R1–R3. Поэтому аудит сверяет код с живой спекой, `docs/SPEC.md` §4,
ADR-002, `api/openapi.yaml` и тестами, а не создаёт способность заново. Якорь
`submissions` не переименовывается.

Что проверялось:

- маршруты `POST /api/v1/tierlists/{slug}/submissions`,
  `PUT /api/v1/tierlists/{slug}/submissions/mine` и
  `GET /api/v1/tierlists/{slug}/submissions/mine`;
- выдача и проверка cookie `tierl_anon`, хранение только хэша токена;
- проверка принадлежности элементов и тиров опросу, запрет повторного элемента;
- граница минимума размещений, частичный ответ и его влияние на статистику;
- уникальность ответа на пару «опрос + анонимная сессия», повторный `POST` и
  замена через `PUT`;
- тесты API, сервиса, хранилища и E2E;
- происхождение требований в `docs/SPEC.md` §4, ADR-002 и OpenAPI.

## Вердикт

- R1: ok — internal/web/api/submissions.go:76:createSubmission — принимает анонимный `POST`, `anonSession` выдаёт cookie `tierl_anon`, `Submit`/`validatePlacements` отклоняют чужие и повторные элементы/тиры, ответ `201` содержит id результата, slug, размещения и время.
- R2: ok — internal/web/api/submissions.go:64:decodeSubmission — отклоняет меньше 3 размещений с `400 bad_request` до выдачи cookie и записи, а `Submit`/`validatePlacements` принимают неполный ответ; статистика учитывает только размещённые элементы.
- R3: ok — internal/tierlist/submissions.go:77:Submit — держит один ответ на «опрос + сессию» через `SubmissionBySession` и `UNIQUE (tierlist_id, session_id)`, повторный `POST` даёт `409 already_submitted` с `details.result_id`, а `PUT` сохраняет прежний id и отвечает `200`.

## Доказательства по требованиям

### R1 — приём ответа на опрос

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Маршрут `POST` без регистрации | `internal/web/api/api.go:175`, `internal/web/api/submissions.go:76:createSubmission` | Маршрут зарегистрирован без auth-middleware; обработчик не требует пользователя и вызывает `Submit` с `SessionID` из анонимной cookie |
| Выдача `tierl_anon`, если cookie нет | `internal/web/api/submissions.go:15:AnonCookieName`, `internal/web/api/submissions.go:133:anonSession` | `anonSession(..., true)` создаёт 32 случайных байта, ставит `HttpOnly`-cookie `tierl_anon` на год и возвращает `auth.HashToken(token)`; в БД попадает только хэш |
| Элемент и тир принадлежат опросу | `internal/tierlist/submissions.go:161:validatePlacements` | Функция строит множества `items` и `tiers` из опроса и возвращает `domain.ValidationError` для чужого элемента или тира |
| Элемент не повторяется | `internal/tierlist/submissions.go:161:validatePlacements`, `internal/storage/postgres/answers.go:17:Save` | Сервис проверяет `seen[p.ItemID]`, хранилище дополнительно возвращает ошибку на дубликат; в схеме `placements` первичный ключ `(submission_id, item_id)` |
| `422 validation_failed` для чужого элемента/тира | `internal/web/api/api.go:270:writeServiceError`, `internal/web/api/submissions_contract_test.go:20:TestSubmissionRejectsInvalidPlacements` | `domain.ValidationError` маппится в `422 validation_failed`; тест проверяет дубликат, неизвестный элемент и неизвестный тир, а затем отсутствие сохранённого ответа |
| `201` с id, slug, placements, временем | `internal/web/api/submissions.go:28:submissionDTO`, `internal/web/api/submissions.go:52:toSubmissionDTO`, `internal/web/api/submissions.go:76:createSubmission` | DTO содержит `id`, `tierlist_slug`, `placements`, `created_at`, `updated_at`; `createSubmission` пишет `http.StatusCreated` |
| Внешние ключи «элемент/тир внутри опроса» | `migrations/20261022100400_create_placements.up.sql` | Составные внешние ключи `(tierlist_id, item_id)` и `(tierlist_id, tier_id)` не дают сохранить размещение из другого опроса даже в обход сервиса |
| Тесты | `internal/web/api/submissions_test.go:81:TestSubmissionEndpoints`, `internal/web/api/submissions_contract_test.go:20`, `internal/app/mvp_e2e_test.go:292:TestMVPFlowRegistrationToResult` | Проверены `201`, cookie, состав ответа, отдельные сессии и отсутствие `session_id`/`user_id` в теле |

### R2 — минимум три размещения и неполный ответ

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Минимум 3 размещения | `internal/tierlist/submissions.go:16:MinPlacements` (`domain.MinItems = 3`), `internal/web/api/submissions.go:64:decodeSubmission`, `internal/tierlist/submissions.go:77:Submit` | API отклоняет меньше 3 размещений до выдачи cookie и записи; сервис повторно проверяет `len(in.Placements) < MinPlacements` |
| Меньше 3 — `400 bad_request`, без сохранения | `internal/web/api/submissions.go:64:decodeSubmission`, `internal/web/api/submissions_test.go:81`, `internal/web/api/submissions_contract_test.go:20` | `decodeSubmission` отвечает `400 bad_request`; тесты отправляют 0/1/2 размещения и проверяют, что `submissions_count` остался нулевым |
| Ровно 3 принимаются | `internal/tierlist/submissions.go:77:Submit`, `internal/web/api/submissions_test.go:81` | Ответ из 3 размещений даёт `201`, а в `placements` ровно 3 элемента |
| Неполный ответ допустим | `internal/tierlist/submissions.go:161:validatePlacements`, `internal/web/api/submissions_test.go:214:TestPartialSubmissionThroughAPI`, `internal/tierlist/submissions_test.go:150:TestSubmitPartialAnswerIgnoresUnplacedItems` | `validatePlacements` не требует всех элементов опроса; тесты на опросе из 4 элементов размещают 3 и получают `201` |
| Неразмещённый элемент не попадает в ответ | `internal/web/api/submissions.go:52:toSubmissionDTO`, `internal/storage/postgres/answers.go:174:submission` | `placements` читаются из таблицы `placements`, где у пропущенного элемента нет строки, поэтому в DTO он отсутствует |
| Неразмещённый элемент не участвует в статистике и сравнении | `internal/storage/postgres/answers.go:124:Distributions`, `internal/tierlist/submissions.go:283:Stats`, `internal/app/mvp_e2e_test.go` | Распределение строится `CROSS JOIN tiers` + `LEFT JOIN item_tier_counts`, поэтому у пропущенного элемента все счётчики нулевые; `Answers` и `Similarity` видят только размещённые элементы |
| Тесты границы и неполного ответа | `internal/tierlist/submissions_test.go:31:TestSubmitRequiresThreePlacements`, `:150`, `internal/web/api/submissions_test.go:214`, `internal/web/api/submissions_contract_test.go:20` | Покрыты 2/3 размещения, дубликаты, чужие элементы и опрос из 4 элементов с 3 размещениями |

### R3 — один ответ на анонимную сессию

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Не более одного ответа на «опрос + сессия» | `migrations/20261022100300_create_submissions.up.sql:10`, `internal/storage/postgres/answers.go:17:Save` | Ограничение `UNIQUE (tierlist_id, session_id)` и upsert `ON CONFLICT (tierlist_id, session_id)` сериализуют записи одной сессии |
| Повторный `POST` — `409 already_submitted` | `internal/tierlist/submissions.go:77:Submit`, `internal/tierlist/submissions.go:31:AlreadySubmittedError`, `internal/web/api/api.go:294` | При `replace=false` найденный или конфликтный ответ превращается в `AlreadySubmittedError`; API отвечает `409` с кодом `already_submitted` |
| `details.result_id` существующего результата | `internal/web/api/api.go:294:writeServiceError`, `internal/web/api/submissions_test.go:81`, `internal/app/mvp_e2e_test.go:368:TestMVPResubmissionContract` | `errors.As` извлекает `AlreadySubmittedError.ResultID` в `details.result_id`; тесты сверяют его с id первого ответа |
| Новый ответ не создаётся | `internal/storage/postgres/answers.go:17:Save`, `internal/tierlist/submissions.go:77:Submit` | Ответ ищется до записи; при конфликте на уровне БД `SaveResult.Replaced` превращается в `AlreadySubmittedError`, а `POST` не возвращает `201` |
| `PUT` заменяет ответ той же сессии | `internal/web/api/submissions.go:97:replaceSubmission`, `internal/tierlist/submissions.go:77:Submit` (`replace=true`), `internal/storage/postgres/answers.go:17:Save` | `replaceSubmission` вызывает `Submit(..., true)`; `Submit` берёт `existing.ID`, а upsert сохраняет прежнюю строку и её id |
| `PUT` — `200` с тем же id | `internal/web/api/submissions.go:97:replaceSubmission`, `internal/web/api/submissions_test.go:81`, `internal/web/api/submissions_contract_test.go:110:TestReplaceUpdatesStatsAndKeepsIdentifiersPrivate` | Замена отвечает `200`, `id` совпадает с первым ответом, распределение пересчитывается |
| Общее число ответов не растёт | `internal/storage/postgres/answers.go:17:Save`, `internal/web/api/submissions_contract_test.go:110` | При замене сначала уменьшаются счётчики и удаляются старые размещения, новая строка `submissions` не вставляется; тест проверяет `total_submissions = 1` после `PUT` |
| Тесты | `internal/tierlist/submissions_test.go:41:TestSubmitOnePerSessionAndReplace`, `internal/web/api/submissions_test.go:81`, `internal/app/mvp_e2e_test.go:368` | Покрыты повторный `POST`, сохранение id при `PUT`, замена размещений и отсутствие привязки к аккаунту |

## Gap

Gap в коде по R1–R3 не найден: приём ответа, минимум три размещения, частичный
ответ и правило «один ответ на анонимную сессию» реализованы и покрыты
тестами.

Найден **gap спеки**, а не реализации: живая `submissions` описывает только
`POST` и `PUT`, но не описывает `GET
/api/v1/tierlists/{slug}/submissions/mine`. Этот маршрут реализован в
`internal/web/api/submissions.go:118:getMySubmission`, описан в
`api/openapi.yaml` (`operationId: getMySubmission`) и используется интерфейсом
прохождения опроса (`web/src/features/take-tierlist/api.ts:fetchMySubmission`),
чтобы не отвечать повторно и показать уже отправленный ответ. Без требования
у части способности «Прохождение опроса» нет полной цепочки «цель → спека».

## Excess

Excess в границах TIERL-59 не найден. `getMySubmission` не является кодом без
требования: он следует `api/openapi.yaml` и клиентскому сценарию прохождения
опроса, поэтому выносится не на удаление, а в требование спеки. Проверки
принадлежности элементов и тиров, уникальности ответа и выдача cookie прямо
следуют R1–R3, `docs/SPEC.md` §4 и ADR-002. Удалять код не требуется.

## Замечания вне вердикта

1. **Контракт говорит об авторизованной сессии, код — только об анонимной.**
   `api/openapi.yaml` для `POST`/`PUT /submissions` перечисляет `sessionCookie`,
   а описание `PUT` называет «анонимную (или авторизованную) сессию». Код
   читает только cookie `tierl_anon` (`internal/web/api/submissions.go:133`), а
   ADR-002 прямо требует не привязывать ответ к аккаунту. Это расхождение
   документации с нормативным решением, а не gap R1–R3.
2. **`403` в контракте не достигается.** OpenAPI перечисляет `403 Forbidden`
   для `POST`/`PUT` ответа, но обработчики не требуют пользователя и не
   возвращают `403`; ответы на закрытый опрос дают `409 tierlist_closed`
   (`internal/web/api/api.go:296`). Это тоже документационное расхождение.

Оба пункта вынесены в follow-up задачи change без изменения кода и контракта в
границах аудита.

## Решения

1. **Gap спеки → change на восстановление требования.** Создан change
   `restore-submissions-own-answer` с capability `submissions` (якорь не
   переименован). В `specs/submissions/spec.md` добавлено требование «Чтение
   своего ответа» для `GET /tierlists/{slug}/submissions/mine`: `200` с
   текущим ответом сессии либо `404 not_found`, без создания ответа и cookie.
   Существующие R1–R3 не переписываются.
2. **Gap кода отсутствует → задача на реализацию не создаётся.** R1, R2 и R3
   уже реализованы; изменение поведения кода не требуется.
3. **Excess отсутствует → задача на удаление кода не создаётся.**
   `getMySubmission` подтверждён контрактом и UI и восстанавливается в спеке.
4. **Расхождения контракта вынесены в follow-up.** В
   `restore-submissions-own-answer/tasks.md` добавлены задачи: убрать
   `sessionCookie`/`403` из описания передачи ответа и привести `PUT` к
   анонимной сессии ADR-002 без изменения кода.
5. **Способность названа по якорю `submissions`.** Новое имя не вводится:
   чтение своего ответа — часть той же цели «Прохождение опроса».

## Проверки

- `test -f docs/audits/submissions.md` и grep по строкам `- R1:`, `- R2:`,
  `- R3:` с доказательством — все три вердикта присутствуют.
- `go mod download` — зависимости Go загружены; `go test ./internal/tierlist
  ./internal/web/api` — пакеты прошли, поведение кода не менялось.
- `openspec validate --strict --all --no-interactive` — без ошибок.

## Границы

Код, тесты, контракт и применённые миграции не изменялись. Изменены только
`docs/audits/submissions.md` и файлы
`openspec/changes/restore-submissions-own-answer/`.
