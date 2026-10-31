# Аудит TIERL-54 — Картинки элементов

## Контекст

Задача: TIERL-54 — аудит способности «Картинки элементов» (якорь цели
`item-images`). Требования аудита: R1 — загрузка картинки элемента; R2 —
картинка привязана к элементу; R3 — удаление файла вместе с элементом; R4 —
редактор работает с картинками.

В отличие от формулировки задачи, живая capability `item-images` в workspace
уже существует: `openspec/specs/item-images/spec.md` восстановлен архивным
change `2026-10-27-add-item-images`. Поэтому аудит сверяет код с живой спекой,
а также с `docs/SPEC.md` §3 (у элемента опциональная картинка) и §8
(хранилище картинок — S3-совместимое или локальная FS), ADR-004 и
`api/openapi.yaml`. Якорь `item-images` не переименовывается.

Что проверялось:

- маршруты `POST /api/v1/media`, `GET /api/v1/media/{key...}`, привязка
  `image_url` через `POST/PATCH /api/v1/tierlists/{slug}`;
- проверка типа, размера и декодирования файла, хранение вне БД;
- проверка владельца URL, выдача URL в элементе и удаление файлов при
  удалении/замене элемента и удалении опроса;
- редактор: выбор файла, превью, удаление картинки, ошибки загрузки;
- тесты API, хранилищ, обработки изображений и редактора.

## Вердикт

- R1: ok — internal/web/api/media.go:uploadMedia — маршрут `POST /api/v1/media` требует авторизацию, проверяет тип/размер/декодирование, сохраняет байты в FS или S3 и отвечает `200` с `url`; в БД пишется только `image_url`.
- R2: ok — internal/web/api/media.go:validateItemImages — `PATCH /api/v1/tierlists/{slug}` принимает `image_url`, проверяет, что URL выдан загрузкой этому же пользователю, сохраняет URL в элементе, а `GET` возвращает его; чужой/внешний URL даёт `400` до изменения опроса.
- R3: ok — internal/web/api/media.go:deleteRemovedImages — после `PATCH` удаляются файлы исчезнувших элементов и заменённых картинок, при `DELETE /api/v1/tierlists/{slug}` удаляются файлы всех элементов; сбой удаления только логируется.
- R4: ok — web/src/features/tierlist-editor/ItemsEditor.tsx:upload — редактор даёт выбрать файл, загружает его через `POST /media`, показывает превью и позволяет убрать картинку; ошибки `413`/`415`/`400` показываются автору, а сохранение отправляет `image_url`.

## Доказательства по требованиям

### R1 — Загрузка картинки элемента

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Маршрут загрузки и авторизация | `internal/web/api/api.go:168` (`POST /api/v1/media`), `internal/web/api/media.go:uploadMedia` (`requireUser`) | Без сессии обработчик возвращает `401` до чтения файла; проверено `internal/web/api/media_test.go:TestMediaUploadServeAndDelete` |
| Приём multipart-файла | `internal/web/api/media.go:uploadMedia` (`ParseMultipartForm`, `FormFile("file")`) | Поле называется `file`; лимит тела ограничен `MaxBytesReader`; ответ — `200` и JSON `{"url": ...}` |
| Тип по содержимому | `internal/media/process.go:Process` (`http.DetectContentType`, `isWebP`) | Принимаются PNG, JPEG, WebP и GIF независимо от имени файла; прочее — `media.ErrUnsupportedType` |
| Лимит размера | `internal/media/process.go:DefaultLimits` (5 МиБ), `internal/media/process.go:Process`, `internal/web/api/media.go:writeMediaError` | Слишком большой файл — `413 file_too_large`; объект не создаётся, потому что `Put` вызывается только после успешного `Process`; проверено `TestMediaUploadTooLarge` |
| Битый файл | `internal/media/process.go:Process` (`image.DecodeConfig`, `decode`), `internal/web/api/media.go:writeMediaError` | Ошибки декодирования дают `400 corrupt_image`; проверено `internal/web/api/media_test.go:TestMediaUploadServeAndDelete` и `internal/media/process_test.go:TestProcess_Rejects` |
| Хранение вне БД | `internal/media/storage.go:Storage`, `internal/media/fs.go:Put`, `internal/media/s3.go:Put`, `internal/web/api/media.go:uploadMedia` (`users/<id>/<sha256>.<ext>`) | Байты пишутся в `media.Storage`; в `migrations/20261022100200_create_items.up.sql` у элемента только `image_url text` |
| Отдача по URL | `internal/web/api/media.go:serveMedia`, `internal/web/api/api.go:169` (`GET /api/v1/media/{key...}`), `internal/media/fs.go:URL`, `internal/media/s3.go:URL` | Для FS файл отдаётся приложением с `Content-Type: image/*`; для S3/CDN URL ведёт в объектное хранилище; проверено `TestMediaUploadServeAndDelete` |
| Выбор бэкенда и выключенное хранилище | `internal/app/setup.go:buildMedia` | `TIERL_STORAGE_TYPE=fs` или `s3` выбирает реализацию; пустой тип оставляет загрузку выключенной и даёт `503 media_unavailable` |

