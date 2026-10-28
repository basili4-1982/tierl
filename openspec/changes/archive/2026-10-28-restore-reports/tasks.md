# Tasks

## 1. Происхождение работы

- [x] 1.1 Сопоставить TIERL-8 с целью MVP «каталог и шеринг» (`docs/SPEC.md`, раздел 7), ролью модератора и статусом `hidden` (разделы 2 и 3) и решением `ADR-002`; результат записан в `proposal.md` и `design.md`
- [x] 1.2 Проверить, что требование не исчезло из кода: жалоба, порог, `hidden` и аудит реализованы в `internal/web/api/reports.go`, `internal/tierlist/reports.go`, `internal/storage/postgres/reports.go` и миграции `20261025140000_create_reports`
- [x] 1.3 Классифицировать работу как необходимую: избыточных маршрутов и мёртвого кода в границах аудита не найдено, удаление не требуется

## 2. Восстановление спеки

- [x] 2.1 Добавить capability `reports` с требованиями к жалобе, порогу скрытия, недоступности скрытого опроса и append-only аудиту решений
- [x] 2.2 Связать требования с существующими маршрутом `POST /api/v1/tierlists/{slug}/reports`, сервисом и схемой БД

## 3. Сверка кода со спекой

- [x] 3.1 Проверить обработчик и маршрут жалобы: `internal/web/api/reports.go:createReport` и `internal/web/api/api.go:New`; проверка — маршрут `POST /api/v1/tierlists/{slug}/reports`
- [x] 3.2 Проверить допустимость только публичных опросов и дедупликацию: `internal/tierlist/reports.go:Report`, `internal/storage/postgres/reports.go:AddReport`, ограничение `reports_unique_reporter`; проверка — `409 report_duplicate`
- [x] 3.3 Проверить порог скрытия: `DefaultReportHideThreshold`, `SetReportHideThreshold`, атомарное обновление `status`, `hidden_at`, `hidden_reason` и `reports_count`
- [x] 3.4 Проверить недоступность скрытого опроса: `internal/web/api/og.go:readableTierlist`, `internal/web/api/og.go:resultFor`, `internal/tierlist/service.go:readable`, `internal/storage/postgres/catalog.go:ListPublic`; проверка — `404` по ссылке и исключение из каталога
- [x] 3.5 Проверить аудит решений: `moderation_audit` в миграции, `AddReport` (`auto_hide`), `RecordModerationAction`, `Hide`/`Unhide` и `AuditTrail`
- [x] 3.6 Записать вердикт `ok` с доказательствами в `docs/audits/TIERL-8.md`

## 4. Проверки

- [x] 4.1 Проверить `openspec validate restore-reports --strict` без ошибок
- [x] 4.2 Проверить `openspec validate --strict --all --no-interactive` без ошибок

## 5. Follow-up

- [x] 5.1 Добавить автоматические тесты жалоб: `internal/web/api/reports_test.go` на `201`, `404` для unlisted/hidden и `409` при повторе; тест порога и записи `auto_hide` в `internal/tierlist` и `internal/storage/postgres`. Код и тесты в границах аудита не меняются. — вынесено в задачу трекера TIERL-29
- [x] 5.2 Описать `POST /api/v1/tierlists/{slug}/reports` и коды `201`/`404`/`409`/`422` в `api/openapi.yaml`. Контракт в границах аудита не меняется. — вынесено в задачу трекера TIERL-29
- [x] 5.3 Изменение архивировано: capability попала в живые спеки
