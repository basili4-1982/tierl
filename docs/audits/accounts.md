# Аудит TIERL-55 — Аккаунты и безопасность

## Контекст

Задача: TIERL-55 — аудит способности «Аккаунты и безопасность» (якорь цели
`accounts`). Проверяются требования R1–R11 из постановки: серверные сессии,
cookie, CSRF, выход, смена пароля, одноразовые токены, подтверждение email,
повторная отправка и запрет создания опроса без подтверждённого email.

Происхождение требований восстанавливается по `docs/SPEC.md` §6 (регистрация,
сессии, CSRF, подтверждение email и сброс пароля), `docs/adr/ADR-003-auth.md`,
`api/openapi.yaml` (`/csrf`, `/auth/*`, `/me/password`) и архивным change
`openspec/changes/archive/2026-10-28-restore-auth-sessions` и
`openspec/changes/archive/2026-10-28-restore-email-verification`.

В базе постановки живая спека `openspec/specs/accounts/spec.md` отсутствует,
поэтому у требований R1–R11 нет цепочки «цель → спека → код» и сверка работы
их не видит. Аудит проверяет фактический код против R1–R11 и не меняет
поведение сервиса. Change `openspec/changes/restore-accounts-audit`
восстанавливает capability `accounts` ADDED-дельтой без переименования якоря.

## Вердикт

- R1: ok — internal/auth/session.go:SessionManager и internal/storage/postgres/sessions.go:SessionStore хранят серверные сессии в Postgres; cookie несёт случайный токен, в БД лежит SHA-256, просроченная сессия отклоняется.
- R2: ok — internal/web/api/auth.go:setSessionCookie выставляет `tierl_session` с `HttpOnly`, `SameSite=Lax`, `Path=/` и `Secure` для HTTPS; `clearSessionCookie` очищает её при выходе.
- R3: ok — internal/web/api/api.go:ServeHTTP требует валидный `X-CSRF-Token` для POST/PUT/PATCH/DELETE и отвечает `403 csrf_failed`; токен выдают `GET /csrf` и `GET /auth/me`.
- R4: ok — internal/web/api/auth.go:logout закрывает текущую сессию, а internal/web/api/auth.go:logoutAll удаляет все сессии пользователя и очищает cookie.
- R5: ok — internal/auth/service.go:ChangePasswordKeepingCurrent и internal/storage/postgres/users.go:UpdatePasswordAndDeleteSessions меняют хэш и удаляют прежние сессии; сброс по токену в internal/auth/email.go:ResetPassword отзывает все сессии.
- R6: ok — internal/auth/email.go:newOneTimeToken создаёт 32-байтовый токен, internal/auth/email.go:hashOneTimeToken хранит только SHA-256, а internal/storage/postgres/tokens.go:Issue инвалидирует предыдущий активный токен.
- R7: ok — internal/auth/service.go:Register создаёт аккаунт с `email_verified = false` и вызывает internal/auth/email.go:notifyVerification; сбой доставки логируется без токена и ссылки.
- R8: ok — internal/auth/email.go:VerifyEmail проверяет 24-часовой одноразовый токен через internal/storage/postgres/tokens.go:Consume; использованный, просроченный или неизвестный токен отклоняется.
- R9: ok — internal/web/api/auth.go:verifyEmail принимает `POST /auth/verify-email`, а web/src/features/auth/VerifyEmailPage.tsx:VerifyEmailPage показывает успех или ошибку по ссылке `/verify-email`.
- R10: ok — internal/web/api/auth.go:resendVerification отвечает `202` для корректного email; internal/auth/email.go:ResendVerification выдаёт новый токен, а rate limiting настроен в internal/app/setup.go:buildDeps.
- R11: ok — internal/tierlist/service.go:Create возвращает `ErrEmailNotVerified`, а internal/web/api/api.go:writeServiceError переводит его в `403 email_not_verified`.

## Доказательства по требованиям

