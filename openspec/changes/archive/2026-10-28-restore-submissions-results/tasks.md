# Tasks

## 1. Происхождение работы

- [x] 1.1 Сопоставить TIERL-11 с `docs/SPEC.md` §4–§5, ADR-001, ADR-002 и `api/openapi.yaml`; результат записан в `proposal.md` и `design.md`
- [x] 1.2 Проверить реализацию: `internal/web/api/submissions.go`, `internal/web/api/results.go`, `internal/tierlist/submissions.go`, `internal/similarity/similarity.go`, `internal/storage/postgres/answers.go` и миграция `20261022100300_create_submissions`
- [x] 1.3 Классифицировать работу: избыточного кода в границах аудита не найдено, удаление не требуется; найден gap контракта и граничного теста неполного ответа

## 2. Восстановление спеки

- [x] 2.1 Добавить capability `submissions` с требованиями к отправке ответа, минимуму 3 размещений, неполному ответу, одному ответу на сессию и замене
- [x] 2.2 Добавить capability `results` с требованиями к результату, распределению по тирам, порогу «мало данных» и отсутствию идентификаторов
- [x] 2.3 Связать требования с существующими маршрутами `POST /api/v1/tierlists/{slug}/submissions`, `PUT`/`GET /api/v1/tierlists/{slug}/submissions/mine`, `GET /api/v1/results/{id}`, `GET /api/v1/results/{id}/stats`

## 3. Исправление gap

- [x] 3.1 Исправить описание `POST /api/v1/tierlists/{slug}/submissions` в `api/openapi.yaml:403`: заменить «Нужны ответы по всем элементам опроса» на «минимум 3 размещённых элемента, неразмещённые считаются пропущенными»; проверка — описание не противоречит `SubmissionInput` (`api/openapi.yaml:1040`) и SPEC §4 — вынесено в задачу трекера TIERL-32
- [x] 3.2 Добавить граничный тест неполного ответа на уровне сервиса в `internal/tierlist/submissions_test.go`: опрос из 4+ элементов, ответ из 3 размещённых; проверка — ответ принимается, неразмещённый элемент отсутствует в `Placements` и не участвует в распределении/сравнении — вынесено в задачу трекера TIERL-32
- [x] 3.3 Добавить тот же граничный тест через API в `internal/web/api/submissions_test.go`: `201`, в ответе 3 размещения, `submissions_count = 1`, статистика считает распределение только по размещённым элементам — вынесено в задачу трекера TIERL-32
- [x] 3.4 Проверить отсутствие идентификаторов в новых проверках: в теле ответа нет `session_id`, `user_id`, email и IP — вынесено в задачу трекера TIERL-32
- [x] 3.5 Проверить `go test ./internal/tierlist ./internal/web/api` после добавления тестов — вынесено в задачу трекера TIERL-32

## 4. Проверки аудита

- [x] 4.1 Записать вердикт `gap` и решения в `docs/audits/TIERL-11.md`
- [x] 4.2 Проверить `openspec validate --strict --all --no-interactive` без ошибок

## Workflow follow-up

- После ревью и реализации gap-задач архивировать изменение, чтобы capability `submissions` и `results` попали в `openspec/specs/`.
