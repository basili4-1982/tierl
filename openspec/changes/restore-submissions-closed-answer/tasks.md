# Tasks

## 1. Аудит TIERL-59

- [x] 1.1 Сопоставить R1–R3 с кодом: `internal/web/api/submissions.go:createSubmission`, `replaceSubmission`, `anonSession`, `internal/tierlist/submissions.go:Submit`, `validatePlacements`, `internal/storage/postgres/answers.go:Save`, `SubmissionBySession`
- [x] 1.2 Подтвердить границу минимума размещений и неполный ответ: `decodeSubmission`, `MinPlacements`, `TestPartialSubmissionThroughAPI`, `TestSubmitPartialAnswerIgnoresUnplacedItems`
- [x] 1.3 Подтвердить правило одного ответа и замену с сохранением id: `Submit`, `AlreadySubmittedError`, `UNIQUE (tierlist_id, session_id)`, `TestSubmitOnePerSessionAndReplace`, `TestMVPResubmissionContract`
- [x] 1.4 Записать вердикты `ok` по R1–R3, отсутствие gap в реализации и gap спеки по закрытому опросу в `docs/audits/submissions.md`

## 2. Восстановление требования

- [x] 2.1 Добавить в capability `submissions` требование «Ответ на закрытый опрос» для `POST /tierlists/{slug}/submissions` и `PUT /tierlists/{slug}/submissions/mine`
- [x] 2.2 Зафиксировать `409 tierlist_closed` и отсутствие сохранения/замены ответа для опроса не в статусе `open`
- [x] 2.3 Сохранить существующие требования R1–R3 без переписывания и не переименовывать якорь `submissions`

## 3. Проверки

- [x] 3.1 Подтвердить, что код, тесты, контракт и applied-миграции не менялись
- [x] 3.2 Проверить `openspec validate --strict --all --no-interactive` без ошибок
