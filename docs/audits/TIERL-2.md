# Аудит TIERL-2 — Mailer и подтверждение email

## Контекст

Задача: Mailer и подтверждение email (PR 58). Заявлено: отправка писем через
SMTP (Mailpit локально), одноразовый токен 32 байта, в БД только хэш, TTL,
повторная отправка. Критерий: регистрация → письмо → подтверждение → создание
опроса разрешено; без подтверждения — `403`.

Происхождение требования восстанавливается: `docs/SPEC.md` (разделы 6 и 11),
`docs/adr/ADR-003-auth.md`, `api/openapi.yaml` (`/auth/register`,
`/auth/verify-email`, `/auth/resend-verification`, `POST /tierlists`).
Канонической capability в OpenSpec не было, поэтому оформлен change
`restore-email-verification` с capability `accounts`.

## Вердикт

- TIERL-2: ok — internal/auth/email.go:VerifyEmail; реализованы SMTP-письмо, токен 32 байта с SHA-256 в БД, TTL, повторная отправка, подтверждение email и запрет создания опроса без подтверждения (`403 email_not_verified`).

## Доказательства по пунктам описания

| Заявлено | Где реализовано | Что подтверждает |
|---|---|---|
| SMTP и Mailpit локально | `internal/platform/mailer/smtp.go:SMTPSender.Send`, `internal/platform/mailer/mailer.go:NewSender`, `internal/app/setup.go:buildAuth`, `docker-compose.yml` (сервис `mailpit`), `.env.example` | Письма уходят по SMTP; без `TIERL_SMTP_HOST` используется log-sender, локально — Mailpit `localhost:1025` |
| Токен 32 байта | `internal/auth/email.go:newOneTimeToken`, константа `oneTimeTokenBytes = 32` | Токен читается из `crypto/rand` ровно 32 байта |
| В БД только хэш | `internal/auth/email.go:newOneTimeToken` (SHA-256), `migrations/20261027100000_create_one_time_tokens.up.sql` (`token_hash bytea`, `octet_length = 32`) | Сырой токен не сохраняется, в `one_time_tokens` только SHA-256 |
| TTL и одноразовость | `internal/auth/email.go:VerifyEmailTTL` (24 часа), `internal/storage/postgres/tokens.go:Consume` (`used_at IS NULL AND expires_at > $3`) | Просроченный и использованный токен не проходят |
| Повторная отправка | `internal/auth/email.go:ResendVerification`, `internal/storage/postgres/tokens.go:Issue`, `internal/web/api/auth.go:resendVerification`, маршрут в `internal/web/api/api.go` | Новая выдача инвалидирует предыдущую; ответ `202` не раскрывает существование адреса |
| Регистрация → письмо | `internal/auth/service.go:Register` → `internal/auth/email.go:notifyVerification` → `issueAndSend`, шаблоны `internal/platform/mailer/templates.go` | После регистрации отправляется ссылка `/verify-email?token=...`; сбой доставки не отменяет аккаунт |
| Подтверждение email | `internal/auth/email.go:VerifyEmail`, `internal/storage/postgres/users.go:MarkEmailVerified`, `internal/web/api/auth.go:verifyEmail` | Токен потребляется, `email_verified` становится `true` |
| UI подтверждения | `web/src/features/auth/VerifyEmailPage.tsx`, `web/src/features/auth/routes.tsx`, `web/src/features/auth/RegisterPage.tsx`, `web/src/features/auth/auth.test.tsx` | Маршрут `/verify-email` и сообщение после регистрации; страница показывает успех/ошибку |
| Без подтверждения — 403 | `internal/tierlist/service.go:Create` (`ErrEmailNotVerified`), `internal/web/api/api.go:writeServiceError` (`403 email_not_verified`), `internal/app/mvp_e2e_test.go:TestMVPFlowRegistrationToResult` | Неподтверждённый автор получает `403 email_not_verified`; после подтверждения создание разрешено |

## Тестовое покрытие

- `internal/app/mvp_e2e_test.go:TestMVPFlowRegistrationToResult` проверяет
  отказ `403 email_not_verified` и успешное создание после подтверждения.
- `web/src/features/auth/auth.test.tsx` проверяет страницу `/verify-email`.
- Ограничение: полный путь «регистрация → письмо → переход по токену →
  подтверждение» не автоматизирован; e2e подтверждает email прямым запросом к
  БД, а у `internal/auth/email.go` и `internal/platform/mailer` нет отдельных
  тестов. Это не отсутствие кода по TIERL-2, но остаточный риск; follow-up
  вынесен в `openspec/changes/restore-email-verification/tasks.md` (5.1).

## Решения

1. **Требование было и исчезло из OpenSpec — восстановить.** Создан change
   `restore-email-verification` с capability `accounts`: письмо подтверждения,
   токен 32 байта/SHA-256/TTL, подтверждение, повторная отправка и запрет
   создания опроса без подтверждения. Поведение кода не меняется.
2. **Gap в коде: нет.** Все заявленные обработчики, маршрут, UI и проверка
   `403` найдены; вердикт `ok`.
3. **Excess: нет.** Сброс пароля, асинхронная очередь писем и шаблоны ru/en
   следуют `docs/SPEC.md` и `ADR-003`; сброс пароля уже восстанавливается
   change `restore-auth-sessions`. Удалять код не требуется.
4. **Тестовый gap: вынесен как задача на реализацию.** В change добавлен
   follow-up 5.1 на автоматический тест полного почтового пути; код и тесты в
   границах этого аудита не меняются.
5. **Эскалация человеку не требуется:** происхождение требования
   восстанавливается по SPEC, ADR-003 и контракту.

## Границы

Код, тесты, контракт и applied-миграции не изменялись. Изменены только
`docs/audits/TIERL-2.md` и файлы `openspec/changes/restore-email-verification/`.