### R1 — Серверные сессии в Postgres

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Сессии хранятся на сервере | `migrations/20261026120000_create_sessions.up.sql`, `internal/storage/postgres/sessions.go:SessionStore` | Таблица `sessions` с `token_hash`, `user_id`, `created_at`, `expires_at`; уникальный первичный ключ по хэшу |
| В cookie случайный токен, в БД SHA-256 | `internal/auth/session.go:SessionManager.Create`, `internal/auth/session.go:HashToken` | `Create` генерирует 32 случайных байта; `HashToken` возвращает hex SHA-256, который и сохраняется в `token_hash` |
| Неизвестная и просроченная сессия не аутентифицируют | `internal/auth/session.go:SessionManager.Lookup`, `internal/storage/postgres/sessions.go:GetByTokenHash`, `internal/web/api/auth.go:currentSession` | SQL-фильтр `expires_at > now` и `pgx.ErrNoRows` → `ErrSessionNotFound`; HTTP-слой отвечает `401 unauthorized` |
| Тесты | `internal/auth/session_test.go:TestSessionManagerLifecycle`, `internal/storage/postgres/sessions_test.go:TestSessionStore` | Проверены создание, поиск, отказ после удаления и истечения срока, а также удаление только нужных сессий |

### R2 — Защищённая cookie сессии

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Имя и атрибуты cookie | `internal/auth/session.go:SessionCookieName`, `internal/web/api/auth.go:setSessionCookie` | Cookie называется `tierl_session`, содержит `HttpOnly: true`, `SameSite: Lax`, `Path: "/"` |
| `Secure` для HTTPS | `internal/web/api/auth.go:secureRequest` | `TLS != nil` и `X-Forwarded-Proto: https` дают `Secure`; локальный HTTP оставляет флаг выключенным для разработки |
| Выход очищает cookie | `internal/web/api/auth.go:logout`, `internal/web/api/auth.go:logoutAll`, `internal/web/api/auth.go:clearSessionCookie` | Cookie перезаписывается с пустым значением, `MaxAge: -1` и прошедшим `Expires` |
| Тесты | `internal/web/api/auth_test.go:TestAuthHTTPFlow` | Проверены установка cookie, вход и `401` после выхода; атрибуты cookie проверяет `sessionFrom` |

### R3 — CSRF-токен на изменяющих запросах

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Проверка на небезопасных методах | `internal/web/api/api.go:isUnsafeMethod`, `internal/web/api/api.go:ServeHTTP` | POST, PUT, PATCH и DELETE проходят проверку `a.csrf.Valid` до маршрутизации |
| Отказ без токена или с неверным токеном | `internal/web/api/api.go:ServeHTTP` | Ответ `403` с кодом `csrf_failed`; обработчик запроса не вызывается |
| Выдача токена | `internal/web/api/auth.go:getCSRF`, `internal/web/api/auth.go:me` | `GET /api/v1/csrf` и `GET /api/v1/auth/me` возвращают `csrf_token` |
| Токен не хранится на сервере | `internal/auth/session.go:CSRF`, `internal/auth/session.go:Token`, `internal/auth/session.go:Valid` | Токен состоит из nonce и HMAC-SHA256, проверяется только секретом процесса |
| Безопасные методы не требуют токена | `internal/web/api/api.go:ServeHTTP` | GET не входит в `isUnsafeMethod` и не проверяется |
| Тесты | `internal/web/api/api_test.go:TestCSRFRequired`, `internal/auth/session_test.go:TestCSRFTokens`, `internal/web/api/auth_test.go:TestAuthHTTPFlow` | Проверены отсутствующий и неверный токен, GET без токена и токен из `/auth/me` |

### R4 — Выход из текущей сессии и со всех устройств

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Маршруты выхода | `internal/web/api/api.go:New` (`POST /auth/logout`, `POST /auth/logout-all`), `internal/web/api/auth.go:logout`, `internal/web/api/auth.go:logoutAll` | Оба маршрута требуют действующую сессию и очищают cookie |
| Только текущая сессия | `internal/auth/session.go:SessionManager.Delete`, `internal/storage/postgres/sessions.go:Delete` | Удаление идёт по `token_hash` текущей cookie; другие сессии остаются |
| Все устройства | `internal/auth/session.go:SessionManager.DeleteAll`, `internal/storage/postgres/sessions.go:DeleteByUserID` | SQL удаляет все строки `sessions` с нужным `user_id` |
| Прежняя cookie не работает | `internal/web/api/auth.go:currentSession`, `internal/storage/postgres/sessions.go:GetByTokenHash` | После удаления сессии поиск даёт `ErrSessionNotFound`, HTTP-слой отвечает `401` |
| Тесты | `internal/web/api/auth_test.go:TestAuthHTTPFlow`, `internal/auth/session_test.go:TestSessionManagerDeleteAll` | Проверено, что logout не трогает чужие сессии, а logout-all закрывает все сессии пользователя |

