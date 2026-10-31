# Proposal

## Why

Аудит TIERL-57 зафиксировал в `docs/audits/reports.md` excess: код способности
`reports`, который не следует ни из живой спеки, ни из `docs/SPEC.md` §2, §3,
§7, ни из ADR-002 и не используется продуктом. Это дублирующие алиасы порога,
полей и методов, неиспользуемая ошибка `ErrReportNotFound`, а также
совместимые представления `tierlist_reports`/`moderation_log` и колонка
`reporter_key` в applied-миграции `20261025140000_create_reports`.

Этот change закрывает только excess. Gap R4 (ручная модерация и чтение аудита)
восстанавливается отдельным change `restore-reports-moderation`; удаление
алиасов его не заменяет.

## What Changes

- Удаляются неиспользуемые алиасы порога в `internal/tierlist/service.go`,
  `internal/tierlist/reports.go` и `internal/web/api/api.go`.
- Удаляются неиспользуемые алиасы `ReportInput`/`Report` и методы
  `Service.AddReport`/`Service.ListAudit`.
- Удаляется ошибка `ErrReportNotFound` и её ветка в `writeServiceError`.
- Новой миграцией удаляются совместимые представления `tierlist_reports`,
  `moderation_log` и генерируемая колонка `reporter_key`; applied-миграция
  `20261025140000_create_reports` не редактируется.
- Поведение R1–R4 и автоскрытие не меняются.

## Capabilities

### New Capabilities

Нет. Удаление избыточного кода не добавляет продуктовой способности.

### Modified Capabilities

Нет. Живая capability `reports` описывает жалобу, порог, недоступность
скрытого опроса и аудит; алиасы и совместимые представления не входят в её
требования. Изменение имеет `skip_specs: true`.

## Impact

- Код: `internal/tierlist/reports.go`, `internal/tierlist/service.go`,
  `internal/web/api/api.go`, тесты, использующие удаляемые поля.
- БД: новая миграция после `20261025140000_create_reports` для удаления
  `reporter_key` и представлений; существующая applied-миграция не меняется.
- Контракт: `api/openapi.yaml` не упоминает удаляемые алиасы; публичные
  маршруты и схемы не меняются.
- Gap R4 остаётся открытым до реализации задач в
  `restore-reports-moderation`.
