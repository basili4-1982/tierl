# Аудит TIERL-35 — Создание и редактирование опросов

## Контекст

Задача: TIERL-35 — аудит способности «Создание и редактирование опросов»
(якорь цели `tierlists`). Требования аудита: R1 — автор создаёт опрос с тирами
и элементами; R2 — публикация с видимостью и slug; R3 — закрытие,
переоткрытие и удаление.

Происхождение требования восстанавливается по `docs/SPEC.md` §1 (автор создаёт
опрос, публикует его), §2 (создавать опросы может пользователь с подтверждённым
email), §3 (поля опроса, видимость `public`/`unlisted`, статусы, границы
элементов и тиров, запрет правки состава после первого ответа), §11 (путь
автора от регистрации до шеринга), а также по `api/openapi.yaml` (`POST
/tierlists`, `GET/PATCH/DELETE /tierlists/{slug}`, `POST
/tierlists/{slug}/close`) и `docs/adr/ADR-004-architecture.md`.

Канонической capability `tierlists` в OpenSpec не было: живая спека
`openspec/specs/author-profile/spec.md` покрывает только управление своими
опросами из профиля (закрытие, переоткрытие, удаление), а создания,
редактирования и публикации в ней нет. Поэтому оформлен change
`restore-tierlists` с capability `tierlists`; якорь не переименовывается.

## Вердикт

- R1: ok — internal/tierlist/service.go:Create.Implements создание опроса: автор с подтверждённым email передаёт название, описание, видимость, тиры и элементы, домен проверяет границы и позиции, а хранилище сохраняет опрос вместе со структурой.
- R2: ok — internal/tierlist/service.go:Create.Publishes опрос с видимостью `public`/`unlisted`: для public автор задаёт уникальный slug, для unlisted сервер генерирует криптостойкий slug не короче 12 символов и игнорирует переданный.
- R3: ok — internal/tierlist/service.go:Close.Closes опрос идемпотентно; переоткрытие идёт через `PATCH /tierlists/{slug}` со статусом `open`/`public`, удаление — через `DELETE /tierlists/{slug}` с каскадным удалением ответов; владелец проверяется, чужой опрос даёт `404`.

## Доказательства по требованиям

### R1 — автор создаёт опрос: тиры и элементы

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Маршрут создания | `internal/web/api/api.go:New` (`POST /api/v1/tierlists`), `internal/web/api/tierlists.go:createTierlist` | Обработчик требует пользователя, разбирает `createRequest`, проверяет картинки и вызывает `Service.Create` |
| Автор и подтверждённый email | `internal/tierlist/service.go:Create` (`s.users.Profile`, `ErrEmailNotVerified`), `internal/web/api/api.go:writeServiceError` (`403 email_not_verified`) | Создание запрещено без подтверждённого email; `docs/SPEC.md` §2 |
| Тиры и элементы | `internal/tierlist/service.go:buildStructure`, `internal/domain/tierlist.go:ValidateStructure`, `internal/domain/tierlist.go:NewTierlist` | Из входных тиров/элементов строятся позиции по индексу, проверяются количество, длины, цвет и уникальность позиций |
| Границы из SPEC §3 | `internal/domain/tierlist.go` (`MinItems = 3`, `MaxItems = 50`, `MinTiers = 2`, `MaxTiers = 10`, `MaxTitleLen = 120`, `MaxDescriptionLen = 1000`, `MaxItemLabelLen = 80`) | Константы домена — источник истины для границ; `internal/domain/domain_test.go:TestNewTierlist` проверяет 2/50/51 элемент и 1/10/11 тиров |
| Сохранение структуры | `internal/storage/postgres/tierlists.go:Create`, `internal/storage/postgres/tierlists.go:insertStructure`, `migrations/20261022100100_create_tiers.up.sql`, `migrations/20261022100200_create_items.up.sql` | Опрос, тиры и элементы вставляются в одной транзакции; позиции и внешние ключи закреплены схемой |
| UI создания | `web/src/features/tierlist-editor/TierlistEditorPage.tsx:onSubmit`, `web/src/features/tierlist-editor/model.ts:toStructure`, `web/src/features/tierlist-editor/ItemsEditor.tsx`, `web/src/features/tierlist-editor/TiersEditor.tsx` | Форма отправляет тиры и элементы в `POST /tierlists`; тесты создания и редактирования структуры в `TierlistEditorPage.test.tsx` |
| Тесты | `internal/web/api/api_test.go:TestCreateAndGet`, `internal/web/api/api_test.go:TestCreate_Errors`, `internal/tierlist/service_test.go:TestCreate`, `internal/tierlist/service_test.go:TestCreate_Errors` | Покрыты успешное создание, отсутствие авторизации, неподтверждённый email, неверные границы и ошибки валидации |

