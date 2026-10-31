# Proposal

## Why

Аудит TIERL-56 подтвердил, что R1–R4 способности «Каталог и поиск» реализованы
в коде, а живая capability `catalog` уже существует: её восстановил архивный
change `2026-10-28-add-catalog`. При этом живая спека описывает sitemap только
негативно — в требовании «Unlisted вне индексации» сказано, что unlisted не
должен попадать в sitemap. Нормативного описания самого sitemap в спеке нет:
не зафиксированы маршрут `GET /sitemap.xml`, состав только `public`+`open`,
абсолютные URL `/t/<slug>` и валидный пустой `urlset` при отсутствии опросов.
Из-за этого R3 не имеет полной цепочки «цель → спека → код», хотя реализация и
тесты уже есть.

## What Changes

- В capability `catalog` добавляется требование «Sitemap публичных опросов»:
  `GET /sitemap.xml` отдаёт XML `urlset` с абсолютными URL `/t/<slug>` только
  для `public`+`open`; unlisted, черновики, закрытые и скрытые не попадают;
  при отсутствии опросов — `200` и валидный пустой `urlset`.
- Существующие требования `catalog` (public+open, пагинация, сортировка,
  поиск, `noindex` для unlisted) не переписываются.
- В `tasks.md` выносится follow-up: уточнить описание `/catalog` в
  `api/openapi.yaml` до `public`+`open`.
- Поведение кода, тестов, HTTP-маршрутов, контракта и applied-миграций в этом
  change не меняется.

## Capabilities

### New Capabilities

Нет. Живая capability `catalog` уже описывает каталог, пагинацию, сортировку,
поиск и исключение unlisted; новая способность не вводится.

### Modified Capabilities

- `catalog`: добавляется требование к sitemap публичных опросов, чтобы
  положительный контракт R3 был виден в живой спеке. Якорь `catalog`
  сохраняется без переименования.

## Impact

- Спека: `openspec/specs/catalog/spec.md` после архивации получит требование
  «Sitemap публичных опросов».
- Код (только проверка соответствия, без изменения поведения):
  `internal/web/api/catalog.go:sitemap`, `internal/web/api/catalog.go:allPublicEntries`,
  `internal/web/api/api.go`, `internal/web/handler.go`.
- Контракт: описание `/catalog` в `api/openapi.yaml` уточняется отдельной
  follow-up задачей; схема и маршруты не меняются.
- Аудит: `docs/audits/catalog.md` фиксирует вердикты `ok` по R1–R4,
  отсутствие gap и excess в коде и найденный пробел спеки R3.
