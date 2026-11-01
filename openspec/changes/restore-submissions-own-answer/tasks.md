# Tasks

## 1. Аудит TIERL-59

- [x] 1.1 Сопоставить R1–R3 с кодом: `internal/web/api/submissions.go:createSubmission`, `replaceSubmission`, `anonSession`, `internal/tierlist/submissions.go:Submit`, `validatePlacements`, `internal/storage/postgres/answers.go:Save`, `SubmissionBySession`
- [x] 1.2 Подтвердить границу минимума размещений и неполный ответ: `decodeSubmission`, `MinPlacements`, `TestPartialSubmissionThroughAPI`, `TestSubmitPartialAnswerIgnoresUnplacedItems`
- [x] 1.3 Подтвердить правило одного ответа и замену с сохранением id: `Submit`, `AlreadySubmittedError`, `UNIQUE (tierlist_id, session_id)`, `TestSubmitOnePerSessionAndReplace`, `TestMVPResubmissionContract`
- [x] 1.4 Записать вердикты `ok` по R1–R3, отсутствие gap и excess в коде и пробел спеки в `docs/audits/submissions.md`

## 2. Восстановление требования

- [x] 2.1 Добавить в capability `submissions` требование «Чтение своего ответа» для `GET /api/v1/tierlists/{slug}/submissions/mine`
- [x] 2.2 Зафиксировать `200` с текущим ответом сессии и `404 not_found` без ответа, отсутствие побочных эффектов и приватность идентификаторов
- [x] 2.3 Сохранить существующие требования R1–R3 без переписывания и не переименовывать якорь `submissions`

## 3. Follow-up по контракту

- [ ] 3.1 Убрать `sessionCookie` и недостижимый `403` из описания `POST`/`PUT /tierlists/{slug}/submissions` в `api/openapi.yaml`, согласовав передачу ответа с анонимной сессией ADR-002; код, тесты и applied-миграции не менять
- [ ] 3.2 Привести формулировку `PUT /tierlists/{slug}/submissions/mine` («анонимная (или авторизованная) сессия») к анонимной сессии и проверить согласованность с `getMySubmission`

## 4. Проверки

- [x] 4.1 Подтвердить, что код, тесты, контракт и applied-миграции не менялись
- [x] 4.2 Проверить `openspec validate --strict --all --no-interactive` без ошибок
