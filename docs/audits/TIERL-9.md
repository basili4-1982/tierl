# Аудит TIERL-9 — E2E-сценарии MVP

## Контекст

Задача: E2E-сценарии MVP (PR 55). Заявлено: регистрация → подтверждение email →
создание → шеринг → ответ гостя → результат; unlisted не в каталоге; повторная
отправка по контракту. Файл-результат — `internal/app/mvp_e2e_test.go`,
контекст — `internal/app/integration_test.go`, `internal/web/api`,
`docs/SPEC.md`. Требования: тест зелёный на чистой БД, unlisted-тест,
повторная отправка по контракту.

Происхождение требования восстанавливается: `docs/SPEC.md` (раздел 1 —
сценарий; раздел 4 — один ответ на анонимную сессию и замена по контракту;
раздел 11 — путь автора без ручных действий в БД и ответ гостя без
регистрации) и `api/openapi.yaml` (`/auth/verify-email`,
`/tierlists/{slug}/submissions`, `/tierlists/{slug}/submissions/mine`,
`/results/{id}`). Канонической capability в OpenSpec не было, поэтому оформлен
change `restore-mvp-e2e` с capability `mvp-e2e`.

## Вердикт

- TIERL-9: gap — internal/app/mvp_e2e_test.go:confirmAuthor подтверждает email прямым `UPDATE users`, а TestMVPFlowRegistrationToResult вставляет ответы через Store.Save и считает результат similarity.ThinkLikeYou в обход существующих HTTP-маршрутов; TestMVPResubmissionContract проверяет Store.Save, а не контракт повторного POST и PUT /submissions/mine.

## Что заявлено и что найдено

### Есть в коде

- Файл-результат присутствует: `internal/app/mvp_e2e_test.go` с
  `TestMVPFlowRegistrationToResult` (`:157`), `TestMVPResubmissionContract`
  (`:245`) и `TestMVPUnlistedNotInCatalog` (`:304`).
- Чистая БД обеспечена общим `testDB`: `internal/app/integration_test.go:47`
  вызывает `MigrateDown` (`:63`) и `MigrateUp` (`:66`) перед каждым тестом.
- Регистрация и создание опроса идут через `web.NewHandler`:
  `internal/app/mvp_e2e_test.go:53` (`newMVPFixture`), `:112` (`create`) и
  `:166` (вызов `create` из сценария, внутри — `POST /api/v1/tierlists`);
  неподтверждённый автор получает `403 email_not_verified` (`:161`).
- Шеринг проверяется через API: `internal/app/mvp_e2e_test.go:173` вызывает
  `GET /api/v1/tierlists/{slug}` для гостя.
- Unlisted-тест есть: `TestMVPUnlistedNotInCatalog` (`:304`) проверяет
  случайный slug (`:312`), доступ по ссылке (`:319`) и отсутствие unlisted в
  `Store.ListPublic` (`:327`). Фильтр каталога реализован в
  `internal/storage/postgres/catalog.go:Store.ListPublic`.
- Публичные маршруты для недостающих шагов уже существуют:
  `POST /api/v1/auth/verify-email` (`internal/web/api/api.go:156`,
  `internal/web/api/auth.go:verifyEmail`), `POST`/`PUT` ответов
  (`internal/web/api/api.go:173`, `:174`,
  `internal/web/api/submissions.go:createSubmission`,
  `internal/web/api/submissions.go:replaceSubmission`) и
  `GET /api/v1/results/{id}` (`internal/web/api/api.go:176`,
  `internal/web/api/results.go:getResult`).
- Контракт повторной отправки описан: `api/openapi.yaml:391` (повторный `POST`
  даёт `409 already_submitted` с `details.result_id`) и `api/openapi.yaml:447`
  (`PUT /submissions/mine` заменяет ответ и сохраняет `result_id`).

### Чего нет (gap)

- Нет подтверждения email через публичный API в E2E: `confirmAuthor`
  (`internal/app/mvp_e2e_test.go:90`) выполняет
  `UPDATE users SET email_verified = true` (`:93`), то есть ручное действие в
  БД, прямо запрещённое критерием `docs/SPEC.md` §11. Маршрут
  `POST /auth/verify-email` и повторная отправка в сценарии не участвуют.
