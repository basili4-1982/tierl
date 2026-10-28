# Аудит TIERL-33 — E2E MVP через публичный API без обходов БД

## Контекст

Задача: TIERL-33 (PR 88) — доработка из аудита `TIERL-9` (change
`restore-mvp-e2e`). Заявлено два пункта в файле-результате
`internal/app/mvp_e2e_test.go`:

1. заменить прямой `UPDATE users` в `confirmAuthor` на
   `POST /api/v1/auth/verify-email` с токеном из тестового канала доставки;
   проверка — тест не содержит ручных изменений бизнес-состояния в БД;
2. заменить `f.store.Save` для гостей на
   `POST /api/v1/tierlists/{slug}/submissions` с анонимными cookie, а расчёт
   результата — на `GET /api/v1/results/{id}`; проверка — 20 гостей проходят
   через HTTP, результат получен через API.

Требование происходит из `docs/SPEC.md`, раздел 11 «Критерии готовности MVP»
(путь автора без ручных действий в БД, ответ гостя без регистрации, unlisted
вне каталога). Отдельные шаги уже описаны в capability `accounts`
(подтверждение email), `tierlists` (создание и чтение опроса), `submissions`
(приём и замена ответа), `results` (результат и статистика) и `catalog`
(unlisted не попадает в каталог), но сквозного требования в OpenSpec не было,
а change `restore-mvp-e2e` из аудита `TIERL-9` в репозитории отсутствует.
Поэтому восстановлена capability `mvp-e2e`, которая связывает эти шаги в один
приёмочный путь и не вводит нового поведения продукта.

## Вердикт

- TIERL-33: ok — internal/app/mvp_e2e_test.go:confirmAuthor и TestMVPFlowRegistrationToResult: подтверждение email идёт через `POST /api/v1/auth/verify-email` токеном из `mailertest.Capture`, 20 гостей отвечают через `POST /api/v1/tierlists/{slug}/submissions` с анонимной cookie, а результат читается через `GET /api/v1/results/{id}` и `/stats`; прямых изменений бизнес-состояния в БД в файле нет.

## Доказательства по пунктам описания

| Заявлено | Где реализовано | Чем подтверждается |
|---|---|---|
| Подтверждение email через публичный API | `internal/app/mvp_e2e_test.go:113:confirmAuthor`, `:135:verificationToken` | `confirmAuthor` берёт одноразовый токен из `f.mail.Last()` (`internal/platform/mailer/mailertest/capture.go:47`) и вызывает `POST /api/v1/auth/verify-email` (`mvp_e2e_test.go:116`); маршрут зарегистрирован в `internal/web/api/api.go:156`, обработчик — `internal/web/api/auth.go:337:verifyEmail`, сервис — `internal/auth/email.go:69:VerifyEmail`. Успех проверяется по `204`, затем вход через `POST /api/v1/auth/login` (`mvp_e2e_test.go:122`) и `email_verified == true` (`:128`) |
| Токен из тестового канала доставки | `internal/app/mvp_e2e_test.go:135:verificationToken` | Письмо захватывается `mailertest.Capture` в `newMVPFixture` (`mvp_e2e_test.go:70`, `:76`); токен извлекается регулярным выражением из `Text`/`HTML` письма (`:29`, `:143`) |
| Нет ручных изменений бизнес-состояния в БД | `internal/app/mvp_e2e_test.go` целиком | В файле нет `UPDATE users`, `email_verified = true`, `f.store.Save` и `similarity.ThinkLikeYou`; единственное обращение к хранилищу — чтение `f.store.ListPublic` (`:465`) в unlisted-тесте, без изменения состояния |
| 20 гостей отвечают через HTTP | `internal/app/mvp_e2e_test.go:292:TestMVPFlowRegistrationToResult` | Константа `guests = 20` (`:322`), цикл `for i := range guests` (`:324`) и `POST /api/v1/tierlists/{slug}/submissions` без пользователя (`:325`) на каждый ответ; успех — `201` и наличие cookie `tierl_anon` (`:327`, `:333`, `:241:mvpAnonCookie`). Обработчик — `internal/web/api/submissions.go:76:createSubmission`, маршрут — `internal/web/api/api.go:173` |
| Результат получен через API | `internal/app/mvp_e2e_test.go:341`, `:354` | `GET /api/v1/results/{id}` (`:341`, обработчик `internal/web/api/results.go:51:getResult`) и `GET /api/v1/results/{id}/stats` (`:354`, обработчик `internal/web/api/results.go:100:getResultStats`, маршруты `internal/web/api/api.go:176-177`); проверяются `submission.id`, `tierlist.submissions_count`, `total_submissions = 20`, `like_you_percent = 100` и распределение по тирам (`:345-358`) |
| Повторная отправка по контракту | `internal/app/mvp_e2e_test.go:368:TestMVPResubmissionContract` | Первый `POST` (`:384`), повторный `POST` с ожиданием `409 already_submitted` и `details.result_id` (`:392`), замена через `PUT /submissions/mine` с сохранением `id` (`:402`); результат и статистика снова читаются через `/results/{id}` и `/stats` (`:417`, `:428`) |