### R2 — Картинка привязана к элементу

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Поле `image_url` в контракте элемента | `internal/web/api/tierlists.go:itemInputDTO`, `internal/web/api/tierlists.go:itemDTO`, `internal/web/api/tierlists.go:toTierlistDTO` | Входной DTO принимает `image_url`, выходной возвращает его у элемента; `GET` отдаёт сохранённый URL |
| Сохранение при создании и правке | `internal/web/api/tierlists.go:createTierlist`, `internal/web/api/tierlists.go:updateTierlist`, `internal/web/api/tierlists.go:toItemInputs` | URL передаётся в сервис и сохраняется в элементе как при `POST`, так и при `PATCH` |
| Проверка владельца загрузки | `internal/web/api/media.go:validateItemImages`, `internal/web/api/media.go:ownedMediaKey`, `internal/web/api/media.go:mediaKeyFromURL` | При настроенном хранилище принимается только ключ с префиксом `users/<текущий пользователь>/`; иначе `400 bad_image_url` |
| Отказ до изменения опроса | `internal/web/api/tierlists.go:updateTierlist` (проверка до `Service.Update`) | При чужом URL обработчик возвращает ошибку до вызова сервиса, поэтому элемент не меняется; проверено `internal/web/api/media_test.go:TestMediaItemLifecycle` |
| Синтаксис URL и предел длины | `internal/tierlist/service.go:validateImageURL`, `internal/domain/tierlist.go:Item.validate` (`MaxImageURLLen = 2048`), `migrations/20261022100200_create_items.up.sql` (`items_image_url_check`) | Допускается пустое значение, абсолютный `http(s)` или корневой путь; длина ограничена и в домене, и в БД |

### R3 — Удаление файла вместе с элементом

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Удаление элемента | `internal/web/api/tierlists.go:updateTierlist`, `internal/web/api/media.go:deleteRemovedImages` | Перед правкой читаются старые элементы, после успешного обновления удаляются URL, которых нет в новом составе; проверено `TestMediaItemLifecycle` |
| Замена картинки | `internal/web/api/media.go:deleteRemovedImages` (старый URL не входит в `keep`), `internal/web/api/media.go:deleteImages` | Заменённый URL удаляется тем же путём, что и картинка исчезнувшего элемента |
| Удаление опроса | `internal/web/api/tierlists.go:deleteTierlist`, `internal/web/api/media.go:itemImageURLs`, `internal/web/api/media.go:deleteImages` | До удаления опроса собираются URL всех элементов, после успешного удаления файлы удаляются; проверено `TestMediaItemLifecycle` |
| Сбой удаления не отменяет изменение | `internal/web/api/media.go:deleteImages` (`WarnContext`) | Ошибка хранилища логируется, но HTTP-ответ на уже сохранённое изменение опроса не меняется |
| Удаление файла в FS и S3 | `internal/media/fs.go:Delete`, `internal/media/s3.go:Delete` | Оба хранилища считают удаление отсутствующего объекта успешным и не оставляют файл после удаления существующего |

### R4 — Редактор работает с картинками

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Выбор файла | `web/src/features/tierlist-editor/ItemsEditor.tsx:ItemsEditor` (`type="file"`, `accept`) | Поле файла есть у каждого элемента; оно блокируется на время загрузки |
| Загрузка через API | `web/src/features/tierlist-editor/ItemsEditor.tsx:upload`, `web/src/features/tierlist-editor/api.ts:uploadMedia` | Файл отправляется в `POST /media` с CSRF-токеном, полученный URL записывается в `DraftItem.imageUrl` |
| Показ загруженной картинки | `web/src/features/tierlist-editor/ItemsEditor.tsx:ItemsEditor` (`<img>`), `web/src/features/tierlist-editor/Preview.tsx` | Превью показывается в редакторе и в предпросмотре |
| Удаление картинки | `web/src/features/tierlist-editor/ItemsEditor.tsx:ItemsEditor` (кнопка «Удалить картинку») | Кнопка очищает `imageUrl`; сохранение без URL отправляет элемент без `image_url` |
| Сохранение `image_url` | `web/src/features/tierlist-editor/model.ts:toStructure` | Непустой URL уходит в `PATCH /tierlists/{slug}` как `image_url` элемента |
| Ошибки загрузки | `web/src/features/tierlist-editor/ItemsEditor.tsx:uploadMessage`, `uploadError` (`role="alert"`) | Для `413`, `415`, `400`, `401` и `503` есть отдельные сообщения; картинка не привязывается |
| Тесты редактора | `web/src/features/tierlist-editor/TierlistEditorPage.test.tsx` («shows tier and image url errors inline») | Покрыта ошибка URL; сценарии выбора файла и ошибок загрузки покрыты не полностью (см. замечания) |

