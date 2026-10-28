# Proposal

## Why

TIERL-35 проверяет способность «Создание и редактирование опросов» (якорь цели
`tierlists`), но в OpenSpec нет capability `tierlists`: требование осталось
только в `docs/SPEC.md` §1–§3 и §11, `api/openapi.yaml` и
`docs/adr/ADR-004-architecture.md`. Живая спека `author-profile` покрывает лишь
управление своими опросами из профиля (закрытие, переоткрытие и удаление), а
создания, редактирования и публикации в ней нет.

Код при этом реализует заявленное: автор с подтверждённым email создаёт опрос с
тирами и элементами (`internal/tierlist/service.go:Create`), публикует его с
видимостью и slug (`internal/domain/tierlist.go:NewTierlist`,
`internal/domain/slug.go:GenerateSlug`), редактирует текст и состав до первого
ответа (`Service.Update`, `ReplaceStructure`), закрывает, переоткрывает и
удаляет (`Service.Close`, `Service.Update`, `Service.Delete`). Поэтому
изменение восстанавливает спеку, не меняя поведение продакшен-кода.

## What Changes

- Добавляется capability `tierlists` с требованиями к созданию опроса автором:
  тиры и элементы, границы количества и длин, уникальные позиции, сохранение
  структуры.
- Фиксируется публикация: видимость `public`/`unlisted`, читаемый уникальный
  slug для public и криптостойкий автоматический slug не короче 12 символов для
  unlisted; смена видимости и публичная выдача.
- Фиксируется редактирование: название, описание и видимость доступны всегда,
  состав тиров и элементов — только до первого ответа.
- Фиксируется жизненный цикл: закрытие (идемпотентное), переоткрытие и
  удаление только владельцем, чужой опрос — `404`, удаление вместе с ответами.
- Отмечается пересечение R3 с уже живой capability `author-profile`: маршруты
  и поведение не дублируются, требование восстанавливается для целостности
  якоря `tierlists`.
- В `tasks.md` выносятся follow-up на описание `status` в `TierlistPatch`
  контракта и на стабильный `closed_at` с регрессионным тестом.
- **BREAKING**: нет. Поведение HTTP-маршрутов, схема БД и applied-миграции не
  меняются.

## Capabilities

### New Capabilities

- `tierlists`: создание и редактирование опросов автором, публикация с
  видимостью и slug, а также закрытие, переоткрытие и удаление.

### Modified Capabilities

Нет: `author-profile` уже описывает управление своими опросами и не меняет
своих требований; `catalog`, `reports`, `submissions` и `item-images`
продолжают владеть своими частями поведения.

## Impact

- Спека: новый `openspec/specs/tierlists/spec.md` после архивации.
- Код (только проверка соответствия, без изменения поведения):
  `internal/domain/tierlist.go`, `internal/domain/slug.go`,
  `internal/tierlist/service.go`, `internal/tierlist/store.go`,
  `internal/storage/postgres/tierlists.go`, `internal/storage/postgres/owners.go`,
  `internal/web/api/tierlists.go`, `internal/web/api/api.go`.
- Контракт: `api/openapi.yaml`, разделы `POST /tierlists`, `GET/PATCH/DELETE
  /tierlists/{slug}` и `POST /tierlists/{slug}/close`; описание `status` в
  `TierlistPatch` вынесено в задачи.
- Миграции: без изменений; `20261022100000_create_tierlists`,
  `20261022100100_create_tiers` и `20261022100200_create_items` остаются как
  есть.
- UI: `web/src/features/tierlist-editor` и `web/src/features/profile`.
- Аудит: вердикты `ok` для R1, R2 и R3 в `docs/audits/tierlists.md`;
  избыточного кода не найдено, удаление не требуется.