## Gap

Gap по заявленным пунктам нет: обработчик подтверждения email, маршрут
`POST /auth/verify-email`, выдача и проверка анонимной cookie, маршруты
`POST`/`PUT` ответов и `GET /results/{id}` существуют, а E2E-тест вызывает
именно их. Прямой `UPDATE users` и `f.store.Save` из сценария удалены.

## Excess

Excess нет. Обвязка `mvpFixture`, `mvpUsers`, `mvpAnonCookie` и
`mvpAssertDistribution` (`internal/app/mvp_e2e_test.go:33`, `:53`, `:241`,
`:265`) — это подготовка фикстур и проверки ответа, а не отдельная
возможность вне требования. Код, не следующий ни из какого требования, в
границах TIERL-33 не найден.

## Замечания вне вердикта

- `TestMVPUnlistedNotInCatalog` по-прежнему читает каталог напрямую через
  `f.store.ListPublic` (`internal/app/mvp_e2e_test.go:461-465`) с пометкой
  `TEMPORARY(TIERL-33)`. Это чтение, а не изменение бизнес-состояния, и оно
  связано с отсутствием маршрута `GET /api/v1/catalog`: HTTP-проверка вынесена
  в `openspec/changes/add-catalog/tasks.md`, пункт 2.4. Поэтому это не gap
  заявленных пунктов TIERL-33, а известный follow-up каталога.
- `go test ./internal/app -run TestMVP -count=1` компилирует пакет и проходит,
  но в этом окружении Docker недоступен: все три MVP-теста пропускаются через
  `testDB` (`internal/app/integration_test.go:56`). Фактический прогон на
  Postgres требует Docker (`TIERL_REQUIRE_DOCKER=1`) или
  `TIERL_TEST_DATABASE_URL`; это ограничение окружения, а не дефект кода.

## Решения

- **gap/excess в коде не найдены.** Реализация соответствует заявленным
  пунктам, задача на реализацию или удаление кода не нужна.
- **Требование отсутствовало в OpenSpec — восстановлено change'ем.** Создан
  `openspec/changes/restore-mvp-e2e` с capability `mvp-e2e`: пути автора и
  гостя через публичный API, повторная отправка по контракту и unlisted вне
  каталога. Имя `mvp-e2e` новое с обоснованием: оно пересекает якоря
  `accounts`, `tierlists`, `submissions`, `results` и `catalog`, и ни один из
  них не покрывает весь сквозной путь.
- **Follow-up остаётся в `add-catalog` (2.4):** заменить чтение
  `Store.ListPublic` в `TestMVPUnlistedNotInCatalog` на HTTP-маршрут каталога
  после его появления.
- **Эскалация человеку не требуется:** происхождение требования
  восстанавливается по `docs/SPEC.md` и `api/openapi.yaml`.

## Границы

Код, тесты, контракт и применённые миграции не изменялись. Изменены
`docs/audits/TIERL-33.md` и файлы `openspec/changes/restore-mvp-e2e/`.
