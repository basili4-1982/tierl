# Proposal

## Why

Аудит TIERL-59 способности «Прохождение опроса» подтвердил, что требования
R1–R3 реализованы в коде, а живая capability `submissions` уже существует (её
восстановил архивный change `2026-10-28-restore-submissions-results`). При этом
живая спека описывает только `POST` и `PUT` ответа и не упоминает
`GET /api/v1/tierlists/{slug}/submissions/mine`, хотя маршрут реализован в
`internal/web/api/submissions.go:getMySubmission`, описан в
`api/openapi.yaml` и используется интерфейсом, чтобы не отвечать на опрос
повторно. Из-за этого у части сценария прохождения опроса нет полной цепочки
«цель → спека».

## What Changes

- В capability `submissions` добавляется требование «Чтение своего ответа»:
  `GET /api/v1/tierlists/{slug}/submissions/mine` возвращает `200` с текущим
  ответом анонимной сессии либо `404 not_found`, если ответа нет; запрос не
  создаёт ответ, не выдаёт cookie и не раскрывает `session_id`/`user_id`.
- Существующие требования R1–R3 (приём ответа, минимум три размещения и
  неполный ответ, один ответ на анонимную сессию) не переписываются.
- В `tasks.md` выносятся follow-up задачи на расхождения `api/openapi.yaml`:
  описание `sessionCookie`/`403` для передачи ответа и формулировка
  «авторизованная сессия» в `PUT` не соответствуют ADR-002 и коду.
- Поведение кода, тестов, HTTP-маршрутов, контракта и applied-миграций в этом
  change не меняется.

## Capabilities

### New Capabilities

Нет. Живая capability `submissions` уже описывает приём ответа, границу
минимума размещений, частичный ответ и правило одного ответа на сессию; новая
способность не вводится.

### Modified Capabilities

- `submissions`: добавляется требование к чтению собственного ответа, чтобы
  реализованный и используемый клиентом маршрут был виден в живой спеке. Якорь
  `submissions` сохраняется без переименования.

## Impact

- Спека: `openspec/specs/submissions/spec.md` после архивации получит новое
  требование к `GET /tierlists/{slug}/submissions/mine`.
- Код (только проверка соответствия, без изменения поведения):
  `internal/web/api/submissions.go:getMySubmission`, `anonSession`,
  `internal/tierlist/submissions.go:MySubmission`,
  `internal/storage/postgres/answers.go:SubmissionBySession`.
- Контракт: `api/openapi.yaml`, раздел
  `/tierlists/{slug}/submissions/mine`; расхождения по `sessionCookie` и `403`
  вынесены в задачи.
- Аудит: `docs/audits/submissions.md` фиксирует вердикты `ok` по R1–R3,
  отсутствие gap и excess в коде и найденный пробел спеки.
