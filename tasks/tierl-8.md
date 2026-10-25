# TIERL-8: Жалобы и модерация публичных опросов

## Что сделано

Реализован полный цикл модерации публичных тирлистов: жалоба на опрос, автоматическое скрытие по порогу, недоступность скрытых опросов в каталоге и по прямой ссылке, аудит действий модератора.

## Список файлов

### Миграции
- `migrations/0003_tierlist_reports_and_moderation.up.sql` — новые таблицы `tierlist_reports`, `moderation_audit`, колонки `hidden_at`, `hidden_reason`, `reports_count` в `tierlists`, индексы.
- `migrations/0003_tierlist_reports_and_moderation.down.sql` — откат.

### Домен — `internal/tierlist`
- `report.go` — сущности `Report`, `ReportReason`, `ModerationAction`, валидация причины, константы статусов.
- `report.go` (в составе) — доменные ошибки: `ErrReportDuplicate`, `ErrReportNotFound`, `ErrNotModerator`, `ErrTierlistHidden`.
- `config.go` — `HideReportsThreshold` (по умолчанию `5`) в конфиге домена.
- `moderation.go` — сервис `ModerationService`: `Report`, `Hide`, `Unhide`, `ListPending`, `ApplyAutoHide`; порог и аудит.
- `moderation_test.go` — юнит-тесты на порог, дубликаты, аудит, hidden-поведение.
- `repository.go` — расширены интерфейсы `TierlistRepository` (методы `SetHidden`, `IncrementReports`) и добавлен `ReportRepository`, `ModerationAuditRepository`.

### Хранилище — `internal/storage/postgres`
- `reports.go` — реализация `ReportRepository` (создание, подсчёт уникальных по (tierlist, reporter_ip_hash), список).
- `moderation_audit.go` — реализация `ModerationAuditRepository`.
- `tierlists.go` — доработка: `SetHidden`, `IncrementReports`, фильтрация `hidden_at IS NULL` в каталоге, `GetBySlug` возвращает опрос с флагом `hidden`.
- `reports_test.go` — интеграционные тесты на порог и аудит (sqlite/inmem-совместимый режим под тег `integration`).

### HTTP API — `internal/web/api`
- `reports.go` — `POST /tierlists/{slug}/reports` (публичный), `GET /moderation/reports` (модератор), `POST /moderation/tierlists/{slug}/hide`, `POST /moderation/tierlists/{slug}/unhide`, `GET /moderation/audit`.
- `reports_test.go`, `moderation_test.go` — httptest на все эндпоинты, включая проверку «hidden недоступен по ссылке» (404 в каталоге и при `GET /tierlists/{slug}`).
- `router.go` — регистрация новых маршрутов и middleware `requireModerator` (внутри api-пакета).
- `errors.go` — маппинг доменных ошибок на HTTP-коды: `ErrReportDuplicate` → 409, `ErrNotModerator` → 403, `ErrTierlistHidden` → 404.

## Ключевые решения

**Порог скрытия.** `ModerationService.Report` после вставки жалобы читает `reports_count` по опросу; если `reports_count >= HideReportsThreshold` и опрос не скрыт — переводит в `hidden` с причиной `auto:threshold`, пишет запись в `moderation_audit` (`actor=system`, `action=auto_hide`).

**Уникальность жалоб.** Одна жалоба на опрос от одного отправителя (`UNIQUE(tierlist_id, reporter_ip_hash)`), повторная → `ErrReportDuplicate` → 409. Это защищает от самопроизвольного скрытия.

**Hidden недоступен.** Каталог (`ListPublic`) фильтрует `hidden_at IS NULL`. `GET /tierlists/{slug}` для скрытого возвращает 404 без утечки содержимого. Модераторы видят скрытые через отдельный `GET /moderation/tierlists?include_hidden=true`.

**Аудит.** Каждое решение (auto_hide, hide, unhide) пишется в `moderation_audit`: `actor_id`, `actor_role`, `action`, `tierlist_id`, `reason`, `created_at`. Эндпоинт `GET /moderation/audit?tierlist={slug}` возвращает историю.

## Миграция (фрагмент)

```sql
-- +goose Up
ALTER TABLE tierlists
    ADD COLUMN hidden_at     TIMESTAMPTZ,
    ADD COLUMN hidden_reason TEXT,
    ADD COLUMN reports_count INT NOT NULL DEFAULT 0;

CREATE INDEX idx_tierlists_public ON tierlists (created_at DESC)
    WHERE hidden_at IS NULL AND is_public = true;

CREATE TABLE tierlist_reports (
    id              BIGSERIAL PRIMARY KEY,
    tierlist_id     UUID NOT NULL REFERENCES tierlists(id) ON DELETE CASCADE,
    reporter_ip_hash TEXT NOT NULL,
    reason          TEXT NOT NULL,
    comment         TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tierlist_id, reporter_ip_hash)
);
CREATE INDEX idx_reports_tierlist ON tierlist_reports(tierlist_id);

CREATE TABLE moderation_audit (
    id          BIGSERIAL PRIMARY KEY,
    tierlist_id UUID NOT NULL REFERENCES tierlists(id) ON DELETE CASCADE,
    actor_id    UUID,
    actor_role  TEXT NOT NULL,
    action      TEXT NOT NULL,
    reason      TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_tierlist ON moderation_audit(tierlist_id, created_at DESC);
```

## Команды проверки (прогнаны)

```
go build ./...                          # OK
go vet ./...                            # OK
go test ./internal/tierlist/...         # ok
go test ./internal/web/api/...          # ok
go test -tags=integration ./internal/storage/postgres/...   # ok
go test ./...                           # ok
```

## Границы задачи

Изменения строго в `internal/tierlist`, `internal/storage/postgres`, `internal/web/api`, `migrations`. Применённые миграции `0001`, `0002` не тронуты. Секреты не коммичены — `reporter_ip_hash` формируется HMAC-SHA256 с ключом из env `REPORT_HASH_SECRET` (значение по умолчанию только для dev).

## Замечания

- Порог берётся из конфига (`HideReportsThreshold`, default 5) — позволяет менять без релиза.
- Роль модератора — из существующего JWT-claim `role` (`moderator`/`admin`); middleware `requireModerator` находится в `internal/web/api` и не затрагивает соседние пакеты аутентификации.
- Второй агент для приёмки может проверить критерий «по порогу скрывается» функциональным тестом `TestReportThresholdHidesTierlist` в `internal/tierlist/moderation_test.go`; «аудит» — `TestModerationAuditTrail`; «hidden недоступен» — `TestHiddenTierlistNotFoundInCatalogAndBySlug` в `internal/web/api/moderation_test.go`.