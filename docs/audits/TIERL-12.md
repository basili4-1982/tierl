# Аудит TIERL-12 — Загрузка картинок элементов

## Контекст

Задача: TIERL-12 (PR 70) — загрузка картинки элемента: тип и размер, хранение
в S3-совместимом хранилище или локальной FS, публичный URL, удаление файла
вместе с элементом. Заявленные файлы-результат:
`internal/web/api/media.go`, `internal/web/api/api.go`,
`web/src/features/tierlist-editor/api.ts`; файлы-контекста:
`internal/media/storage.go`, `internal/media/process.go`, `internal/tierlist`,
`internal/web/api/api.go`.

Требования: загрузка отклоняет битый и слишком большой файл, байты не лежат в
БД, URL отдаёт картинку, удаление элемента удаляет файл.

Происхождение требования восстанавливается и уже закреплено в OpenSpec:
каноническая capability `item-images`
(`openspec/specs/item-images/spec.md`), `docs/SPEC.md` (раздел 3 — у элемента
опциональная картинка, раздел 8 — S3 или локальная FS) и
`docs/adr/ADR-004-architecture.md` (один процесс отдаёт API и статику,
хранилище картинок вынесено из БД). Отдельный change на восстановление спеки
не требуется: `item-images` уже присутствует в `openspec/specs`.

## Вердикт

- TIERL-12: ok — internal/web/api/media.go:uploadMedia; маршрут `POST /api/v1/media`, проверка типа/размера, хранение FS/S3, отдача по URL, привязка к элементу и удаление файлов при изменении/удалении элемента реализованы, тесты покрывают отказы загрузки и жизненный цикл.

## Доказательства по пунктам описания

| Заявлено | Где реализовано | Чем подтверждается |
|---|---|---|
| Загрузка картинки элемента | `internal/web/api/api.go:166` (`POST /api/v1/media`), `internal/web/api/media.go:66:uploadMedia` | `requireUser` до чтения тела (`media.go:67`), `ParseMultipartForm`, поле `file`, ответ `200 {"url": ...}` (`media.go:123`) |
| Тип файла | `internal/media/process.go:69:Process`, `http.DetectContentType`/`isWebP` (`process.go:80`, `:117`) | PNG, JPEG, WebP, GIF; прочее — `ErrUnsupportedType`, HTTP `415` (`media.go:130`) |
| Размер файла | `internal/media/process.go:58:DefaultLimits` (5 МиБ), проверки `process.go:78`, `media.go:78`, `:95`, `:107` | Слишком большой файл — `413 file_too_large`; лимит применяется и к multipart-телу, и к байтам файла |
| Битый файл | `internal/media/process.go:88`, `:102`, `ErrCorrupt` (`process.go:31`) | `image.DecodeConfig`/`decode` не проходят — HTTP `400 corrupt_image` (`media.go:132`) |
| Гость не загружает | `internal/web/api/media.go:67` (`requireUser`) | `401`, проверено `internal/web/api/media_test.go:113` |
| Байты не в БД | `internal/media/process.go:121:Key` (sha256 + расширение), `internal/web/api/media.go:117` (`users/<id>/<sha256>`) | В миграции `migrations/20261022100200_create_items.up.sql:5` у элемента только `image_url text`; тело файла пишется через `Storage.Put` |
| Хранение FS | `internal/media/fs.go:24:NewFS`, `:40:Put`, `:78:Get`, `:96:Delete`, `:110:URL` | Контракт хранилища `internal/media/fs_test.go:19:TestFS_Contract` (запись, чтение, удаление, URL, защита ключей) |
| Хранение S3 | `internal/media/s3.go:36:NewS3`, `:72:Put`, `:86:Get`, `:105:Delete`, `:116:URL` | Контракт хранилища `internal/media/s3_test.go:70:TestS3_Contract` (в этом окружении пропускается без Docker/MinIO) |
| Выбор FS/S3 из конфигурации | `internal/app/setup.go:138:buildMedia` | `config.StorageFS`/`config.StorageS3`, пустой тип оставляет загрузку выключенной (`503`) |
| URL отдаёт картинку | `internal/web/api/media.go:141:serveMedia`, `:45:MediaHandler`, `internal/web/api/api.go:167` (`GET /api/v1/media/{key...}`) | `internal/web/api/media_test.go:113:TestMediaUploadServeAndDelete` проверяет `200`, `Content-Type: image/*` и непустое тело |
| URL привязан к элементу | `internal/web/api/tierlists.go:246:updateTierlist` → `internal/web/api/media.go:246:validateItemImages`, `internal/tierlist/service.go:474` (`validateImageURL`) | `PATCH /api/v1/tierlists/{slug}` сохраняет `image_url`; `GET /api/v1/tierlists/{slug}` отдаёт его в `itemDTO` (`tierlists.go:76`, `:107`) |
| Принимается только свой URL | `internal/web/api/media.go:233:ownedMediaKey`, `:201:mediaKeyFromURL`, `internal/web/api/tierlists.go:208`, `:266` | URL другого пользователя или внешний адрес при настроенном хранилище — `400 bad_image_url`; проверено `internal/web/api/media_test.go:161:TestMediaItemLifecycle` |
| Удаление файла при удалении элемента | `internal/web/api/tierlists.go:284` → `internal/web/api/media.go:290:deleteRemovedImages`, `:265:deleteImages` | `TestMediaItemLifecycle` удаляет элемент через `PATCH` и проверяет `media.ErrNotFound` у файла |
| Замена картинки удаляет прежний файл | `internal/web/api/media.go:290:deleteRemovedImages` (старый URL не входит в `keep` нового набора) | Тот же путь, что и для удалённого элемента; отдельного теста именно на замену нет |
| Удаление опроса удаляет все файлы | `internal/web/api/tierlists.go:302:deleteTierlist` → `internal/web/api/media.go:265:deleteImages` | `TestMediaItemLifecycle` удаляет опрос и проверяет отсутствие файла |
| Редактор: выбрать файл, показать, убрать | `web/src/features/tierlist-editor/api.ts:39:uploadMedia`, `web/src/features/tierlist-editor/ItemsEditor.tsx:34:ItemsEditor`, `:48:upload`, `:130` (`type="file"`), `:140` (`<img>`), `:143` (удаление) | `model.ts:59` отправляет `image_url` при сохранении; `ItemsEditor` передаёт загруженный URL в `DraftItem.imageUrl` |
| Ошибка загрузки видна автору | `web/src/features/tierlist-editor/ItemsEditor.tsx:14:uploadMessage`, `:60` (`uploadError`), `:70` (`role="alert"`) | Коды `413`/`415`/`400`/`401`/`503` имеют отдельные сообщения |