### R5 — Смена пароля инвалидирует прежние сессии

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Смена через профиль сохраняет текущую сессию | `internal/web/api/auth.go:changeMyPassword`, `internal/auth/service.go:ChangePasswordKeepingCurrent` | Обработчик передаёт raw-токен текущей cookie в сервис; cookie не очищается, ответ `200` |
| Удаление остальных сессий | `internal/storage/postgres/users.go:UpdatePasswordAndDeleteSessions`, `internal/auth/session.go:SessionManager.DeleteOthersByUserID` | В одной транзакции обновляется `password_hash` и удаляются сессии с `token_hash <> keepTokenHash` |
| Смена через `/auth/change-password` и `PATCH /auth/password` закрывает все сессии | `internal/web/api/api.go:New`, `internal/web/api/auth.go:changePassword`, `internal/auth/service.go:ChangePassword` | Обе точки используют `ChangePassword`, затем `storePassword` вызывает `revokeSessions` |
| Сброс по ссылке закрывает все сессии | `internal/auth/email.go:ResetPassword`, `internal/auth/service.go:revokeSessions` | Перед обновлением хэша вызывается `LogoutAll` или `DeleteByUserID` |
| Слабый пароль не меняет состояние | `internal/auth/validate.go:ValidatePassword`, `internal/auth/service.go:ChangePasswordKeepingCurrent`, `internal/web/api/auth.go:changeMyPassword` | Валидация выполняется до хэширования и изменения сессий; обработчик отдаёт `400 validation_failed` |
| Тесты | `internal/web/api/auth_test.go:TestMePasswordKeepsCurrentSession`, `internal/web/api/auth_test.go:TestAuthChangePasswordRevokesAllSessions`, `internal/auth/session_test.go:TestChangePasswordRevokesSessions` | Проверены сохранение текущей сессии, отзыв всех остальных, отказ старому паролю и отказ слабому паролю без изменения сессий |

### R6 — Одноразовые токены email и сброса пароля

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Токен из 32 байт | `internal/auth/email.go:newOneTimeToken`, `internal/auth/email.go:oneTimeTokenBytes` | `io.ReadFull` читает ровно 32 случайных байта и кодирует их в base64url |
| В БД только SHA-256 | `internal/auth/email.go:newOneTimeToken`, `migrations/20261027100000_create_one_time_tokens.up.sql` | В `OneTimeToken` сохраняется `TokenHash`, колонка `token_hash bytea` с проверкой `octet_length = 32` |
| Ограниченный срок | `internal/auth/email.go:VerifyEmailTTL`, `internal/auth/email.go:ResetPasswordTTL` | Подтверждение email — 24 часа, сброс пароля — 1 час; `expires_at` проверяется при `Consume` |
| Повторная выдача отменяет предыдущий токен | `internal/storage/postgres/tokens.go:Issue` | Транзакция блокирует пользователя, помечает `used_at` у прежних активных токенов и вставляет новый |
| Использованный и просроченный токен отклоняются | `internal/storage/postgres/tokens.go:Consume` | `UPDATE ... WHERE used_at IS NULL AND expires_at > now RETURNING`; отсутствие строки даёт `ErrInvalidToken` |
| Тесты | `internal/auth/email_test.go:TestEmailVerificationFullPath`, `internal/auth/email_test.go:TestVerifyEmailRejectsInvalidTokens`, `internal/auth/email_test.go:TestResendVerificationInvalidatesPreviousToken` | Проверены длина токена, SHA-256 в хранилище, TTL, одноразовость и отмена предыдущей ссылки |

### R7 — Письмо подтверждения при регистрации

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Аккаунт создаётся неподтверждённым | `internal/auth/service.go:Register`, `migrations/20261022110000_create_users.up.sql` | `User.EmailVerified` по умолчанию `false`, колонка `email_verified boolean NOT NULL DEFAULT false` |
| Письмо со ссылкой | `internal/auth/email.go:notifyVerification`, `internal/auth/email.go:issueAndSend`, `internal/auth/email.go:link` | После создания пользователя выпускается токен и отправляется письмо с URL `/verify-email?token=...` |
| SMTP и Mailpit | `internal/platform/mailer/smtp.go:SMTPSender`, `internal/platform/mailer/mailer.go:NewSender`, `docker-compose.yml` | При заданном SMTP host используется `SMTPSender`; в compose есть сервис `mailpit` и переменные `TIERL_SMTP_*` |
| Шаблоны ru и en | `internal/platform/mailer/templates.go:letters` | Для `VerifyEmail` и `ResetPassword` есть варианты `LangRU` и `LangEN` в text и HTML |
| Сбой доставки не отменяет регистрацию и не раскрывает токен | `internal/auth/service.go:notifyVerification`, `internal/platform/mailer/async.go:AsyncSender.Send`, `internal/platform/mailer/mailer.go:LogSender.Send` | `Register` не возвращает ошибку доставки; логируются только `user_id`/получатель/тема и ошибка, без тела письма и ссылки |
| Тесты | `internal/auth/email_test.go:TestEmailVerificationFullPath`, `internal/auth/email_test.go:TestRegisterSurvivesDeliveryFailure` | Проверены письмо, отсутствие подтверждения до перехода по ссылке и успешная регистрация при недоступном SMTP |

