# Аудит TIERL-57 — Жалобы и модерация

## Контекст

Задача: TIERL-57 — аудит способности «Жалобы и модерация» (якорь цели
`reports`). Требования аудита: R1 — жалоба на публичный опрос; R2 — порог
жалоб скрывает опрос; R3 — скрытый опрос недоступен; R4 — аудит решений
модерации.

В отличие от формулировки задачи, живая capability `reports` в workspace уже
существует: `openspec/specs/reports/spec.md` восстановлен архивным change
`2026-10-28-restore-reports`. Поэтому аудит сверяет код с живой спекой и
`docs/SPEC.md` §2, §3, §7, ADR-002 и `api/openapi.yaml`, а не создаёт
способность заново. Якорь `reports` не переименовывается.

Что проверялось:

- маршрут `POST /api/v1/tierlists/{slug}/reports` и его ответы `201`/`404`/`409`/`422`;
- допустимость только `public`-опросов, дедупликация жалоб и валидация причины/комментария;
- порог `5`, настраиваемость через конфигурацию сервиса и атомарность скрытия;
- недоступность скрытого опроса в каталоге, `GET /api/v1/tierlists/{slug}`, `/t/{slug}` и `/r/{id}`;
- автоматический и ручной аудит решений, append-only-хранение и порядок чтения;
- код без требования (совместимые алиасы, неиспользуемые представления и методы).

## Вердикт

- R1: ok — internal/web/api/reports.go:createReport.Handles `POST /api/v1/tierlists/{slug}/reports`: принимает только `public`-опрос, валидирует причину и комментарий, отклоняет повтор и отвечает `201` с `id`, `reports_count`, `hidden`.
- R2: ok — internal/storage/postgres/reports.go:AddReport.Hides опрос при достижении порога `5` в одной транзакции с жалобой и обновляет `status`, `hidden_at`, `hidden_reason`, `reports_count`.
- R3: ok — internal/storage/postgres/catalog.go:ListPublic.Excludes скрытые опросы из каталога, а `internal/tierlist/service.go:readable` и `internal/web/api/og.go:readableTierlist`/`resultFor` дают `404` для `/api/v1/tierlists/{slug}`, `/t/{slug}` и `/r/{id}`.
- R4: gap — автоматический `auto_hide` и чтение аудита в БД есть, но ручные hide/unhide и чтение аудита недоступны из продукта: `internal/tierlist/reports.go:Hide`/`Unhide`/`AuditTrail` не подключены ни к одному маршруту или UI, а `Unhide` не очищает `hidden_at`/`hidden_reason`, поэтому автоскрытый public-опрос после переоткрытия не возвращается в каталог.

## Доказательства по требованиям

### R1 — жалоба на публичный опрос

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Маршрут `POST /api/v1/tierlists/{slug}/reports` | `internal/web/api/api.go:174` (`a.mux.HandleFunc("POST "+Prefix+"/tierlists/{slug}/reports", a.createReport)`), `internal/web/api/reports.go:createReport` | Маршрут зарегистрирован под `/api/v1`; обработчик разбирает тело и вызывает `Service.Report` |
| Только `public`-опрос | `internal/tierlist/reports.go:Report` (проверка `t.Visibility != domain.VisibilityPublic`), `internal/tierlist/service.go:readable` | `unlisted`, `draft` и `hidden` возвращают `ErrNotFound`, а `createReport` отдаёт `404 not_found`, не раскрывая опрос |
| Необязательная причина и лимиты | `internal/tierlist/reports.go:Report` (`reason = "other"` при пустой причине, лимиты 100/2000 рун), `migrations/20261025140000_create_reports.up.sql` (`reports_reason_check`, `reports_comment_check`) | Проверка в сервисе до обращения к опросу; `domain.ValidationError` маппится в `422 validation_failed` (`internal/web/api/api.go:writeServiceError`) |
| Один автор — одна жалоба | `internal/web/api/reports.go:reporterKey`, `internal/tierlist/reports.go:Report`, `internal/storage/postgres/reports.go:AddReport` (`isReportCollision`), `migrations/20261025140000_create_reports.up.sql` (`reports_unique_reporter`) | Ключ репортёра — аккаунт, анонимная сессия или явный `reporter`; уникальность `(tierlist_id, reporter_ip_hash)` даёт `409 report_duplicate` |
| Ответ `201` с `id`, `reports_count`, `hidden` | `internal/web/api/reports.go:reportDTO`, `createReport` | DTO содержит обязательные поля ответа; `ReportsCount` и `Hidden` берутся из результата сервиса |
| Тесты | `internal/web/api/reports_test.go:TestReportEndpoints`, `internal/tierlist/reports_test.go:TestReportThresholdAndAudit`, `TestReportRequiresPublicTierlist`, `internal/storage/postgres/reports_test.go:TestReportThresholdAndAutoHide` | Покрыты `201`, повтор `409`, `422`, `404` для `unlisted`/`hidden`, причина по умолчанию `other` |

