# Tasks

## 1. Спека и происхождение

- [x] 1.1 Сопоставить TIERL-9 с критерием готовности MVP (`docs/SPEC.md`, разделы 1, 4 и 11) и контрактом `api/openapi.yaml`; результат записан в `proposal.md` и `design.md`
- [x] 1.2 Проверить артефакт `internal/app/mvp_e2e_test.go` и существующие маршруты `internal/web/api`; зафиксировать, что подтверждение email, ответы гостя, результат и повторная отправка обходят публичный API
- [x] 1.3 Записать вердикт `gap` с доказательствами в `docs/audits/TIERL-9.md`; проверка — строка `- TIERL-9: gap — …` и ссылки на файлы/символы

## 2. Восстановление спеки

- [x] 2.1 Добавить capability `mvp-e2e` с требованиями к сквозному пути MVP, unlisted вне каталога и повторной отправке по контракту
- [x] 2.2 Обосновать новое имя `mvp-e2e` пересечением якорей `accounts`, `tierlists`, `sharing`, `submissions`, `results` и `catalog` в `proposal.md`
- [x] 2.3 Связать требование с существующими маршрутами `internal/web/api/api.go` и контрактом `api/openapi.yaml`

## 3. Исправление E2E-теста

- [ ] 3.1 Заменить прямой `UPDATE users` в `internal/app/mvp_e2e_test.go:confirmAuthor` на `POST /api/v1/auth/verify-email` с токеном из тестового канала доставки; проверка — тест не содержит ручных изменений бизнес-состояния в БД
- [ ] 3.2 Заменить `f.store.Save` для гостей на `POST /api/v1/tierlists/{slug}/submissions` с анонимными cookie, а расчёт результата — на `GET /api/v1/results/{id}`; проверка — 20 гостей проходят через HTTP, результат проверяется по ответу API
- [ ] 3.3 Переписать `TestMVPResubmissionContract` на контракт: повторный `POST` → `409 already_submitted` с `details.result_id`, `PUT /submissions/mine` → `200` с тем же `id`, счётчики и распределение обновляются; проверка — тест в `internal/app/mvp_e2e_test.go`
- [ ] 3.4 Проверить unlisted через `GET /api/v1/catalog` (после `restore-catalog`) и доступность unlisted по прямой ссылке; до реализации каталога сохранить явно помеченную временную проверку `Store.ListPublic`
- [ ] 3.5 Убедиться, что тест зелёный на чистой БД после `MigrateDown`/`MigrateUp`; проверка — `go test -race ./internal/app`

## 4. Проверки аудита

- [x] 4.1 Записать вердикт аудита и решения в `docs/audits/TIERL-9.md`
- [x] 4.2 Проверить `openspec validate --strict --all --no-interactive` без ошибок

## Workflow follow-up

- После ревью и реализации архивировать изменение, чтобы capability `mvp-e2e` попала в `openspec/specs/mvp-e2e/spec.md`.
