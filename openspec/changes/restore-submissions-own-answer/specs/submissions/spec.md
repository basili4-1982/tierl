# Spec Delta

## ADDED Requirements

### Requirement: Чтение своего ответа

Система SHALL отдавать текущий ответ анонимной сессии через
`GET /api/v1/tierlists/{slug}/submissions/mine` без регистрации. Если у сессии
есть ответ на этот опрос, система MUST возвращать `200` с идентификатором
результата, slug опроса, размещениями и временем создания/обновления. Если
ответа нет или cookie `tierl_anon` отсутствует, система MUST возвращать `404
not_found`. Запрос MUST NOT создавать ответ, MUST NOT выдавать новую cookie и
MUST NOT раскрывать `session_id` или `user_id`.

#### Scenario: Сессия читает свой ответ

- **WHEN** сессия с cookie `tierl_anon`, уже отправившая ответ, вызывает
  `GET /tierlists/{slug}/submissions/mine`
- **THEN** ответ `200` с тем же `id` результата, что и при отправке
- **AND** `placements` совпадают с сохранёнными размещениями
- **AND** в ответе нет `session_id` и `user_id`

#### Scenario: Ответа ещё нет

- **WHEN** сессия без ответа или без cookie `tierl_anon` вызывает
  `GET /tierlists/{slug}/submissions/mine`
- **THEN** ответ `404 not_found`
- **AND** новый ответ не создаётся
- **AND** новая cookie `tierl_anon` не выдаётся