### R8 — Одноразовый токен подтверждения email

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| 32 случайных байта | `internal/auth/email.go:newOneTimeToken` | Размер задан `oneTimeTokenBytes = 32`; токен читается из `s.rand` через `io.ReadFull` |
| В БД только SHA-256 | `internal/auth/email.go:newOneTimeToken`, `internal/storage/postgres/tokens.go:Issue` | Хэш вычисляется через `sha256.Sum256`, сырое значение остаётся только в письме |
| Срок 24 часа | `internal/auth/email.go:VerifyEmailTTL`, `internal/auth/email.go:notifyVerification` | Для `PurposeVerifyEmail` используется `VerifyEmailTTL = 24 * time.Hour` |
| Использованный, просроченный или неизвестный токен отклоняется | `internal/auth/email.go:VerifyEmail`, `internal/storage/postgres/tokens.go:Consume` | Проверяются формат, назначение, `used_at IS NULL` и `expires_at > now`; иначе `ErrInvalidToken` |
| Тесты | `internal/auth/email_test.go:TestEmailVerificationFullPath`, `internal/auth/email_test.go:TestVerifyEmailRejectsInvalidTokens` | Повторное использование, истечение срока, пустой, короткий и неизвестный токен отклоняются |

### R9 — Подтверждение email по ссылке

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| HTTP-маршрут подтверждения | `internal/web/api/api.go:New` (`POST /auth/verify-email`), `internal/web/api/auth.go:verifyEmail` | Обработчик принимает `token`, вызывает `Accounts.VerifyEmail` и отвечает `204` при успехе |
| Пометка email подтверждённым | `internal/auth/email.go:VerifyEmail`, `internal/storage/postgres/users.go:MarkEmailVerified` | После успешного `Consume` вызывается `MarkEmailVerified`, обновляющий `email_verified = true` |
| UI показывает результат | `web/src/features/auth/routes.tsx`, `web/src/features/auth/VerifyEmailPage.tsx:VerifyEmailPage` | Страница `/verify-email` вызывает API и показывает `role="alert"` для ошибки или `role="status"` для успеха |
| Тесты | `internal/auth/email_test.go:TestEmailVerificationFullPath`, `internal/app/mvp_e2e_test.go:TestMVPFlowRegistrationToResult`, `web/src/features/auth/auth.test.tsx` | Проверены подтверждение по токену и создание опроса после него; UI-тесты покрывают валидную и неверную ссылку |

### R10 — Повторная отправка подтверждения

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Маршрут и ответ `202` | `internal/web/api/api.go:New` (`POST /auth/resend-verification`), `internal/web/api/auth.go:resendVerification` | Для корректного email обработчик отвечает `202 Accepted` |
| Неизвестный или уже подтверждённый email не раскрывается | `internal/auth/email.go:ResendVerification` | Для `ErrUserNotFound` и `u.EmailVerified` сервис молча возвращает `nil`; письмо не отправляется |
| Новая ссылка отменяет старую | `internal/auth/email.go:issueAndSend`, `internal/storage/postgres/tokens.go:Issue` | Новая выдача помечает прежние активные токены `used_at`; тест подтверждает отказ старой ссылке |
| Rate limiting | `internal/web/api/auth.go:allowEmail`, `internal/app/setup.go:buildDeps` | Применяются middleware по IP и лимит по email для `resend_verification`; при превышении ответ `429` |
| Тесты | `internal/auth/email_test.go:TestResendVerificationInvalidatesPreviousToken`, `internal/auth/email_test.go:TestEmailVerificationFullPath` | Проверены новая ссылка, отказ предыдущей, отсутствие письма для подтверждённого и неизвестного адреса |

### R11 — Создание опроса требует подтверждённого email

