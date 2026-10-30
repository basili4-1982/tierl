# Spec Delta

## ADDED Requirements

### Requirement: Каноническая ссылка OG-превью

Страницы шеринга SHALL публиковать в `og:image` и `twitter:image` абсолютный
URL `/og/t/<slug>.png` для опроса и `/og/r/<id>.png` для результата. Этот URL
MUST отдавать PNG 1200x630 с `Content-Type: image/png` и теми же cache- и
`noindex`-правилами, что и алиасы `/t/<slug>/og.png` и `/r/<id>/og.png`.
Серверные тесты MUST запрашивать изображение по URL из `og:image`.

#### Scenario: Каноническая ссылка отдаёт PNG

- **WHEN** клиент читает `og:image` со страницы `/t/<slug>` или `/r/<id>`
- **THEN** ссылка абсолютная и указывает на `/og/t/<slug>.png` или `/og/r/<id>.png`
- **AND** запрос по этой ссылке возвращает `200`, `Content-Type: image/png` и PNG 1200x630

#### Scenario: Unlisted-превью закрыто от индексации

- **WHEN** запрошена каноническая ссылка OG-превью unlisted-опроса
- **THEN** ответ содержит `X-Robots-Tag` с `noindex`
- **AND** `Cache-Control` содержит `private` и `no-store`

#### Scenario: Тест идёт по ссылке со страницы

- **WHEN** серверный тест проверяет PNG-превью
- **THEN** он извлекает адрес из `og:image` страницы и запрашивает его
- **AND** жёстко заданный старый алиас не считается проверкой канонической ссылки
