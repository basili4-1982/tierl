# Spec Delta

## ADDED Requirements

### Requirement: Условная валидация OG-превью по ETag

Маршруты PNG-превью `GET /og/t/<slug>.png`, `GET /og/r/<id>.png` и
совместимые алиасы `GET /t/<slug>/og.png`, `GET /r/<id>/og.png` SHALL
поддерживать условные запросы. Если заголовок `If-None-Match` содержит текущий
`ETag` ресурса, ответ MUST быть `304 Not Modified` без тела. Если заголовок
отсутствует или не совпадает, ответ MUST быть `200` с PNG. Ответ `304` MUST
сохранять `ETag`, `Cache-Control` и, для unlisted, `X-Robots-Tag` с
`noindex`; кэш unlisted MUST оставаться `private, no-store`.

#### Scenario: Совпадающий ETag даёт 304

- **WHEN** клиент запрашивает PNG-превью с `If-None-Match`, равным текущему ETag
- **THEN** ответ имеет статус `304` и пустое тело
- **AND** ответ содержит тот же `ETag` и те же cache-заголовки, что и `200`

#### Scenario: Несовпадающий ETag даёт PNG

- **WHEN** клиент запрашивает PNG-превью с отсутствующим или несовпадающим `If-None-Match`
- **THEN** ответ `200` с `Content-Type: image/png`
- **AND** тело является PNG 1200x630

#### Scenario: Unlisted сохраняет noindex при 304

- **WHEN** клиент повторно запрашивает PNG-превью unlisted-опроса с текущим ETag
- **THEN** ответ `304` содержит `X-Robots-Tag` с `noindex`
- **AND** `Cache-Control` содержит `private` и `no-store`
