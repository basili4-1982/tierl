# Аудит TIERL-53 — Профиль автора

## Контекст

Задача: TIERL-53 — аудит способности «Профиль автора» (якорь цели
`author-profile`). Требования аудита: R1 — список своих опросов; R2 —
управление своим опросом; R3 — смена пароля закрывает остальные сессии; R4 —
страница профиля автора.

Происхождение требования восстанавливается по `docs/SPEC.md` §2 (пользователь
управляет своими опросами), §6 (сессии и сброс всех сессий после смены пароля),
§11 (путь автора без ручных действий в БД), `docs/adr/ADR-003-auth.md`,
`api/openapi.yaml` (`GET /me/tierlists`) и архивным целям
`openspec/changes/archive/2026-10-27-add-profile-views` и
`openspec/changes/archive/2026-10-28-restore-author-profile`.

В текущем снимке каноническая спека `openspec/specs/author-profile/spec.md` уже
содержит все четыре требования: она восстановлена архивным change
`2026-10-28-restore-author-profile`. Поэтому аудит проверяет фактический код
против R1–R4, а change оформляется как MODIFIED-дельта к существующей
capability: ADDED-требования с теми же заголовками архив бы отклонил как
дубликаты.

## Вердикт

- R1: ok — internal/web/api/tierlists.go:listMyTierlists возвращает только опросы текущего автора через `ListMine` и `OwnerStore.ListByOwner` с фильтром `author_id`, состоянием и агрегатами.
- R2: ok — internal/tierlist/service.go:owned проверяет владельца, а `Update`/`Close`/`Delete` и `SetStatusForOwner`/`DeleteForOwner` закрывают, переоткрывают и удаляют только свой опрос; чужой даёт `404`.
- R3: ok — internal/web/api/auth.go:changeMyPassword вызывает `ChangePasswordKeepingCurrent`, который в одной транзакции меняет хэш и удаляет все сессии, кроме текущей (`UpdatePasswordAndDeleteSessions`).
- R4: ok — web/src/features/profile/ProfilePage.tsx:ProfilePage показывает свои опросы, действия и форму пароля за `RequireAuth`; `RequireAuth`/`LoginPage` перенаправляют гостя на вход и возвращают на `/profile`.

## Доказательства по требованиям

### R1 — список своих опросов

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Маршрут и обязательная сессия | `internal/web/api/api.go:New` (`GET /api/v1/me/tierlists`), `internal/web/api/tierlists.go:listMyTierlists`, `internal/web/api/api.go:requireUser` | Без сессии `requireUser` пишет `401 unauthorized`; с сессией обработчик передаёт её `userID` в сервис |
| Только свои опросы | `internal/tierlist/service.go:ListMine`, `internal/storage/postgres/owners.go:ListByOwner` | `ListMine` берёт `OwnerStore.ListByOwner`, а SQL фильтрует `WHERE t.author_id = $1`, поэтому чужой опрос не попадает в ответ |
| Состояние и агрегаты | `internal/web/api/tierlists.go:myTierlistDTO`, `toMyTierlistDTO`, `internal/storage/postgres/owners.go:ListByOwner` | Каждая строка содержит `status`, `submissions_count`; дополнительно `items_count`, `visibility`, `created_at` |
| Тест изоляции авторов | `internal/web/api/me_test.go:TestMyTierlistsOnlyOwn` | Проверены `401` без сессии, один свой опрос у Анны, свой у Боба и отсутствие чужих строк |

