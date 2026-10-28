# Proposal

## Why

TIERL-6 заявляет каталог `public`+`open` опросов с пагинацией, сортировкой,
поиском и исключением `unlisted` из каталога, sitemap и поиска, но в OpenSpec
нет capability `catalog`: требование осталось только в `docs/SPEC.md`
(разделы 7 и 11), `api/openapi.yaml` (`/catalog`) и `ADR-004`. При этом
заявленные артефакты реализованы не полностью: нет
`internal/web/api/catalog.go`, маршрута `GET /api/v1/catalog`, сортировки
`popular`, поиска `q`, sitemap и рабочего UI каталога. Из-за этого у задачи нет
живой цепочки «цель → спека → код».

## What Changes

- Восстанавливается capability `catalog` с требованиями к публичному каталогу,
  пагинации, сортировке `new|popular`, поиску и исключению `unlisted` из
  каталога, sitemap и поиска, включая `noindex` для unlisted.
- Фиксируется происхождение работы: `docs/SPEC.md`, раздел 7 «Каталог и
  шеринг», критерий готовности из раздела 11, контракт `api/openapi.yaml`
  (`/catalog`) и `docs/adr/ADR-004-architecture.md`.
- В `tasks.md` выносятся задачи на отсутствующие части: HTTP-обработчик
  каталога, сортировка по популярности, поиск, sitemap, UI каталога и тесты.
- Уже существующие части не переписываются: фильтр `public`+`open`,
  keyset-пагинация и `noindex` для unlisted соответствуют требованию.
- **BREAKING**: нет. Поведение существующих маршрутов, схема БД и применённые
  миграции не меняются.

## Capabilities

### New Capabilities

- `catalog`: публичный каталог `public`+`open` опросов, пагинация, сортировка
  по новизне и популярности, поиск, а также исключение `unlisted` из каталога,
  sitemap и поиска с отдачей `noindex`.

### Modified Capabilities

Нет: другие capability не меняют своих требований.

## Impact

- Спека: новый `openspec/specs/catalog/spec.md` после архивации.
- Код (задачи на реализацию, не в этом change): `internal/web/api/catalog.go`,
  `internal/web/api/api.go`, `internal/storage/postgres/catalog.go`,
  `internal/tierlist/store.go`, `web/src/pages/CatalogPage.tsx`,
  `web/src/api/client.ts`.
- Контракт: `api/openapi.yaml`, `/catalog`.
- Миграции: без изменений; индекс `tierlists_catalog_idx` уже покрывает
  сортировку по новизне.
- Аудит: вердикт `gap` в `docs/audits/TIERL-6.md`; избыточного кода не
  найдено, удаление не требуется.