| Что проверено | Где реализовано | Чем подтверждается |
|---|---|---|
| Проверка подтверждения в сервисе | `internal/tierlist/service.go:Create`, `internal/tierlist/service.go:Profile` | `Create` загружает профиль автора и возвращает `ErrEmailNotVerified`, если `EmailVerified` равно `false` |
| Профиль берётся из аккаунта | `internal/app/setup.go:authUsers.Profile`, `internal/storage/postgres/users.go:GetByID` | `authUsers.Profile` читает `email_verified` из `users` и передаёт его в `tierlist.Profile` |
| HTTP-ответ `403 email_not_verified` | `internal/web/api/api.go:writeServiceError` | Ошибка `tierlist.ErrEmailNotVerified` переводится в `403` с кодом `email_not_verified` до сохранения опроса |
| Тесты | `internal/tierlist/service_test.go:TestCreate_Errors`, `internal/web/api/api_test.go:TestCreate_Errors` (сценарий `email not verified`), `internal/app/mvp_e2e_test.go:TestMVPFlowRegistrationToResult` | Проверено, что неподтверждённый пользователь получает отказ, а после подтверждения опрос создаётся |

## Gap

Gap в коде в границах R1–R11 не найдено: все заявленные требования
реализованы и подтверждены чтением кода и тестами.

Зафиксирован gap прослеживаемости: в базовой живой спеке отсутствует
capability `accounts`, поэтому R1–R11 не видны сверке работы. Заявлено:
способность «Аккаунты и безопасность» должна иметь живую спеку с якорем
`accounts`; в коде требования есть, а в живой спеке capability не было.
Решение — `openspec/changes/restore-accounts-audit` с ADDED-дельтой,
восстанавливающей одиннадцать требований.

Вне вердикта R1–R11 зафиксировано одно расхождение документации, не являющееся
отсутствующим требованием:

- `docs/adr/ADR-003-auth.md` называет TTL сброса пароля 30 минут, а
  `internal/auth/email.go:ResetPasswordTTL` задаёт 1 час. Требование R6
  ограничивает лишь наличие срока действия и не задаёт конкретное значение,
  поэтому вердикт R6 остаётся `ok`; это кандидат на отдельное решение о
  синхронизации ADR и конфигурации, а не gap R1–R11.

## Excess

Excess в границах R1–R11 не найдено:

- `POST /auth/change-password` и `PATCH /auth/password`
  (`internal/web/api/api.go:New`) реализуют R5 — смену пароля с отзывом всех
  сессий; их назначение подтверждено архивным change
  `2026-10-28-restore-auth-sessions`.
- `internal/auth/service.go:SetPassword` и
  `internal/auth/service.go:ResetPasswordForUser` не добавляют HTTP-поведения
  сверх R5 и не используются production-путями; это внутренние helper-методы,
  а не отдельная возможность. Удаление кода в рамках аудита не требуется.
- Альтернативные обёртки `SessionManager.Authenticate`, `Revoke`, `RevokeAll`
  и alias-методы `SessionStore` реализуют те же R1/R4 и не создают нового
  поведения.

Удалять код не требуется.

## Решения

1. **R1–R11 — `ok`, задач на реализацию или удаление кода нет.** Поведение
   сервиса, тесты, контракт и applied-миграции не меняются.
2. **Change на восстановление требования создан.**
   `openspec/changes/restore-accounts-audit` с ADDED-дельтой capability
   `accounts` (якорь не переименован). В базовой живой спеке capability
   отсутствует, поэтому MODIFIED был бы неприменим и не восстановил бы
   требования; ADDED добавляет R1–R11 и восстанавливает цепочку
   «цель → спека → код».
3. **Gap в коде не найден, задача на реализацию отсутствующего требования не
   создаётся.** Восстановление живой спеки выполняет change из решения 2.
   Расхождение TTL сброса пароля в ADR вынесено в замечание вне вердикта:
   R6 не фиксирует 30 минут.
4. **Excess не найден, задача на удаление кода не создаётся.** Альтернативные
   маршруты и helper-методы относятся к уже заявленному поведению R5/R1/R4.

## Проверки

- `go mod download` — зависимости Go загружены.
- `go test ./internal/auth ./internal/web/api ./internal/tierlist ./internal/storage/postgres -count=1`
  — пакеты прошли; поведение кода не менялось.
- `test -f docs/audits/accounts.md` и grep-проверка строк `- R1:`…`- R11:` по
  шаблону задачи — пройдены.
- `openspec validate --strict --all --no-interactive` — все элементы прошли.

## Границы

Код, тесты, контракт и применённые миграции не изменялись. Изменены только
`docs/audits/accounts.md` и файлы
`openspec/changes/restore-accounts-audit/`.