### R2 — порог жалоб скрывает опрос

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Порог по умолчанию `5` | `internal/tierlist/reports.go:DefaultReportHideThreshold` (`const ... = 5`), `internal/tierlist/reports.go:threshold` | `NewService` и `SetReportHideThreshold` подставляют значение по умолчанию, если порог не задан |
| Настройка конфигурацией сервиса | `internal/tierlist/service.go:ServiceDeps.ReportHideThreshold`, `internal/tierlist/reports.go:SetReportHideThreshold`, `internal/web/api/api.go:Deps.ReportHideThreshold` | Порог можно задать через `ServiceDeps` или `api.Deps`; `MAY` в спеке допускает и значение по умолчанию |
| Скрытие и счётчик в одной транзакции | `internal/storage/postgres/reports.go:AddReport` (`inTx`, `SELECT ... FOR UPDATE`, `INSERT INTO reports`, `UPDATE tierlists`, `INSERT INTO moderation_audit`) | Жалоба, `reports_count`, `status = 'hidden'`, `hidden_at`, `hidden_reason` и запись `auto_hide` коммитятся вместе; гонка сериализуется блокировкой строки |
| До порога статус не меняется | `internal/storage/postgres/reports.go:AddReport` (`hide := count >= threshold`), `internal/tierlist/tierlisttest/tierlisttest.go:AddReport` | При `count < threshold` SQL оставляет прежний `status`, но обновляет `reports_count` |
| Ответ жалобы с `hidden = true` | `internal/tierlist/reports.go:Report` → `ReportResult.Hidden`, `internal/web/api/reports.go:createReport` | `ReportOutcome.Hidden` заполняется при скрытии и попадает в DTO |
| Тесты | `internal/tierlist/reports_test.go:TestReportThresholdAndAudit`, `internal/storage/postgres/reports_test.go:TestReportThresholdAndAutoHide` | Проверены порог `2`, первый/второй отчёт, `auto_hide`, `hidden_at`, `hidden_reason`, `reports_count` |

### R3 — скрытый опрос недоступен

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Каталог без скрытых опросов | `internal/storage/postgres/catalog.go:ListPublic` (`t.status = 'open' AND t.hidden_at IS NULL`) | Фильтр применяется и для сортировки по новизне, и для популярности; в каталог попадают только `public`+`open` |
| `GET /api/v1/tierlists/{slug}` — `404` | `internal/web/api/api.go:170`, `internal/web/api/tierlists.go:getTierlist`, `internal/tierlist/service.go:Get`, `readable` | `readable` пропускает только `open`/`closed`; `hidden` и `draft` дают `ErrNotFound` → `404 not_found` |
| `/t/{slug}` — `404` | `internal/web/api/og.go:mountShareRoutes`, `handleShareTerm`, `readableTierlist` | Страница получает опрос через `Service.Get` (или проверку `readableStatus`) и при ошибке вызывает `http.NotFound` |
| `/r/{id}` — `404` | `internal/web/api/og.go:handleShareResult`, `resultFor`, `internal/tierlist/submissions.go:Result`, `internal/app/og_results.go:GetOGResult` | Результат проверяет `readableStatus` опроса; для `hidden` оба пути — API и share-страница — отдают `404` |
| Публичные и закрытые опросы сохраняют доступ | `internal/tierlist/service.go:readable`, `internal/web/api/og.go:readableStatus` | `open` и `closed` остаются читаемыми; меняется только `hidden` |
| Тесты | `internal/tierlist/service_test.go:TestGet` (`hidden and draft are not found`), `internal/web/api/api_test.go`, `internal/storage/postgres/store_test.go` (каталог исключает `hidden`) | Подтверждены `404` для `hidden` и отсутствие скрытого опроса в каталоге |

