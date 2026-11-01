# Spec Delta

## ADDED Requirements

### Requirement: Ответ на закрытый опрос

Система SHALL принимать новые и заменяющие ответы только для опроса в статусе
`open`. Если `POST /api/v1/tierlists/{slug}/submissions` или
`PUT /api/v1/tierlists/{slug}/submissions/mine` вызван для опроса в статусе
`closed`, `draft` или `hidden`, система MUST отвечать `409` с кодом
`tierlist_closed`. Такой запрос MUST NOT создавать новый ответ, MUST NOT
заменять существующий ответ и MUST NOT менять статистику.

#### Scenario: Ответ на закрытый опрос отклоняется

- **WHEN** сессия отправляет `POST` или `PUT` ответа на опрос не в статусе
  `open`
- **THEN** ответ `409` с кодом `tierlist_closed`
- **AND** ответ не сохраняется и существующий ответ не заменяется
- **AND** число ответов и распределение по тирам не меняются
