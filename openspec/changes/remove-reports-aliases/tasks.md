# Tasks

## 1. Аудит TIERL-57

- [x] 1.1 Сопоставить алиасы и представления `reports` с требованиями R1–R4 и живой спекой
- [x] 1.2 Подтвердить, что `DefaultHideReportsThreshold`, `HideReportsThreshold`, `ReportThreshold`, `ReportsThreshold`, поля `ReportInput`, `Service.AddReport`/`ListAudit` и `ErrReportNotFound` не используются продуктом
- [x] 1.3 Подтвердить, что `reporter_key`, `tierlist_reports` и `moderation_log` не читаются Go-кодом и контрактом
- [x] 1.4 Записать excess в `docs/audits/reports.md`

## 2. Удаление избыточного кода

- [ ] 2.1 Удалить алиасы порога в `internal/tierlist/reports.go`, `internal/tierlist/service.go` и `internal/web/api/api.go`
- [ ] 2.2 Удалить неиспользуемые поля `ReportInput`/`Report`, методы `Service.AddReport`/`Service.ListAudit` и ошибку `ErrReportNotFound` с веткой в `writeServiceError`
- [ ] 2.3 Обновить тесты, которые используют удаляемые поля, не меняя поведение жалоб и автоскрытия
- [ ] 2.4 Добавить новую миграцию для удаления представлений `tierlist_reports`, `moderation_log` и колонки `reporter_key`, не редактируя applied-миграцию `20261025140000_create_reports`
- [ ] 2.5 Проверить, что на удаляемые алиасы и представления нет ссылок в коде, контракте и CI

## 3. Сверка с gap

- [ ] 3.1 Убедиться, что удаление excess не заменяет реализацию R4 из `restore-reports-moderation` и не меняет требования R1–R4

## 4. Проверки

- [x] 4.1 Подтвердить, что в границах аудита код, тесты, контракт и applied-миграции не менялись
- [x] 4.2 Проверить `openspec validate --strict --all --no-interactive` без ошибок