## Gap

Gap в коде по R1–R4 не найден: загрузка, проверки типа/размера/декодирования,
хранение вне БД, привязка URL к элементу, удаление файлов и работа редактора
реализованы и в основном покрыты тестами.

Найден **gap спеки**, а не реализации: живая `item-images` говорит про
«превышение лимита», но не фиксирует лимиты сторон и пикселей, которые
реально применяет код (`MaxSide = 8000`, `MaxPixels = 40 000 000`), а также
перекодирование, уменьшение длинной стороны до 1024 px и удаление метаданных
(`internal/media/process.go:DefaultLimits`, `Process`, `downscale`;
подтверждено `internal/media/process_test.go:TestProcess_Downscale` и
`TestProcess_StripsEXIF`). Из-за этого часть наблюдаемого поведения R1 не
имеет нормативного описания в живой спеке.

Дополнительно зафиксированы документационное и тестовое расхождения, не
меняющие вердикт R1–R4:

- `api/openapi.yaml` не описывает `POST /api/v1/media` и
  `GET /api/v1/media/{key...}`, хотя маршруты зарегистрированы в
  `internal/web/api/api.go:168-169`.
- Нет отдельного теста на замену картинки и на загрузку файла через UI;
  удаление и замена используют один путь `deleteRemovedImages`, а UI-компонент
  покрыт только тестом ошибки URL.

## Excess

Excess в границах R1–R4 не найден. Поля `image_url`, FS/S3-хранилища,
перекодирование и проверка URL следуют `item-images`, `docs/SPEC.md` §3/§8 и
ADR-004. Поддержка `image_url` при создании опроса — тот же сценарий R2, а не
новая возможность. Совместимые alias-имена (`ImageStorage`/`MediaStore` в
`internal/web/api/api.go`, `uploadImage`/`uploadItemImage` в
`web/src/features/tierlist-editor/api.ts`) не добавляют отдельного поведения,
поэтому задача на удаление кода не создаётся.

## Замечания вне вердикта

- Если хранилище не настроено (`a.media == nil`), `validateItemImages`
  пропускает проверку владельца URL, а `POST /media` отвечает `503`. В
  штатной конфигурации (`TIERL_STORAGE_TYPE=fs` по умолчанию) проверка
  работает; ужесточение поведения без хранилища можно рассмотреть отдельно,
  не меняя текущий вердикт.
- `api/openapi.yaml` и тестовое покрытие редактора отстают от реализации;
  это вынесено в follow-up задачи change, а не в удаление или переписывание
  кода.

## Решения

1. **Gap в коде отсутствует.** R1, R2, R3 и R4 реализованы; задачи на
   реализацию отсутствующего кода не создаются.
2. **Gap спеки → change на восстановление требования.** Создан change
   `restore-item-images` с capability `item-images` (якорь не
   переименован). В `specs/item-images/spec.md` добавлены требования
   «Пределы размеров кадра» и «Нормализация загруженной картинки»: лимиты
   сторон/пикселей, `413` без создания объекта, перекодирование без метаданных
   и уменьшение длинной стороны до 1024 px. Существующие R1–R4 не
   переписываются.
3. **Excess отсутствует.** Задача на удаление кода не создаётся; соседние
   элементы принадлежат `item-images`, `tierlists` и `sharing`, а не
   избыточному коду.
4. **Документационные и тестовые расхождения вынесены в задачи.** В
   `restore-item-images/tasks.md` добавлены follow-up на описание медиа-маршрутов
   в OpenAPI и на регрессионные тесты замены картинки и ошибок загрузки в UI;
   код, тесты, контракт и applied-миграции в границах аудита не меняются.
5. **Способность названа по якорю `item-images`.** Новое имя не вводится:
   загрузка, привязка, удаление и редактор — части одной цели «Картинки
   элементов».
6. **Эскалация человеку не требуется:** происхождение требования
   восстанавливается по `docs/SPEC.md` §3/§8, ADR-004 и живой спеке.

## Проверки

- `f=docs/audits/item-images.md; test -f "$f"` и grep по строкам вердикта
  R1–R4 — файл и все четыре строки присутствуют.
- `go mod download` — зависимости Go загружены; `go test
  ./internal/media ./internal/web/api ./internal/tierlist` — пакеты прошли,
  поведение кода не менялось.
- `openspec validate --strict --all --no-interactive` — без ошибок.

## Границы

Код, тесты, контракт и применённые миграции не изменялись. Изменены только
`docs/audits/item-images.md` и файлы
`openspec/changes/restore-item-images/`.
