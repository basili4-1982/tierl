# Design

## Context

TIERL-33 реализован в `internal/app/mvp_e2e_test.go`. `newMVPFixture`
поднимает настоящий HTTP-обработчик `web.NewHandler`, `confirmAuthor` берёт
одноразовый токен из `mailertest.Capture` и вызывает
`POST /api/v1/auth/verify-email`, `TestMVPFlowRegistrationToResult` создаёт
опрос через API, открывает его гостем по ссылке, отправляет 20 ответов через
`POST /api/v1/tierlists/{slug}/submissions` с cookie `tierl_anon` и читает
результат через `GET /api/v1/results/{id}` и `/stats`.
`TestMVPResubmissionContract` проверяет повторный `POST` и
`PUT /submissions/mine`, а `TestMVPUnlistedNotInCatalog` — недоступность
unlisted в каталоге.

OpenSpec этого сквозного требования не описывает: есть `docs/SPEC.md` §11 и
`api/openapi.yaml`, но канонической capability нет, а change
`restore-mvp-e2e` из аудита `TIERL-9` отсутствует. Аудит восстанавливает
требование и фиксирует соответствие кода, а не переписывает реализацию.

## Goals / Non-Goals

**Goals:**

- зафиксировать сквозной путь MVP через публичный API как требование
  OpenSpec;
- связать его с существующими capability `accounts`, `tierlists`,
  `submissions`, `results` и `catalog`;
- подтвердить, что тест не меняет бизнес-состояние в БД напрямую и что
  избыточного кода нет.

**Non-Goals:**

- менять обработчики, маршруты, сервисы, фронтенд или тесты;
- менять applied-миграции и контракт API;
- реализовывать HTTP-маршрут каталога: он относится к change `add-catalog`;
- добавлять новые возможности продукта, которых не было в TIERL-33.

## Decisions

1. **Новая capability `mvp-e2e`, а не один из якорей.** Сквозной путь пересекает
   `accounts`, `tierlists`, `submissions`, `results` и `catalog`; ни один якорь
   не покрывает его целиком. Имя `mvp-e2e` фиксирует именно совместное
   прохождение и обосновано в `proposal.md`.
2. **Спека описывает наблюдаемое поведение.** Требования ссылаются на
   публичные маршруты и коды ответов, а не на имена функций или структуру
   фикстур.
3. **Поведение кода не меняется.** Требование уже реализовано и проверено;
   аудит только восстанавливает цепочку «цель → спека».
4. **Unlisted проверяется по требованию каталога.** CAPABILITY `mvp-e2e`
   требует недоступности unlisted в публичном каталоге. Сейчас
   `TestMVPUnlistedNotInCatalog` читает `Store.ListPublic` напрямую, потому что
   `GET /api/v1/catalog` ещё не реализован; замена на HTTP-проверку вынесена в
   `openspec/changes/add-catalog/tasks.md`, пункт 2.4, и не блокирует
   восстановление спеки.
5. **Избыточный код не удаляется.** Обвязка `mvpFixture`, `mvpUsers`,
   `mvpAnonCookie` и проверки ответа нужны для сквозного теста и следуют из
   требования.

## Risks / Trade-offs

- [Новое имя capability пересекается с существующими якорями] → в
  `proposal.md` явно перечислено, за что отвечает каждый якорь и почему
  сквозной путь требует отдельного имени; дублирования поведения нет.
- [E2E-тесты требуют Docker] → без Docker они пропускаются через `testDB`;
  это ограничение окружения, а не дефект; CI задаёт
  `TIERL_REQUIRE_DOCKER=1`.
- [HTTP-проверка каталога ещё недоступна] → follow-up зафиксирован в
  `add-catalog` 2.4; текущее чтение хранилища не меняет бизнес-состояние.
