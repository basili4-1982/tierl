# Proposal

## Why

TIERL-33 (PR 88) закрыл доработку из аудита `TIERL-9`: E2E-тест
`internal/app/mvp_e2e_test.go` больше не меняет бизнес-состояние в БД напрямую.
Подтверждение email идёт через `POST /api/v1/auth/verify-email` с токеном из
тестового канала доставки, 20 гостей отвечают через
`POST /api/v1/tierlists/{slug}/submissions` с анонимной cookie, а результат
читается через `GET /api/v1/results/{id}` и `/stats`.

Требование происходит из `docs/SPEC.md`, раздел 11 «Критерии готовности MVP»
(автор проходит путь без ручных действий в БД, гость отвечает без регистрации
и видит результат, unlisted не попадает в каталог). Маршруты уже описаны в
`api/openapi.yaml`. При этом в OpenSpec нет capability, которая фиксировала бы
этот сквозной приёмочный путь, а change `restore-mvp-e2e`, созданный аудитом
`TIERL-9`, в репозитории отсутствует. Поэтому у TIERL-33 нет живой цепочки
«цель → спека», хотя код и тест уже слиты. Изменение восстанавливает
требование, не меняя поведение кода.

## What Changes

- Восстанавливается capability `mvp-e2e`: сквозной путь MVP проходит через
  публичный HTTP API без ручных изменений бизнес-состояния в БД.
- Фиксируется происхождение работы: `docs/SPEC.md` §1, §4, §11,
  `api/openapi.yaml` (`/auth/verify-email`, `/tierlists/{slug}`,
  `/tierlists/{slug}/submissions`, `/tierlists/{slug}/submissions/mine`,
  `/results/{id}`, `/results/{id}/stats`).
- Связываются существующие требования якорей `accounts`, `tierlists`,
  `submissions`, `results` и `catalog`: `mvp-e2e` проверяет их совместно, а не
  вводит новое поведение продукта.
- **BREAKING**: нет. HTTP-маршруты, схема БД, applied-миграции и поведение
  продакшен-кода не меняются.

## Capabilities

### New Capabilities

- `mvp-e2e`: сквозной приёмочный путь «регистрация → подтверждение email →
  создание → шеринг → ответ гостя → результат» только через публичный API,
  повторная отправка по контракту и недоступность unlisted в публичном
  каталоге.

Имя `mvp-e2e` новое, потому что ни один якорь цели не покрывает весь путь:
`accounts` отвечает за подтверждение email, `tierlists` — за создание и
чтение опроса, `submissions` — за приём и замену ответа, `results` — за
результат и статистику, `catalog` — за публичный список. Требование
существует только в их совместном прохождении, поэтому вводится отдельное имя
с этим обоснованием.

### Modified Capabilities

Нет: `accounts`, `tierlists`, `submissions`, `results` и `catalog` сохраняют
свои требования; `mvp-e2e` связывает их в один приёмочный сценарий.

## Impact

- Спека: новый `openspec/specs/mvp-e2e/spec.md` после архивации.
- Код (только сверка, без изменения поведения): `internal/app/mvp_e2e_test.go`,
  `internal/web/api/api.go`, `internal/web/api/auth.go`,
  `internal/web/api/submissions.go`, `internal/web/api/results.go`.
- Контракт: `api/openapi.yaml` — существующие маршруты, изменения не требуются.
- Миграции: без изменений.
- Аудит: вердикт `ok` в `docs/audits/TIERL-33.md`; gap и excess в коде не
  найдены, удаление кода не требуется.
