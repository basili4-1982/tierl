# Tasks

## 1. Происхождение работы

- [x] 1.1 Сопоставить TIERL-35 с `docs/SPEC.md` §1–§3 и §11, `api/openapi.yaml` (`POST /tierlists`, `GET/PATCH/DELETE /tierlists/{slug}`, `POST /tierlists/{slug}/close`) и `docs/adr/ADR-004-architecture.md`; результат записан в `proposal.md` и `design.md`
- [x] 1.2 Проверить реализацию: `internal/domain/tierlist.go`, `internal/domain/slug.go`, `internal/tierlist/service.go`, `internal/tierlist/store.go`, `internal/storage/postgres/tierlists.go`, `internal/storage/postgres/owners.go`, `internal/web/api/tierlists.go`, `internal/web/api/api.go` и UI редактора/профиля
- [x] 1.3 Классифицировать работу: R1, R2 и R3 реализованы, избыточного кода не найдено; найденные замечания по контракту и `closed_at` вынесены в раздел 3

## 2. Восстановление спеки

- [x] 2.1 Добавить capability `tierlists` с требованием создания опроса автором: тиры и элементы, границы, уникальные позиции и сохранение структуры
- [x] 2.2 Добавить требование публикации: видимость `public`/`unlisted`, обязательный уникальный slug для public и случайный slug не короче 12 символов для unlisted
- [x] 2.3 Добавить требование редактирования: название, описание и видимость всегда, тиры и элементы — до первого ответа
- [x] 2.4 Добавить требование жизненного цикла: закрытие, переоткрытие и удаление владельцем; отметить пересечение с `author-profile`
- [x] 2.5 Связать требования с существующими маршрутами `POST /api/v1/tierlists`, `GET/PATCH/DELETE /api/v1/tierlists/{slug}`, `POST /api/v1/tierlists/{slug}/close` и `POST /api/v1/tierlists/{slug}/submissions` (закрытый опрос не принимает ответы)

## 3. Follow-up по замечаниям

- [x] 3.1 Описать `status` (`open`/`closed`, а также авторский синоним `public` для переоткрытия) в `TierlistPatch` контракта `api/openapi.yaml`; проверка — `PATCH /tierlists/{slug}` с `status` соответствует схеме и `openspec/specs/author-profile/spec.md` — вынесено в задачу трекера TIERL-37 — вынесено в задачу трекера TIERL-37
- [x] 3.2 Сделать `closed_at` стабильным: хранить отдельное время закрытия или не перезаписывать его при правке текста; проверка — правка названия закрытого опроса не меняет `closed_at` — вынесено в задачу трекера TIERL-37 — вынесено в задачу трекера TIERL-37
- [x] 3.3 Добавить регрессионный тест на стабильный `closed_at` после закрытия и последующей правки названия/описания — вынесено в задачу трекера TIERL-37
- [x] 3.4 Проверить `go test ./internal/domain ./internal/tierlist ./internal/web/api` после follow-up-изменений — вынесено в задачу трекера TIERL-37

## 4. Проверки аудита

- [x] 4.1 Записать вердикты `ok` для R1, R2 и R3 с ссылками на код в `docs/audits/tierlists.md`
- [x] 4.2 Проверить `go test ./internal/domain ./internal/tierlist ./internal/web/api`
- [x] 4.3 Проверить `openspec validate --strict --all --no-interactive` без ошибок

## Workflow follow-up

- После ревью и реализации follow-up-задач архивировать изменение, чтобы
  capability `tierlists` попала в `openspec/specs/tierlists/spec.md`.
