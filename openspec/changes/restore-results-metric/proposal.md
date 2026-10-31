# Proposal

## Why

Аудит TIERL-52 способности «Результат и метрика сходства» подтвердил, что
требования R1–R4 реализованы в коде, а живая capability `results` уже
существует (её восстановил архивный change
`2026-10-28-restore-submissions-results`). При этом в живой спеке нет
нормативного описания самой метрики сходства: формула, `min_overlap = 3` и
порог `0.80` остаются только в `docs/SPEC.md` §5 и ADR-001, а поле
`similarity_threshold`, которое API реально возвращает и требует OpenAPI, не
названо в требовании о пороге «мало данных». Из-за этого у метрики и у
публикуемого порога нет полной цепочки «цель → спека».

## What Changes

- В capability `results` добавляется требование «Метрика сходства и порог»:
  нормативная формула из `docs/SPEC.md` §5 и ADR-001, `min_overlap = 3`,
  порог «думают как ты» `0.80` и явное поле `similarity_threshold` в ответе
  статистики.
- Существующее требование «Порог «мало данных»» не переписывается: порог
  ответов `20`, при меньшем числе — `insufficient_data = true`,
  `like_you_percent = null`, пустые `similar`/`dissimilar` сохраняются.
- В `tasks.md` выносится follow-up на приведение примера
  `min_submissions` в `api/openapi.yaml` к нормативным `20` (сейчас в примере
  `10`) без изменения кода.
- Поведение кода, тестов, HTTP-маршрутов, контракта и applied-миграций в этом
  change не меняется.

## Capabilities

### New Capabilities

Нет. Живая capability `results` уже описывает результат по идентификатору,
распределение по тирам, порог «мало данных» и анонимность публичной
статистики; новая способность не вводится.

### Modified Capabilities

- `results`: добавляется нормативное требование к метрике сходства и порогу,
  чтобы формула сходства и поле `similarity_threshold` были видны в живой
  спеке. Якорь `results` сохраняется без переименования.

## Impact

- Спека: `openspec/specs/results/spec.md` после архивации получит новое
  требование к метрике сходства и порогу.
- Код (только проверка соответствия, без изменения поведения):
  `internal/web/api/results.go:getResult`, `getResultStats`,
  `internal/tierlist/submissions.go:Result`, `Stats`,
  `internal/similarity/similarity.go:Similarity`, `ThinkLikeYou`,
  `internal/storage/postgres/answers.go:Distributions`.
- Контракт: `api/openapi.yaml`, раздел `/results/{id}/stats` и схема
  `ResultStats`; расхождение примера `min_submissions: 10` с нормативным `20`
  вынесено в задачи.
- Аудит: `docs/audits/results.md` фиксирует вердикты `ok` по R1–R4,
  отсутствие gap и excess в коде и найденный пробел спеки.