### R2 — управление своим опросом

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Закрытие и переоткрытие через PATCH | `internal/web/api/api.go:New` (`PATCH /api/v1/tierlists/{slug}`), `internal/web/api/tierlists.go:updateTierlist`, `parseStatus`, `internal/tierlist/service.go:Update`, `changeStatus` | `closed` закрывает, `open`/`public` переоткрывает; статус применяется только после `owned` |
| Прямое закрытие | `internal/web/api/tierlists.go:closeTierlist`, `internal/tierlist/service.go:Close` | Закрытие идемпотентно и сохраняет владельца через `SetStatusForOwner` |
| Удаление | `internal/web/api/tierlists.go:deleteTierlist`, `internal/tierlist/service.go:Delete`, `internal/storage/postgres/owners.go:DeleteForOwner` | Удаляется только строка с совпадающим `author_id`; повторный `GET` даёт `404` |
| Чужой опрос неотличим от несуществующего | `internal/tierlist/service.go:owned`, `internal/storage/postgres/owners.go:SetStatusForOwner`, `DeleteForOwner` | `owned` загружает опрос по ID (или старому slug) и возвращает `ErrNotFound` для чужого; SQL-операции также требуют `author_id` |
| Тесты | `internal/web/api/me_test.go:TestManageOwnedTierlistByID`, `TestPatchRejectsUnknownStatus` | Покрыты закрытие, переоткрытие, удаление, чужой `404`, неизменность чужого опроса и отказ неизвестному статусу |

### R3 — смена пароля закрывает остальные сессии

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Маршрут профиля | `internal/web/api/api.go:New` (`POST /api/v1/me/password`), `internal/web/api/auth.go:changeMyPassword` | Обработчик берёт текущую сессию и передаёт её raw-токен в сервис, чтобы сохранить именно её |
| Смена пароля и удаление остальных сессий | `internal/auth/service.go:ChangePasswordKeepingCurrent`, `PasswordSessions`, `internal/storage/postgres/users.go:UpdatePasswordAndDeleteSessions` | Хэш обновляется и сессии с `token_hash <> keepTokenHash` удаляются в одной транзакции; без транзакционной реализации используется `deleteOtherSessions` и `DeleteOthersByUserID` |
| Текущая сессия работает | `internal/web/api/auth.go:changeMyPassword` | После успеха cookie текущей сессии не очищается, ответ `200` с новым CSRF-токеном; `GET /auth/me` этой сессией продолжает отвечать `200` |
| Старый пароль отклоняется | `internal/auth/service.go:Login`, `ChangePasswordKeepingCurrent` | При первом входе проверяется актуальный хэш; старый пароль даёт `401 invalid_credentials` |
| Слабый пароль | `internal/auth/service.go:ChangePasswordKeepingCurrent`, `internal/web/api/auth.go:changeMyPassword`, `internal/auth/password.go:ValidatePassword` | `ValidatePassword` вызывается до изменения хэша; обработчик переводит ошибку в `400 validation_failed` с причиной и не трогает сессии |
| Тесты | `internal/web/api/auth_test.go:TestMePasswordKeepsCurrentSession`, `internal/auth/session_test.go:TestChangePasswordRevokesSessions` | Покрыты сохранение текущей сессии, отзыв старой, вход новым паролем, отказ старому и `400` на слабый пароль |

### R4 — страница профиля автора

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Маршрут `/profile` только для авторизованных | `web/src/routes.tsx:AppRoutes`, `web/src/features/auth/RequireAuth.tsx:RequireAuth` | `/profile` вложен в `RequireAuth`; при отсутствии пользователя возвращается `<Navigate to="/login" state={{ from: ... }} />` |
| Возврат на запрошенную страницу | `web/src/features/auth/LoginPage.tsx:redirectTarget` | После входа принимается только локальный путь из `location.state.from`; для `/profile` возврат идёт на `/profile` |
| Свои опросы и агрегаты | `web/src/features/profile/ProfilePage.tsx:ProfilePage`, `web/src/features/profile/api.ts:fetchMyTierlists` | Страница вызывает `GET /me/tierlists` и показывает только полученные строки: статус, число ответов и число элементов |
| Действия и подтверждение удаления | `web/src/features/profile/ProfilePage.tsx:statusMutation`, `deleteMutation`, `ConfirmDialog` | «Закрыть»/«Открыть» вызывают `PATCH`, «Удалить» — `DELETE` после диалога подтверждения, затем список инвалидируется |
| Смена пароля из профиля | `web/src/features/profile/ProfilePage.tsx:PasswordForm`, `web/src/features/profile/api.ts:changePassword` | Форма отправляет `POST /me/password` и после успеха показывает, что остальные сессии закрыты |
| Ошибки не выдаются за успех | `web/src/features/profile/ProfilePage.tsx` (`list.isError`, `actionError`, `fb.formError`) | Ошибка загрузки, действия или формы показывается в `role="alert"`, успех — только в `role="status"` после успешного ответа |
| Тесты | `web/src/features/profile/ProfilePage.test.tsx` | Покрыты список с агрегатами, закрытие/переоткрытие, удаление с подтверждением, смена пароля и клиентская валидация |

