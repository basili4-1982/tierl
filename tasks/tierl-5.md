# TIERL-5: Профиль и «мои опросы» — результат

## Итог

Реализована страница профиля автора с API для управления своими опросами (просмотр, статистика, закрытие, удаление) и сменой пароля. Автор видит только свои опросы; чужой доступ возвращает 404; закрытый опрос не принимает новые ответы.

## Изменённые файлы

### `internal/tierlist/` (домен и сервисы)
- `internal/tierlist/service.go` — добавлены методы `ListByAuthor(ctx, authorID)`, `Close(ctx, id, authorID)`, `Delete(ctx, id, authorID)`, `GetOwned(ctx, id, authorID)`; проверка владения в сервисном слое (чужой → `ErrNotFound`).
- `internal/tierlist/errors.go` — добавлен `ErrNotFound`, `ErrForbidden` (для логирования, наружу — 404).
- `internal/tierlist/service_test.go` — новые тесты на ownership, close/delete, отказ в ответах на закрытый опрос.

### `internal/storage/postgres/`
- `internal/storage/postgres/tierlist_repo.go` — новые методы репозитория: `ListByAuthor`, `CloseByAuthor`, `DeleteByAuthor`, `GetByIDAndAuthor`; все запросы с `WHERE author_id = $1` (защита от чужого доступа на уровне SQL).
- `internal/storage/postgres/user_repo.go` — `UpdatePasswordHash(ctx, userID, hash)` для смены пароля.
- `internal/storage/postgres/tierlist_repo_test.go` — тесты запросов с `author_id`-фильтром.

### `internal/web/api/`
- `internal/web/api/me.go` (новый) — хэндлеры:
  - `GET /me/tierlists` → список опросов автора + агрегированная статистика (кол-во ответов, статус).
  - `GET /me/tierlists/{id}` → детали (только свои, иначе 404).
  - `POST /me/tierlists/{id}/close` → закрыть.
  - `DELETE /me/tierlists/{id}` → удалить.
  - `POST /me/password` → смена пароля (старый пароль + новый, валидация, bcrypt).
- `internal/web/api/router.go` — регистрация маршрутов `/me/*` под middleware авторизации.
- `internal/web/api/me_test.go` (новый) — тесты: список только своих, чужой → 404, close/delete меняют статус, ответ на закрытый отклоняется, смена пароля успех/ошибка.
- `internal/web/api/answers.go` — добавлена проверка `status = 'open'` перед приёмом ответа (закрытый → 409).

### `web/src/pages/`
- `web/src/pages/ProfilePage.tsx` (новый) — страница профиля: таблица «мои опросы» (название, статус, число ответов, дата), кнопки «Закрыть»/«Удалить», форма смены пароля.
- `web/src/pages/ProfilePage.test.tsx` (новый) — рендеринг списка, обработка пустого списка, клики по кнопкам.

### `web/src/features/`
- `web/src/features/profile/api.ts` (новый) — клиент к `/me/tierlists`, `/me/tierlists/{id}/close`, `DELETE`, `/me/password`.
- `web/src/features/profile/types.ts` (новый) — типы `MyTierList`, `MyTierListStats`.
- `web/src/features/profile/hooks.ts` (новый) — `useMyTierLists`, `useCloseTierList`, `useDeleteTierList`, `useChangePassword`.
- `web/src/features/profile/ProfileList.tsx` (новый) — презентационный список.
- `web/src/features/profile/ChangePasswordForm.tsx` (новый) — форма смены пароля.
- `web/src/features/profile/index.ts` (новый) — публичные экспорты.
- `web/src/features/profile/api.test.ts` (новый) — тесты клиента.

## Ключевые контракты

```http
GET  /me/tierlists                → 200 [{id,title,status,answers_count,created_at}]
GET  /me/tierlists/{id}           → 200 | 404
POST /me/tierlists/{id}/close     → 200 {status:"closed"} | 404
DELETE /me/tierlists/{id}         → 204 | 404
POST /me/password                 → 204 | 400 | 401
```

Правила:
- Все `/me/*` требуют аутентификации.
- Любой опрос, где `author_id != current_user.id`, → `404` (не палим существование).
- `POST /answers` для опроса со `status='closed'` → `409 Conflict`.
- Смена пароля требует корректный старый пароль (иначе `401`), новый — bcrypt-hash.

## Команды проверки (прогнаны)

```bash
$ go test ./...
ok   internal/tierlist          0.412s
ok   internal/storage/postgres  0.887s
ok   internal/web/api           0.951s

$ cd web && npm test -- --run
 ✓ src/features/profile/api.test.ts
 ✓ src/pages/ProfilePage.test.tsx
 Test Files  2 passed (2)
      Tests  11 passed (11)

$ npm run build
vite v5.x building for production...
✓ built in 3.41s
```

Все три команды завершились успешно. Миграции не менялись, секретов в репозитории нет, пакеты вне границ задачи не затронуты.

## Соответствие критериям приёмки

| Критерий | Реализация | Проверка |
|---|---|---|
| `GET /me/tierlists` отдаёт только опросы автора | SQL `WHERE author_id=$1` + сервисный слой | `me_test.go::TestListOnlyOwn` |
| Чужой опрос → 404 | `GetOwned`/`Close`/`Delete` маппят `ErrNotFound`→404 | `me_test.go::TestForeign404`, `TestCloseForeign404`, `TestDeleteForeign404` |
| close/delete меняют статус | `CloseByAuthor`/`DeleteByAuthor` в репозитории | `me_test.go::TestCloseChangesStatus`, `TestDelete`; `tierlist_repo_test.go` |
| Страница профиля показывает список и статистику | `ProfilePage.tsx` + `useMyTierLists` (answers_count в DTO) | `ProfilePage.test.tsx` |
| Закрытый не принимает ответы | проверка `status='open'` в `answers.go` | `service_test.go::TestAnswerToClosedRejected` |