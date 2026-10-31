# Tasks

## 1. Аудит TIERL-55

- [x] 1.1 Проверить R1: серверные сессии в Postgres, SHA-256 токена в БД, отказ неизвестной и просроченной сессии
- [x] 1.2 Проверить R2: cookie `tierl_session`, `HttpOnly`, `SameSite=Lax`, `Path=/`, `Secure` для HTTPS, очистка при выходе
- [x] 1.3 Проверить R3: `X-CSRF-Token` на POST/PUT/PATCH/DELETE, `403 csrf_failed`, выдача токена через `/csrf` и `/auth/me`
- [x] 1.4 Проверить R4: `POST /auth/logout` закрывает текущую сессию, `POST /auth/logout-all` — все сессии
- [x] 1.5 Проверить R5: смена пароля отзывает прежние сессии, `/me/password` сохраняет текущую, слабый пароль отклоняется
- [x] 1.6 Проверить R6: одноразовые токены email и сброса пароля, 32 байта, SHA-256, TTL, отмена предыдущего токена
- [x] 1.7 Проверить R7: письмо подтверждения при регистрации, SMTP/Mailpit, шаблоны ru и en, устойчивость к сбою доставки
- [x] 1.8 Проверить R8: одноразовый токен email, 32 байта, SHA-256, TTL 24 часа, отказ использованного и просроченного
- [x] 1.9 Проверить R9: `POST /auth/verify-email`, пометка `email_verified`, интерфейс `/verify-email`
- [x] 1.10 Проверить R10: `POST /auth/resend-verification`, ответ `202`, новая ссылка отменяет старую, rate limiting
- [x] 1.11 Проверить R11: создание опроса требует подтверждённого email и отвечает `403 email_not_verified`
- [x] 1.12 Записать вердикты `ok` с доказательствами в `docs/audits/accounts.md`

## 2. Оформление результата

- [x] 2.1 Зафиксировать в аудите отсутствие gap в коде и excess в границах R1–R11
- [x] 2.2 Вынести расхождение TTL сброса пароля между ADR и кодом в замечание вне вердикта R6
- [x] 2.3 Создать change `restore-accounts-audit` с дельтой capability `accounts` без переименования якоря
- [x] 2.4 Использовать ADDED-дельту, потому что в базовой живой спеке capability `accounts` отсутствует
- [x] 2.5 Подтвердить, что код, тесты, контракт и applied-миграции не менялись
- [x] 2.6 Зафиксировать gap прослеживаемости: отсутствие capability `accounts` в базовой живой спеке закрывается восстановлением требований

## 3. Проверки

- [x] 3.1 Выполнить `go mod download`
- [x] 3.2 Запустить `go test ./internal/auth ./internal/web/api ./internal/tierlist ./internal/storage/postgres -count=1`
- [x] 3.3 Проверить строки вердиктов R1–R11 в `docs/audits/accounts.md`
- [x] 3.4 Проверить `openspec validate --strict --all --no-interactive`
