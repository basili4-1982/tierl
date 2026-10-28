# Tasks

## 1. Происхождение работы

- [x] 1.1 Сопоставить TIERL-33 с `docs/SPEC.md` §1, §4 и §11, `api/openapi.yaml` (`/auth/verify-email`, `/tierlists/{slug}`, `/tierlists/{slug}/submissions`, `/tierlists/{slug}/submissions/mine`, `/results/{id}`, `/results/{id}/stats`) и `docs/adr/ADR-002-anonymous-answers.md`; результат записан в `proposal.md` и `design.md`
- [x] 1.2 Сверить поведение с существующими capability `accounts`, `tierlists`, `submissions`, `results` и `catalog`: сквозной путь не добавляет нового поведения продукта
- [x] 1.3 Классифицировать работу как необходимую: избыточного кода и мёртвых веток в границах аудита не найдено, удаление не требуется

## 2. Восстановление спеки

- [x] 2.1 Добавить capability `mvp-e2e` с требованиями к пути автора без ручных действий в БД, ответу и результату гостя через публичный API, повторной отправке по контракту и недоступности unlisted в каталоге
- [x] 2.2 Обосновать новое имя capability: ни один якорь цели (`accounts`, `tierlists`, `submissions`, `results`, `catalog`, `reports`, `sharing`, `author-profile`, `item-images`, `self-host`) не покрывает весь сквозной путь
- [x] 2.3 Связать требования с существующими маршрутами и анонимной cookie `tierl_anon`, не меняя контракт

## 3. Сверка кода со спекой

- [x] 3.1 Проверить подтверждение email: `internal/app/mvp_e2e_test.go:confirmAuthor` берёт токен из `mailertest.Capture` и вызывает `POST /api/v1/auth/verify-email`; маршрут — `internal/web/api/api.go:156`, обработчик — `internal/web/api/auth.go:337:verifyEmail`
- [x] 3.2 Проверить отсутствие ручных изменений бизнес-состояния: в `internal/app/mvp_e2e_test.go` нет `UPDATE users`, `email_verified = true` и `f.store.Save`
- [x] 3.3 Проверить ответы гостей: `TestMVPFlowRegistrationToResult` отправляет 20 ответов через `POST /api/v1/tierlists/{slug}/submissions` с cookie `tierl_anon`; обработчик — `internal/web/api/submissions.go:76:createSubmission`
- [x] 3.4 Проверить результат и статистику: `GET /api/v1/results/{id}` и `/stats` в `mvp_e2e_test.go:341`, `:354`; обработчики — `internal/web/api/results.go:51:getResult` и `:100:getResultStats`
- [x] 3.5 Проверить повторную отправку: `TestMVPResubmissionContract` ожидает `409 already_submitted`, `details.result_id` и замену через `PUT /submissions/mine` с сохранением `id`
- [x] 3.6 Проверить unlisted: `TestMVPUnlistedNotInCatalog` подтверждает доступ по ссылке и отсутствие в публичном каталоге; HTTP-проверка каталога вынесена в follow-up

## 4. Проверки

- [x] 4.1 Записать вердикт `ok` с доказательствами в `docs/audits/TIERL-33.md`
- [x] 4.2 Проверить `go test ./internal/app -run TestMVP -count=1` (в окружении без Docker тесты пропускаются через `testDB`, пакет компилируется)
- [x] 4.3 Проверить `openspec validate --strict --all --no-interactive` без ошибок

## 5. Follow-up

- [ ] 5.1 Заменить чтение `Store.ListPublic` в `TestMVPUnlistedNotInCatalog` на `GET /api/v1/catalog` после реализации маршрута; проверка — unlisted отсутствует в HTTP-ответе каталога. Относится к `openspec/changes/add-catalog/tasks.md`, пункт 2.4; код и тесты в границах аудита не меняются

## Workflow follow-up

- После ревью и реализации follow-up архивировать изменение, чтобы capability `mvp-e2e` попала в `openspec/specs/mvp-e2e/spec.md`.
