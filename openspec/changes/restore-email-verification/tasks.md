# Tasks

## 1. Происхождение работы

- [x] 1.1 Сопоставить TIERL-2 с целью MVP «аккаунты и безопасность» (`docs/SPEC.md`, разделы 6 и 11) и решением `ADR-003`; результат записан в `proposal.md` и `design.md`
- [x] 1.2 Сверить поведение с контрактом `api/openapi.yaml`: `/auth/register`, `/auth/verify-email`, `/auth/resend-verification`, `POST /tierlists` и код `email_not_verified`
- [x] 1.3 Проверить, что код TIERL-2 не является избыточным: SMTP, токены, подтверждение и запрет создания следуют требованиям SPEC и ADR-003

## 2. Восстановление спеки

- [x] 2.1 Добавить capability `accounts` с требованиями к письму подтверждения, одноразовому токену, подтверждению, повторной отправке и запрету создания опроса
- [x] 2.2 Связать требования с существующими HTTP-маршрутами, UI `/verify-email` и `403 email_not_verified`

## 3. Сверка кода со спекой

- [x] 3.1 Проверить SMTP и Mailpit: `internal/platform/mailer/smtp.go`, `internal/app/setup.go` и `docker-compose.yml`
- [x] 3.2 Проверить токен 32 байта, SHA-256 в БД, TTL и одноразовость: `internal/auth/email.go` и миграция `20261027100000_create_one_time_tokens`
- [x] 3.3 Проверить регистрацию и отправку письма: `auth.Service.Register` → `notifyVerification` → `issueAndSend`
- [x] 3.4 Проверить подтверждение и повторную отправку: `VerifyEmail`, `ResendVerification`, маршруты `internal/web/api/auth.go` и `internal/web/api/api.go`
- [x] 3.5 Проверить допуск к созданию опроса: `tierlist.Service.Create` и `403 email_not_verified` в `internal/web/api/api.go`
- [x] 3.6 Проверить UI: `web/src/features/auth/RegisterPage.tsx`, `VerifyEmailPage.tsx`, `routes.tsx` и `auth.test.tsx`

## 4. Проверки

- [x] 4.1 Записать вердикт аудита в `docs/audits/TIERL-2.md`
- [x] 4.2 Проверить `openspec validate --strict --all --no-interactive` без ошибок

## 5. Follow-up

- [ ] 5.1 Добавить автоматический тест полного пути регистрация → письмо → переход по токену → подтверждение → создание опроса; сейчас e2e подтверждает email запросом к БД, а `internal/auth/email.go` не имеет отдельного теста. Код и тесты в границах аудита не меняются.