### R4 — аудит решений модерации

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Append-only таблица аудита | `migrations/20261025140000_create_reports.up.sql` (`moderation_audit`, индексы), `internal/storage/postgres/reports.go:AuditTrail` | В коде есть только `INSERT` и `SELECT`; `UPDATE`/`DELETE` аудита отсутствуют |
| Автоскрытие пишет `system`/`auto_hide` | `internal/storage/postgres/reports.go:AddReport` (`VALUES ($1, NULL, 'system', 'auto_hide', $2, $3, $4)`), `internal/tierlist/tierlisttest/tierlisttest.go:AddReport` | Запись содержит `actor_role = system`, действие `auto_hide`, причину `auto:threshold` и `reports_count` |
| Чтение от новых к старым | `internal/storage/postgres/reports.go:AuditTrail` (`ORDER BY created_at DESC, id DESC`), `internal/tierlist/reports.go:AuditTrail` | Порядок задан SQL; сервис возвращает записи без переупорядочивания |
| Ручное скрытие/переоткрытие | `internal/tierlist/reports.go:Hide`, `Unhide`, `recordAudit`, `internal/storage/postgres/reports.go:RecordModerationAction` | В сервисе методы есть, но **маршрутов и обработчиков нет**: в `internal/web/api/api.go:143-180` зарегистрированы только жалоба, каталог, опросы, ответы, результаты и медиа; в `web/src` нет модерации |
| Чтение аудита из продукта | `internal/tierlist/reports.go:AuditTrail`, `ReportsCount` | Методы не вызываются ни из HTTP-слоя, ни из UI: `grep` по `api`/`web` не находит `AuditTrail`, `ListAudit`, `ReportsCount` у сервиса |
| Стабильное переоткрытие | `internal/tierlist/reports.go:Unhide`, `internal/storage/postgres/tierlists.go:SetStatus`, `internal/storage/postgres/catalog.go:ListPublic` | `Unhide` ставит `status = open`, но `SetStatus` не очищает `hidden_at`/`hidden_reason`; каталог требует `hidden_at IS NULL`, поэтому автоскрытый и переоткрытый public-опрос остаётся вне каталога |
| Атомарность ручного решения | `internal/tierlist/reports.go:Hide`/`Unhide` (`SetStatus`, затем `recordAudit`), `internal/storage/postgres/reports.go:RecordModerationAction` | Смена статуса и запись аудита не в одной транзакции: сбой аудита оставляет решение без записи |
| Тесты | `internal/tierlist/reports_test.go` (только автоскрытие и `auto_hide`), `internal/domain/domain_test.go:TestTransitions` (только домен) | Нет теста ручного hide/unhide, очистки `hidden_at` и чтения аудита через HTTP |

## Gap

- **R4 — заявленное ручное решение модерации не реализовано в продукте.**
  Живая спека требует, чтобы ручное скрытие и переоткрытие записывались в аудит
  с актором, ролью, действием и причиной. В коде есть только
  `Service.Hide`/`Service.Unhide` (`internal/tierlist/reports.go:179-220`), но
  ни один HTTP-маршрут, обработчик или UI их не вызывает. Сценарий «модератор
  скрывает или переоткрывает опрос» недостижим.
- **R4 — чтение аудита не выведено наружу.** `Service.AuditTrail`
  (`internal/tierlist/reports.go:237`) и `Store.AuditTrail`
  (`internal/storage/postgres/reports.go:100`) возвращают записи в правильном
  порядке, но доступны только внутреннему коду; маршрута чтения аудита нет.
- **R4 — переоткрытие не восстанавливает публичный доступ.** `Unhide`
  переводит опрос в `open`, но `Store.SetStatus`
  (`internal/storage/postgres/tierlists.go:178`) не обнуляет `hidden_at` и
  `hidden_reason`. Поскольку `ListPublic`
  (`internal/storage/postgres/catalog.go:67,86`) требует `hidden_at IS NULL`,
  автоскрытый опрос после переоткрытия не возвращается в каталог, что нарушает
  и R3 («публичные опросы сохраняют прежние правила доступа»).
- **R4 — ручное решение не атомарно.** `Hide`/`Unhide` сначала меняют статус,
  затем пишут аудит. Ошибка `RecordModerationAction` оставляет изменение без
  записи, хотя требование говорит, что решение MUST записываться.

## Excess

