# Tasks

## 1. Происхождение работы

- [x] 1.1 Сопоставить TIERL-15 с целью из `openspec/changes/archive/2026-10-27-add-profile-views/proposal.md`; результат записан в `proposal.md` и `design.md`
- [x] 1.2 Сверить API-часть с `openspec/specs/author-profile/spec.md`: список своих опросов, управление, смена пароля — требования есть
- [x] 1.3 Найти часть работы без требования: страница `/profile` упомянута только в proposal и tasks архивного change, но отсутствует в канонической спеке
- [x] 1.4 Классифицировать работу как необходимую: избыточных маршрутов и мёртвого кода в границах аудита не найдено, удаление не требуется
- [x] 1.5 Отметить выполненные пункты в `openspec/changes/archive/2026-10-27-add-profile-views/tasks.md`

## 2. Восстановление спеки

- [x] 2.1 Добавить требование «Страница профиля автора» со сценариями списка, действий и формы смены пароля
- [x] 2.2 Связать UI-сценарии с существующими требованиями к `GET /api/v1/me/tierlists`, `PATCH/DELETE /api/v1/tierlists/{id}` и `POST /api/v1/me/password`

## 3. Сверка кода со спекой

- [x] 3.1 Проверить защиту маршрута `/profile` и перенаправление гостя — подтверждено `web/src/features/auth/RequireAuth.tsx` и `web/src/routes.tsx`
- [x] 3.2 Проверить список, действия и форму пароля — подтверждено `web/src/features/profile/ProfilePage.test.tsx`
- [x] 3.3 Проверить API-часть — подтверждено `internal/web/api/me_test.go` и `internal/web/api/auth_test.go`

## 4. Проверки

- [x] 4.1 Запустить `go test ./...`
- [x] 4.2 Запустить `cd web && npm test -- --run && npm run build`
- [x] 4.3 Проверить `openspec validate restore-author-profile --strict` без ошибок

## Workflow follow-up

- После ревью архивировать изменение, чтобы требование попало в `openspec/specs/author-profile/spec.md`.
