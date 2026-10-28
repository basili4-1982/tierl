# Аудит TIERL-8 — работа без спеки

## Контекст

Задача: «Жалобы и модерация публичных опросов» (PR 65). Заявлено: жалоба на
публичный опрос, порог скрытия, статус `hidden`, аудит решений и недоступность
скрытого опроса. Требования: `POST /tierlists/{slug}/reports`, порог скрывает
опрос, `hidden` даёт `404` в каталоге и по ссылке, решения модерации попадают в
аудит.

Происхождение требования восстанавливается: `docs/SPEC.md` (раздел 2 — роль
модератора, раздел 3 — статус `hidden`, раздел 7 — жалобы и скрытие по порогу),
`docs/adr/ADR-002-anonymous-answers.md` (жалобы и модерация действуют на опрос
целиком). Канонической capability в OpenSpec не было, поэтому оформлен change
`restore-reports` с capability `reports`.

## Вердикт

- TIERL-8: ok — internal/web/api/reports.go:createReport; маршрут жалобы, порог скрытия, `hidden` и аудит решений реализованы, скрытый опрос даёт `404`.

## Доказательства по пунктам описания

| Заявлено | Где реализовано | Что подтверждает |
|---|---|---|
| Жалоба на публичный опрос | `internal/web/api/reports.go:createReport`, `internal/tierlist/reports.go:Service.Report` | `Report` принимает только `visibility = public`; для unlisted и скрытого возвращает `ErrNotFound`, не раскрывая существование опроса |
| Маршрут `POST /tierlists/{slug}/reports` | `internal/web/api/api.go:172` (`a.mux.HandleFunc("POST "+Prefix+"/tierlists/{slug}/reports", a.createReport)`) | Маршрут зарегистрирован под `/api/v1`; тело разбирает `internal/web/api/reports.go:decodeReport` |
| Один отчёт на автора жалобы | `internal/tierlist/reports.go:Report` (ключ репортёра), `internal/storage/postgres/reports.go:AddReport`, `migrations/20261025140000_create_reports.up.sql` (`reports_unique_reporter`), `internal/web/api/api.go:277` | Повторная жалоба даёт `409 report_duplicate`; в БД уникальность `(tierlist_id, reporter_ip_hash)` |
| Порог скрытия | `internal/tierlist/reports.go:DefaultReportHideThreshold` (5), `internal/tierlist/reports.go:SetReportHideThreshold`, `internal/storage/postgres/reports.go:AddReport` | После достижения порога в той же транзакции `tierlists.status` становится `hidden`, заполняются `hidden_at`, `hidden_reason`, `reports_count` |
| Статус `hidden` и счётчик | `migrations/20261025140000_create_reports.up.sql` (`hidden_at`, `hidden_reason`, `reports_count`), `internal/storage/postgres/reports.go:AddReport` | Скрытие и обновление счётчика атомарны; строка опроса блокируется `FOR UPDATE` от гонки порога |
| Аудит решений | `migrations/20261025140000_create_reports.up.sql` (`moderation_audit`), `internal/storage/postgres/reports.go:AddReport` (`INSERT ... 'system', 'auto_hide'`), `internal/storage/postgres/reports.go:RecordModerationAction`, `internal/tierlist/reports.go:Hide`/`Unhide` | Автоскрытие пишет `actor_role = system`, `action = auto_hide`, `reason = auto:threshold`; ручные решения пишут актора, роль, действие и причину; чтение — `AuditTrail` |
| `hidden` недоступен в каталоге | `internal/storage/postgres/catalog.go:42` (`status = 'open' AND hidden_at IS NULL`), `internal/storage/postgres/store_test.go:321` | Скрытые и прочие непубличные опросы исключены из `ListPublic`; проверено тестом |
| `hidden` даёт `404` по ссылке | `internal/tierlist/service.go:readable` (только `open`/`closed`), `internal/web/api/tierlists.go:getTierlist`, `internal/web/api/og.go:readableTierlist`, `internal/web/api/og.go:resultFor`, `internal/tierlist/service_test.go:191` | `GET /api/v1/tierlists/{slug}`, страницы `/t/{slug}` и `/r/{id}` и OG-картинки отдают `404` для `hidden` |
| Ошибки маршрута жалобы | `internal/web/api/api.go:writeServiceError`, `internal/web/api/reports.go:createReport` | `ErrReportDuplicate` → `409`, `ErrTierlistHidden`/`ErrReportNotFound`/`ErrNotFound` → `404`, ошибки валидации → `422` |
| Тестовое покрытие жалоб | Нет отдельного `reports_test.go` ни в `internal/tierlist`, ни в `internal/storage/postgres`, ни в `internal/web/api` | Сквозного теста порога, дедупликации и записи аудита нет; это остаточный тестовый gap, а не отсутствие заявленного кода (follow-up в `restore-reports/tasks.md`) |

## Решения

1. **Требование было и исчезло из OpenSpec — восстановить.** Создан change
   `restore-reports` с capability `reports` (якорь цели `reports`): жалоба на
   публичный опрос, порог скрытия, статус `hidden`, недоступность скрытого
   опроса и append-only аудит решений. Поведение кода не меняется.
2. **Gap в коде: нет.** Обработчик, маршрут, сервис, хранилище, миграция и
   аудит найдены; заявленные требования выполнены, поэтому общий вердикт `ok`.
3. **Excess: нет.** Скрытие по порогу, `hidden_at`/`hidden_reason`/`reports_count`
   и `moderation_audit` следуют `docs/SPEC.md` (разделы 2, 3, 7) и ADR-002.
   Совместимые alias-имена и SQL-представления не добавляют отдельного
   поведения, поэтому удаление кода не требуется.
4. **Тестовый и контрактный gap: вынесен как задачи на реализацию.** В
   `openspec/changes/restore-reports/tasks.md` добавлены follow-up на тесты
   жалоб/порога/аудита и на описание `POST /tierlists/{slug}/reports` в
   `api/openapi.yaml`; код и тесты в границах этого аудита не меняются.
5. **Эскалация человеку не требуется:** происхождение требования
   восстанавливается по SPEC и ADR-002, а реализация подтверждена чтением кода.

## Границы

Код, тесты, контракт и applied-миграции не изменялись. Изменены только
`docs/audits/TIERL-8.md` и файлы `openspec/changes/restore-reports/`.