## Gap

Gap в коде по R1–R4 не найдено: все заявленные требования реализованы и
подтверждены чтением кода и тестами.

Вне вердикта R1–R4 найдены два расхождения контракта, которые не являются
отсутствием заявленного поведения, но должны быть закрыты отдельными задачами:

- **Контракт списка не совпадает с реализацией.** `api/openapi.yaml` для
  `GET /me/tierlists` объявляет параметры `limit` и `cursor`, но
  `internal/web/api/tierlists.go:listMyTierlists` их не читает, а
  `internal/tierlist/service.go:ListMine` возвращает весь список и всегда
  оставляет `next_cursor` пустым.
- **Смена пароля не описана в контракте.** R3 и UI используют
  `POST /api/v1/me/password` (`internal/web/api/api.go:New`,
  `web/src/features/profile/api.ts:changePassword`), но в `api/openapi.yaml`
  такого пути нет.

## Excess

Excess в границах TIERL-53 не найдено:

- `/auth/change-password` и `PATCH /auth/password` закрывают все сессии,
  включая текущую; это поведение относится к capability `accounts` и
  `ADR-003`, а не к R3 из `author-profile`;
- ссылка «Редактировать» в `ProfilePage` следует capability `tierlists`;
- очистка картинок при удалении и правке опроса относится к `item-images`.

Удалять код не требуется.

## Решения

1. **R1, R2, R3, R4 — `ok`, задач на реализацию или удаление кода нет.**
   Поведение сервиса, тесты, контракт и applied-миграции не меняются.
2. **Change на восстановление/уточнение требования создан.** Это
   `openspec/changes/restore-author-profile-audit` с дельтой capability
   `author-profile` (якорь не переименован). В текущем снимке спека уже есть,
   поэтому дельта использует MODIFIED, а не дублирует требования через ADDED;
   R1 явно закрепляет `items_count`, R3 — сохранение текущей сессии и
   неизменность сессий при слабом пароле. Если снимок без канонической спеки,
   эта дельта переносит все четыре требования в capability.
3. **Расхождение контракта по пагинации вынесено в задачу на реализацию.**
   В `tasks.md` change добавлен follow-up: реализовать `limit`/`cursor` для
   `GET /me/tierlists` либо убрать неработающие параметры из контракта. Это не
   gap R1: список своих опросов уже работает.
4. **Отсутствие `/me/password` в контракте вынесено в задачу на реализацию.**
   Нужно описать существующий маршрут в `api/openapi.yaml`; поведение кода при
   этом не меняется.
5. **Excess не найден, задача на удаление кода не создаётся.** Соседние
   маршруты относятся к `accounts`, `tierlists` и `item-images`.

## Проверки

- `go mod download` — зависимости Go загружены.
- `go test ./internal/web/api ./internal/auth ./internal/tierlist -count=1` —
  все три пакета прошли (`ok`); поведение кода не менялось.
- `test -f docs/audits/author-profile.md` и grep-проверка строк `- R1:`…`- R4:`
  по шаблону задачи — пройдены.
- `openspec validate --strict --all --no-interactive` — 14 passed, 0 failed.

## Границы

Код, тесты, контракт и применённые миграции не изменялись. Изменены только
`docs/audits/author-profile.md` и файлы
`openspec/changes/restore-author-profile-audit/`.