- Нет E2E-ответа гостя через API: `TestMVPFlowRegistrationToResult` вставляет
  20 ответов через `f.store.Save` (`internal/app/mvp_e2e_test.go:191`), минуя
  `POST /api/v1/tierlists/{slug}/submissions` и выдачу анонимной cookie.
- Нет получения результата через API: результат считается прямым вызовом
  `similarity.ThinkLikeYou` (`internal/app/mvp_e2e_test.go:229`), а не через
  `GET /api/v1/results/{id}` и `/results/{id}/stats`; проверяются внутренние
  структуры хранилища, а не контракт ответа.
- Повторная отправка проверена не по контракту:
  `TestMVPResubmissionContract` вызывает `f.store.Save` дважды
  (`internal/app/mvp_e2e_test.go:259`, `:267`) и проверяет `SaveResult.Replaced`.
  Повторный `POST` с `409 already_submitted` и `PUT /submissions/mine` с тем же
  `id` в этом файле не проверяются. Контрактный тест
  `internal/web/api/submissions_contract_test.go:TestReplaceUpdatesStatsAndKeepsIdentifiersPrivate`
  покрывает API отдельно, но заявленный E2E-файл его не использует.
- Unlisted проверяется только на уровне хранилища (`Store.ListPublic`,
  `internal/app/mvp_e2e_test.go:327`), а не через `GET /api/v1/catalog`:
  HTTP-маршрут каталога отсутствует (gap TIERL-6). Это частичное покрытие
  требования «unlisted не в каталоге», а не полноценный E2E-сценарий.

### Excess

- Избыточного кода не найдено. Обвязка `mvpUsers`/`mvpFixture`
  (`internal/app/mvp_e2e_test.go:33`, `:53`) и прямые вызовы хранилища — это
  подготовка фикстур, а не отдельная возможность вне требования; удалять код
  не требуется. Проблема в том, что подготовка подменяет проверяемые шаги, а не
  в наличии лишней функциональности.

## Тестовое окружение

`go test ./internal/app -run TestMVP -count=1` компилирует пакет, но в этом
окружении Docker недоступен: все три теста пропускаются через `testDB`
(`internal/app/integration_test.go:56`). Поэтому «зелёный на чистой БД» здесь
подтверждён только структурно (`MigrateDown`/`MigrateUp`) и компиляцией; для
фактического прогона нужен Docker (`TIERL_REQUIRE_DOCKER=1`) или
`TIERL_TEST_DATABASE_URL`. Это ограничение окружения, а не вердикт по коду.

## Решения

1. **gap → change на восстановление требования и задачи на реализацию.**
   Создан `openspec/changes/restore-mvp-e2e` с capability `mvp-e2e`: сквозной
   путь через публичный API, unlisted вне каталога и повторная отправка по
   контракту. В `tasks.md` (раздел 3) вынесены задачи на замену прямого
   `UPDATE users`, `Store.Save` и `similarity.ThinkLikeYou` публичными
   вызовами, на контрактный тест повторной отправки и HTTP-проверку unlisted.
2. **Имя capability обосновано.** Критерий `docs/SPEC.md` §11 пересекает
   `accounts`, `tierlists`, `sharing`, `submissions`, `results` и `catalog`;
   ни один якорь не покрывает весь путь, поэтому в `proposal.md` обосновано
   отдельное имя `mvp-e2e`.
3. **Excess: нет.** Код, не вытекающий из требования, не найден; задача на
   удаление не создаётся.
4. **Связанные follow-up.** `restore-email-verification` (5.1) закрывает
   почтовый путь подтверждения, `restore-catalog` (2.4) — HTTP-проверку
   unlisted в каталоге; `restore-mvp-e2e` связывает их в один приёмочный тест.
5. **Эскалация человеку не требуется:** происхождение требования
   восстанавливается по `docs/SPEC.md` и `api/openapi.yaml`.

## Границы

Код, тесты, контракт и applied-миграции не изменялись. Изменены только
`docs/audits/TIERL-9.md` и файлы `openspec/changes/restore-mvp-e2e/`.

## Итог

Change `restore-mvp-e2e` не оформлен как capability: сквозной тест не способность продукта, а проверка существующих. Исправления вынесены в задачу трекера TIERL-33.
