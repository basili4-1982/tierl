# API-контракт

`api/openapi.yaml` - единственный источник правды по HTTP API (OpenAPI 3.1).
Бэкенд и фронт работают от него параллельно; изменения контракта идут
отдельными небольшими PR.

Ключевые договорённости:

- префикс `/api/v1`, JSON;
- сессия в cookie `tierl_session`, анонимный участник - cookie `tierl_anon`
  (HttpOnly, SameSite=Lax; Secure, в локальной разработке отключается);
- сессия серверная, со скользящим продлением и абсолютным потолком;
- CSRF: токен из `GET /csrf` отправляется в заголовке `X-CSRF-Token` на все
  POST/PUT/PATCH/DELETE, кроме публичных `/auth/login` и `/auth/register`;
- ошибки: `{ "code": "...", "message": "...", "details": {...} }`;
- пагинация курсором: `?limit=&cursor=` -> `{ items, next_cursor }`;
- видимость опроса: `public` (в каталоге) или `unlisted` (только по ссылке).

## Валидация

```bash
npx @redocly/cli lint api/openapi.yaml
```

Правила - в `redocly.yaml` в корне. Команда не требует установки, только Node.js.
Для CI достаточно этой же строки.

## TypeScript-типы для фронта

```bash
npx openapi-typescript api/openapi.yaml -o web/src/api/schema.d.ts
```

Типизированный клиент поверх типов: пакет `openapi-fetch`
(`createClient<paths>({ baseUrl: '/api/v1', credentials: 'include' })`).

## Моки

Prism поднимает мок-сервер напрямую из спецификации (использует `example`):

```bash
npx @stoplight/prism-cli mock -p 4010 api/openapi.yaml
```

Для разработки фронта и тестов компонентов можно использовать MSW:
обработчики строятся на примерах из контракта (`examples`), типы - из
сгенерированного `schema.d.ts`. Сами моки добавляются вместе с `web/`.
