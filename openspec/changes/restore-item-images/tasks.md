# Tasks

## 1. Аудит TIERL-54

- [x] 1.1 Сопоставить R1–R4 с кодом: `internal/web/api/media.go:uploadMedia`, `validateItemImages`, `deleteRemovedImages`, `internal/web/api/tierlists.go:updateTierlist`, `deleteTierlist`, `web/src/features/tierlist-editor/ItemsEditor.tsx:upload`
- [x] 1.2 Подтвердить хранение вне БД, проверку владельца URL и удаление файлов: `internal/media/process.go:Process`, `internal/media/storage.go:Storage`, `migrations/20261022100200_create_items.up.sql`
- [x] 1.3 Записать вердикты `ok` по R1–R4, отсутствие gap и excess в коде и найденный пробел спеки в `docs/audits/item-images.md`

## 2. Восстановление требования

- [x] 2.1 Добавить в capability `item-images` требования «Пределы размеров кадра» (лимиты сторон/пикселей, ответ `413`) и «Нормализация загруженной картинки» (перекодирование без метаданных, уменьшение длинной стороны до 1024 px)
- [x] 2.2 Сохранить существующие требования R1–R4 без переписывания и не переименовывать якорь `item-images`

## 3. Follow-up по контракту и тестам

- [ ] 3.1 Описать `POST /api/v1/media` и `GET /api/v1/media/{key...}` в `api/openapi.yaml`, включая multipart-поле `file`, ответ `{ "url": "..." }` и ошибки `401/413/415/400/503`; код, тесты и applied-миграции не менять
- [ ] 3.2 Добавить регрессионный тест замены `image_url` (прежний файл удалён, новый доступен) и тест ошибок загрузки `413`/`415`/`400` в редакторе; код поведения не менять

## 4. Проверки

- [x] 4.1 Подтвердить, что код, тесты, контракт и applied-миграции не менялись
- [x] 4.2 Проверить `openspec validate --strict --all --no-interactive` без ошибок
