# Tasks

## 1. Происхождение работы

- [x] 1.1 Сопоставить TIERL-1, 2, 6–12 с целью MVP «аккаунты и безопасность» (`docs/SPEC.md`, раздел 6) и решением `ADR-003`; результат записан в `proposal.md` и `design.md`
- [x] 1.2 Проверить, что требование не исчезло из контракта: сверить `api/openapi.yaml` (сессии, CSRF, `/auth/*`) с восстановленной спекой
- [x] 1.3 Классифицировать работу как необходимую: избыточных маршрутов и мёртвого кода в границах аудита не найдено, удаление не требуется

## 2. Сверка кода со спекой

- [x] 2.1 Проверить серверные сессии: миграция `20261026120000_create_sessions`, хранение SHA-256 токена, отказ просроченной сессии — подтверждено `internal/storage/postgres/sessions_test.go`
- [x] 2.2 Проверить cookie `tierl_session`: `HttpOnly`, `Secure` для HTTPS, `SameSite=Lax`, очистка при выходе — подтверждено `internal/web/api/auth_test.go`
- [x] 2.3 Проверить CSRF: `GET /api/v1/csrf`, заголовок `X-CSRF-Token`, `403 csrf_failed` без токена на POST/PUT/PATCH/DELETE — подтверждено `TestCSRFRequired` и `TestAuthHTTPFlow`
- [x] 2.4 Проверить вход, выход из текущей сессии и со всех устройств — подтверждено `TestAuthHTTPFlow`
- [x] 2.5 Проверить смену пароля: `/auth/change-password` и сброс по токену закрывают сессии, `/me/password` сохраняет текущую — подтверждено `TestAuthChangePasswordRevokesAllSessions` и `TestMePasswordKeepsCurrentSession`
- [x] 2.6 Проверить одноразовые токены email и сброса пароля: 32 байта, SHA-256 в БД, TTL, повторная выдача отменяет предыдущую — подтверждено кодом `internal/auth/email.go` и миграцией `20261027100000_create_one_time_tokens`

## 3. Проверки

- [x] 3.1 Запустить `go test ./...` и убедиться, что все пакеты проходят
- [x] 3.2 Проверить `openspec validate restore-auth-sessions --strict` без ошибок

## Workflow follow-up

- После ревью архивировать изменение, чтобы требования попали в `openspec/specs/authentication/spec.md`.
