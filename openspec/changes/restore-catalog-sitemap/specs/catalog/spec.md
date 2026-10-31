# Spec Delta

## ADDED Requirements

### Requirement: Sitemap публичных опросов

`GET /sitemap.xml` SHALL отдавать XML `urlset` с абсолютными URL страниц
`/t/<slug>` только для опросов с видимостью `public` и статусом `open`.
Unlisted, черновики, закрытые и скрытые модерацией опросы MUST NOT попадать в
sitemap. При отсутствии подходящих опросов ответ MUST быть `200` с валидным
пустым `urlset`.

#### Scenario: Публичные открытые опросы попадают в sitemap

- **WHEN** клиент запрашивает `/sitemap.xml` и в системе есть public+open опросы
- **THEN** ответ `200` с XML `urlset`
- **AND** для каждого такого опроса в sitemap есть абсолютный URL `/t/<slug>`

#### Scenario: Unlisted, черновик, закрытый и скрытый отсутствуют

- **WHEN** в системе есть unlisted, draft, closed и hidden опросы
- **THEN** их URL отсутствуют в sitemap
- **AND** public+open опросы при этом могут присутствовать

#### Scenario: Пустой sitemap

- **WHEN** нет ни одного public+open опроса
- **THEN** ответ `200` с валидным пустым `<urlset>`