### R2 — публикация: видимость, slug

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Публикация при создании | `internal/tierlist/service.go:Create` (`t.Publish(s.now())`), `internal/domain/tierlist.go:Publish` | Созданный через API опрос сразу переходит из `draft` в `open` и доступен по правилам видимости |
| Видимость `public`/`unlisted` | `internal/domain/tierlist.go:Visibility`, `Visibility.Valid`, `internal/web/api/tierlists.go:createRequest`, `internal/web/api/tierlists.go:updateTierlist` | Принимаются только два значения; неверное значение — `422 validation_failed`; смена видимости разрешена автору |
| Slug для public | `internal/domain/slug.go:ValidateSlug`, `internal/storage/postgres/tierlists.go:mapWriteErr`, `internal/web/api/api.go:writeServiceError` (`409 slug_taken`), `migrations/20261022100000_create_tierlists.up.sql` (`tierlists_slug_key UNIQUE`) | Public-опрос требует slug 3..64 из `[A-Za-z0-9_-]`; коллизия отклоняется `409 slug_taken` |
| Slug для unlisted | `internal/domain/tierlist.go:NewTierlist`, `internal/domain/slug.go:GenerateSlug`, `internal/domain/slug.go:MinUnlistedSlugLength` (12), `migrations/20261022100000_create_tierlists.up.sql` (`tierlists_unlisted_slug_check`) | Переданный slug для unlisted игнорируется; генерируется 24 символа из `crypto/rand.Reader`; БД запрещает unlisted slug короче 12 |
| Смена видимости | `internal/tierlist/service.go:changeVisibility` | `public` -> `unlisted` заменяет короткий slug на случайный; `unlisted` -> `public` сохраняет длинный slug; повторная установка той же видимости — no-op |
| Публичная выдача | `internal/tierlist/service.go:readable`, `internal/storage/postgres/catalog.go:ListPublic` | Открытые и закрытые опросы доступны по slug; `draft` и `hidden` дают `404`; в каталог попадают только `public`+`open` (это также требование `catalog`) |
| UI публикации | `web/src/features/tierlist-editor/TierlistEditorPage.tsx` (выбор видимости, поле адреса, подсказка для unlisted), `web/src/features/tierlist-editor/model.ts:emptyDraft` | Для public показано обязательное поле slug, для unlisted — сообщение об автоматической генерации |
| Тесты | `internal/tierlist/service_test.go:TestCreate_UnlistedGetsRandomSlug`, `internal/tierlist/service_test.go:TestCreate_SlugTaken`, `internal/tierlist/service_test.go:TestUpdate_TextAndVisibility`, `internal/web/api/api_test.go:TestCreate_Unlisted`, `internal/web/api/api_test.go:TestCreate_SlugTaken`, `web/src/features/tierlist-editor/TierlistEditorPage.test.tsx` | Покрыты случайный slug, занятый slug, переходы видимости и клиентская валидация |

### R3 — закрытие, переоткрытие, удаление

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Закрытие | `internal/web/api/api.go:New` (`POST /tierlists/{slug}/close`), `internal/web/api/tierlists.go:closeTierlist`, `internal/tierlist/service.go:Close`, `internal/tierlist/service.go:changeStatus` | Закрытие идемпотентно, закрытый опрос остаётся читаемым, но новые ответы не принимаются (`AcceptsSubmissions`) |
| Закрытие через PATCH | `internal/web/api/tierlists.go:parseStatus`, `internal/web/api/tierlists.go:updateTierlist`, `internal/tierlist/service.go:Update` | `PATCH` со статусом `closed` закрывает опрос; неверный статус — `400 bad_request` |
| Переоткрытие | `internal/tierlist/service.go:changeStatus` (`closed`/`draft` -> `open`), `internal/web/api/tierlists.go:parseStatus` (`open`/`public`), `internal/tierlist/service.go:Update` | Переоткрытие доступно владельцу через `PATCH`; `public` — авторское имя состояния «снова открыт», как требует `openspec/specs/author-profile/spec.md` |
| Удаление | `internal/web/api/api.go:New` (`DELETE /tierlists/{slug}`), `internal/web/api/tierlists.go:deleteTierlist`, `internal/tierlist/service.go:Delete`, `internal/storage/postgres/tierlists.go:Delete` | Удаление владельцем; повторное удаление и `GET` дают `404`; ответы, тиры и элементы удаляются каскадом (`ON DELETE CASCADE` в миграциях) |
| Проверка владельца | `internal/tierlist/service.go:owned`, `internal/storage/postgres/owners.go:SetStatusForOwner`, `internal/storage/postgres/owners.go:DeleteForOwner` | Гость получает `403 forbidden`, чужой опрос — `404 not_found`; чужой опрос не меняется |
| UI управления | `web/src/features/profile/ProfilePage.tsx` (кнопки «Закрыть»/«Открыть»/«Удалить»), `web/src/features/profile/api.ts:setTierlistStatus`, `web/src/features/profile/api.ts:deleteTierlist` | Профиль вызывает `PATCH /tierlists/{id}` и `DELETE /tierlists/{id}`; удаление требует подтверждения |
| Тесты | `internal/tierlist/service_test.go:TestClose`, `internal/tierlist/service_test.go:TestDelete`, `internal/web/api/api_test.go:TestClose`, `internal/web/api/api_test.go:TestDelete`, `internal/web/api/me_test.go:TestManageOwnedTierlistByID`, `web/src/features/profile/ProfilePage.test.tsx` | Покрыты идемпотентное закрытие, переоткрытие, удаление, чужой `404` и UI-сценарии |