Следующий код не следует ни из одного требования `reports` (ни из живой
спеки, ни из `docs/SPEC.md` §2, §3, §7, ни из ADR-002) и не используется
продуктом:

| Excess | Где | Почему избыточно |
|---|---|---|
| Алиасы порога | `internal/tierlist/reports.go:18` (`DefaultHideReportsThreshold`), `internal/tierlist/service.go:52-58` (`HideReportsThreshold`, `ReportThreshold`, `ReportsThreshold`), `internal/web/api/api.go:70-74` | Спеке нужен один порог; алиасы нигде не используются и дублируют `ReportHideThreshold` |
| Алиасы входа жалобы | `internal/tierlist/reports.go:52-54` (`ReporterID`, `ReporterIPHash`, `SessionID` в `ReportInput`), `internal/tierlist/reports.go:38` (`Report.ReporterIPHash`) | HTTP-слой передаёт `ReporterKey`; остальные поля не нужны требованию и используются только в `firstNonEmpty`/тестовой подстановке |
| Алиасы методов сервиса | `internal/tierlist/reports.go:160-162` (`Service.AddReport`), `internal/tierlist/reports.go:165-167` (`Service.ListAudit`) | Полные дубли `Report` и `AuditTrail`; вызовов из продукта нет |
| Неиспользуемая ошибка | `internal/tierlist/reports.go:25` (`ErrReportNotFound`) и её ветка в `internal/web/api/api.go:283` | Ни сервис, ни хранилище не возвращают эту ошибку |
| Совместимые представления и колонка | `migrations/20261025140000_create_reports.up.sql:15` (`reporter_key`), `:30-32` (`tierlist_reports`), `:52-54` (`moderation_log`) | Go-код и контракт не читают эти представления и колонку; требование описывает только `reports` и `moderation_audit` |

Applied-миграция `20261025140000_create_reports` не меняется: удаление
избыточных колонки и представлений планируется отдельной новой миграцией.

## Решения

1. **R1, R2, R3 — ok, код менять не нужно.** Жалоба, порог скрытия и
   недоступность скрытого опроса реализованы, покрыты тестами и
   соответствуют живой спеке; отдельных задач на реализацию или удаление по
   этим требованиям нет.
2. **R4 — gap → change на восстановление требования и задача на реализацию.**
   Создан `openspec/changes/restore-reports-moderation` с capability `reports`
   (якорь не переименован). Дельта добавляет требования «Интерфейс ручной
   модерации», «Атомарный аудит ручного решения» и «Чтение аудита модерации»:
   маршруты ручного hide/unhide, очистка `hidden_at`/`hidden_reason` при
   переоткрытии, атомарная запись аудита и чтение от новых к старым. В `tasks.md` вынесены задачи на
   реализацию кода и тестов; в границах аудита поведение не меняется.
3. **Excess → change на удаление кода.** Создан
   `openspec/changes/remove-reports-aliases` (`skip_specs: true`) с задачами
   удалить неиспользуемые алиасы, `ErrReportNotFound` и совместимые
   представления/колонку через новую миграцию, не редактируя применённую
   `20261025140000_create_reports`. Это не меняет требования R1–R4.
4. **Способность названа по якорю цели.** Используется `reports`; новое имя не
   вводится, потому что жалобы, скрытие и аудит — одна цель «Жалобы и
   модерация».
5. **Поведение кода не менялось.** Изменены только аудит и планирующие
   артефакты OpenSpec.

## Проверки

- `test -f docs/audits/reports.md` — файл создан.
- `grep -Eq "^- R[1-4]: (ok|gap) — ..."` — вердикты R1–R4 присутствуют и
  содержат доказательства.
- `go mod download` — зависимости загружены.
- `go test ./internal/tierlist ./internal/web/api` — пакеты прошли (`ok`).
- `go test ./internal/storage/postgres -run TestReportThresholdAndAutoHide`
  — пакет прошёл (`ok`), но интеграционный тест пропущен: в окружении нет
  Docker (`rootless Docker not found`); код в границах аудита не менялся.
- `openspec validate --strict --all --no-interactive` — все элементы прошли,
  ошибок нет.
- Код, тесты, контракт и применённые миграции не изменялись.

## Границы

Код, тесты, контракт и применённые миграции не изменялись. Изменены только
`docs/audits/reports.md` и файлы
`openspec/changes/restore-reports-moderation/` и
`openspec/changes/remove-reports-aliases/`.
