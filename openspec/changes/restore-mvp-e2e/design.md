# Design

## Context

См. `proposal.md` — Why. `internal/app/mvp_e2e_test.go` содержит три теста:
`TestMVPFlowRegistrationToResult`, `TestMVPResubmissionContract` и
`TestMVPUnlistedNotInCatalog`. Они компилируются и используют общий
`testDB` (`internal/app/integration_test.go:47`), который перед каждым тестом
откатывает и заново применяет миграции.

При этом ключевые шаги заявленного E2E обходят публичный API:

- `confirmAuthor` (`internal/app/mvp_e2e_test.go:90`) делает
  `UPDATE users SET email_verified = true` напрямую (`:93`), хотя маршрут
  `POST /api/v1/auth/verify-email` существует
  (`internal/web/api/api.go:156`, `internal/web/api/auth.go:verifyEmail`);
- гости сохраняются через `f.store.Save` (`internal/app/mvp_e2e_test.go:191`),
  хотя `POST /api/v1/tierlists/{slug}/submissions` зарегистрирован
  (`internal/web/api/api.go:173`, `internal/web/api/submissions.go:createSubmission`);
- результат считается прямым вызовом `similarity.ThinkLikeYou`
  (`internal/app/mvp_e2e_test.go:229`), хотя `GET /api/v1/results/{id}`
  существует (`internal/web/api/api.go:176`, `internal/web/api/results.go:getResult`);
- повторная отправка проверяется двумя вызовами `f.store.Save`
  (`internal/app/mvp_e2e_test.go:259`, `:267`), а не контрактом повторного
  `POST` (`409 already_submitted`) и `PUT /submissions/mine`
  (`api/openapi.yaml:391`, `api/openapi.yaml:447`).

## Goals / Non-Goals

**Goals:**

- восстановить в OpenSpec требование к сквозному тесту MVP через публичный API;
- зафиксировать, что подтверждение email, ответ гостя, получение результата и
  повторная отправка проверяются на HTTP-уровне, а не прямыми запросами в БД;
- сохранить проверку unlisted вне каталога и доступности unlisted по ссылке;
- сохранить запуск на чистой схеме после миграций.

**Non-Goals:**

- менять продакшен-код, контракт или applied-миграции;
- исправлять сам `internal/app/mvp_e2e_test.go` в этом аудите;
- реализовывать каталог: HTTP-проверка unlisted опирается на
  `restore-catalog`;
- добавлять новые пользовательские возможности вне MVP.

## Decisions

1. **Отдельная capability `mvp-e2e`, а не правка шести существующих.**
   Приёмочный путь пересекает `accounts`, `tierlists`, `sharing`,
   `submissions`, `results` и `catalog`. Один сквозной якорь не дублирует
   требования и явно связывает критерий `docs/SPEC.md` §11 с тестом.

2. **E2E означает публичный HTTP-контракт.** Регистрация и создание уже идут
   через `web.NewHandler`; остальные шаги тоже должны идти через
   `POST /auth/verify-email`, `POST /tierlists/{slug}/submissions` и
   `GET /results/{id}`. Прямой `UPDATE users` и `Store.Save` допустимы только
   для подготовки неизменяемых фикстур, но не для проверяемого бизнес-пути.

3. **Токен подтверждения берётся из тестового канала доставки.** Тест не
   должен угадывать или подменять хэш в `one_time_tokens`; нужен тестовый
   mailer/capture либо явный тестовый API выдачи токена, после чего
   подтверждение выполняется публичным `POST /auth/verify-email`.

4. **Повторная отправка проверяется по контракту, а не по `SaveResult`.**
   Сценарий: первый `POST /submissions` → `201`; повторный `POST` → `409
   already_submitted` с `details.result_id`; `PUT /submissions/mine` → `200` с
   тем же `id`; `total_submissions` не растёт, распределение меняется.
   Существующий `internal/web/api/submissions_contract_test.go` подтверждает,
   что такой контракт реализуем, но TIERL-9 требует проверки в своём
   E2E-файле.

5. **Unlisted проверяется на двух уровнях.** Ссылка `GET /tierlists/{slug}`
   открывается гостем, а отсутствие в каталоге проверяется через
   `GET /api/v1/catalog` после реализации `restore-catalog`. До этого
   допускается текущая проверка `Store.ListPublic`, но она не считается
   полноценной E2E-проверкой каталога.

6. **Чистая БД — обязательное условие.** Тест использует `testDB`, который
   делает `MigrateDown`/`MigrateUp` перед каждым запуском
   (`internal/app/integration_test.go:63`, `:66`), и не зависит от данных
   других тестов.

## Risks / Trade-offs

- [Тестовый mailer может отсутствовать] → добавить capture в тестовой
  обвязке или выдавать токен тестовым методом сервиса, не меняя продакшен-путь
  подтверждения.
- [HTTP-проверка unlisted заблокирована отсутствующим каталогом] → задача
  выполняется после `restore-catalog`; до этого остаётся проверка уровня
  хранилища, явно помеченная как временная.
- [E2E станет медленнее из-за 20 HTTP-ответов] → это приемлемая цена за
  проверку реального контракта; минимальное число ответов задаётся
  `similarity.DefaultMinAnswers`.
- [Дублирование с `submissions_contract_test.go`] → контрактный тест проверяет
  маршруты изолированно, E2E — их совместную работу после регистрации и
  создания опроса; дублирование ограничено одним сценарием.

## Migration Plan

Изменение не требует миграции данных. После архивации требование попадёт в
`openspec/specs/mvp-e2e/spec.md`; откат — удаление нового change до архивации,
поведение сервиса не меняется.