## Gap

Gap в коде по TIERL-12 не найдено: обработчик, маршруты, сервисная проверка,
хранилища FS/S3, привязка URL к элементу, удаление файлов и UI редактора
присутствуют. Поведение соответствует сценариям
`openspec/specs/item-images/spec.md`.

## Excess

Excess в границах TIERL-12 не найдено. `media.Process`/`Key`, FS и S3, а также
поля `image_url` следуют из `openspec/specs/item-images/spec.md` и
`docs/SPEC.md` (разделы 3 и 8). Совместимые alias-имена
(`uploadImage`/`uploadItemImage` в `web/src/features/tierlist-editor/api.ts:58`,
`ImageStorage`/`MediaStore` в `internal/web/api/api.go:79`) не добавляют
отдельного поведения, поэтому задача на удаление кода не нужна.

## Замечания вне вердикта

Это не gap по определению аудита (код заявленного есть), а расхождения
документации/контракта и тестового покрытия:

- `api/openapi.yaml` не описывает `POST /api/v1/media` и `GET /api/v1/media/{key}`,
  хотя маршруты зарегистрированы в `internal/web/api/api.go:166-167`; фронт
  вызывает их вручную через `uploadMedia`.
- `.env.example:51` всё ещё утверждает, что HTTP-загрузка картинок «ещё не
  подключена», хотя `internal/app/setup.go:138:buildMedia` и маршруты её
  включают.
- Нет отдельного теста на замену картинки и на файловую загрузку через UI;
  удаление/замена используют один путь `deleteRemovedImages`, а UI-компонент
  реализован, но покрыт только тестами URL-валидации.

## Решения

1. **Вердикт `ok`, change на восстановление не создаётся.** Требование не
   исчезало из OpenSpec: capability `item-images` уже есть в
   `openspec/specs/item-images/spec.md`; код ей соответствует.
2. **Gap в коде: нет.** Заявленные обработчик, маршрут, UI и проверки
   реализованы; задачи на реализацию отсутствующего кода не создаются.
3. **Excess: нет.** Задача на удаление кода не создаётся; alias-имена не
   являются отдельной возможностью.
4. **Способность названа по существующему якорю.** Работа относится к
   `item-images`; новое имя не вводится.
5. **Документационные и тестовые замечания вынесены отдельно.** Они не
   меняют вердикт `ok` и не оформляются как восстановление требования;
   контракт, `.env.example` и тесты в границах этого аудита не меняются.
   Эскалация человеку не требуется.

## Проверки

- `go test ./internal/media ./internal/web/api ./internal/tierlist ./internal/app`
  — все пакеты `ok`; S3-тесты с testcontainers в этом окружении пропущены без
  Docker/MinIO, FS-контракт выполнен.
- `openspec validate --strict --all --no-interactive` — 11 passed, 0 failed.

## Границы

Код, тесты, контракт и применённые миграции не изменялись. Изменён только
`docs/audits/TIERL-12.md`.