### Редактирование после создания

Хотя R1–R3 явно не перечисляют правку, способность называется «Создание и
редактирование опросов», поэтому проверено и она:

- текст и видимость меняются всегда: `internal/tierlist/service.go:Update`,
  `internal/domain/tierlist.go:UpdateText`;
- состав тиров и элементов меняется только до первого ответа:
  `internal/tierlist/service.go:changeStructure`,
  `internal/domain/tierlist.go:ReplaceStructure` (`ErrStructureLocked`),
  HTTP `409 tierlist_locked` в `internal/web/api/api.go:writeServiceError`;
- хранилище повторно проверяет отсутствие ответов под блокировкой строки:
  `internal/storage/postgres/tierlists.go:Update`;
- тесты: `internal/tierlist/service_test.go:TestUpdate_Structure`,
  `internal/web/api/api_test.go:TestPatch`, `internal/web/api/api_test.go:TestPatch_LockedAfterSubmission`,
  `web/src/features/tierlist-editor/TierlistEditorPage.test.tsx` (edit/locked).

## Gap

Gap в коде по R1–R3 не найдено: создание с тирами и элементами, публикация с
видимостью и slug, закрытие, переоткрытие и удаление присутствуют и покрыты
тестами. Требование отсутствовало именно в OpenSpec, а не в реализации.

## Excess

Excess в границах TIERL-35 не найдено:

- `Hide`/`hidden` и порог жалоб (`internal/domain/tierlist.go:Hide`,
  `internal/tierlist/reports.go`) следуют требованию `reports`, а не
  `tierlists`;
- `ListPublic` (`internal/storage/postgres/catalog.go`) следует требованию
  `catalog`;
- поля `image_url` следуют capability `item-images`;
- приём статуса `public` как синонима переоткрытия закреплён сценарием в
  `openspec/specs/author-profile/spec.md`, поэтому это не код без требования.

Удалять код не требуется.

## Замечания вне вердикта

Это не gap по определению аудита (реализация заявленного есть), но
расхождения, которые стоит закрыть отдельными задачами:

1. **Контракт не описывает смену статуса.** `TierlistPatch` в
   `api/openapi.yaml` содержит `title`, `description`, `visibility`, `tiers`,
   `items`, но не `status`, хотя `PATCH /tierlists/{slug}` принимает
   `status` (`internal/web/api/tierlists.go:patchRequest`) и переоткрытие
   требует `openspec/specs/author-profile/spec.md`.
2. **`closed_at` может смещаться при правке текста.** `Details.ClosedAt`
   (`internal/tierlist/service.go:ClosedAt`) берёт `UpdatedAt`, а
   `UpdateText` (`internal/domain/tierlist.go:UpdateText`) обновляет
   `UpdatedAt`; правка названия или описания уже закрытого опроса меняет
   `closed_at`, хотя момент закрытия не изменился.

## Решения

1. **Требование было и исчезло из OpenSpec — восстановить.** Создан change
   `restore-tierlists` с capability `tierlists` (якорь цели не
   переименован): создание опроса с тирами и элементами, публикация с
   видимостью и slug, редактирование и жизненный цикл. Поведение кода не
   меняется.
2. **Gap в коде: нет.** R1, R2 и R3 реализованы; задачи на реализацию
   отсутствующего кода не создаются.
3. **Excess: нет.** Задача на удаление кода не создаётся; найденные соседние
   возможности принадлежат `reports`, `catalog` и `item-images`.
4. **R3 пересекается с `author-profile`.** Живая спека `author-profile` уже
   содержит закрытие, переоткрытие и удаление. В `tierlists` они
   восстанавливаются как часть целостного жизненного цикла способности;
   поведение и владение маршрутами не дублируются и не меняются.
5. **Замечания вне вердикта вынесены в задачи.** В
   `openspec/changes/restore-tierlists/tasks.md` добавлены follow-up на
   описание `status` в контракте и на стабильный `closed_at` с регрессионным
   тестом. Код, контракт и тесты в границах этого аудита не меняются.
6. **Эскалация человеку не требуется:** происхождение требования
   восстанавливается по `docs/SPEC.md`, `api/openapi.yaml` и ADR-004.

## Проверки

- `go test ./internal/domain ./internal/tierlist ./internal/web/api` — все
  пакеты прошли (`ok`), поведение кода не менялось.
- `openspec validate --strict --all --no-interactive` — без ошибок.

## Границы

Код, тесты, контракт и применённые миграции не изменялись. Изменены только
`docs/audits/tierlists.md` и файлы
`openspec/changes/restore-tierlists/`.
