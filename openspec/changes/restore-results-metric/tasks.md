# Tasks

## 1. Аудит TIERL-52

- [x] 1.1 Сопоставить R1–R4 с кодом: `internal/web/api/results.go:getResult`, `getResultStats`, `internal/tierlist/submissions.go:Result`, `Stats`, `internal/similarity/similarity.go:Similarity`, `ThinkLikeYou`, `internal/storage/postgres/answers.go:Distributions`
- [x] 1.2 Подтвердить анонимность публичной выдачи: `toResultStatsDTO` копирует только агрегаты, в `submission` нет `session_id`/`user_id`
- [x] 1.3 Записать вердикты `ok` по R1–R4, отсутствие gap и excess в коде и пробел спеки в `docs/audits/results.md`

## 2. Восстановление требования

- [x] 2.1 Добавить в capability `results` требование «Метрика сходства и порог» с формулой ADR-001, `min_overlap = 3`, порогом `0.80` и полем `similarity_threshold`
- [x] 2.2 Сохранить существующие требования R1–R4 без переписывания и не переименовывать якорь `results`

## 3. Follow-up по контракту

- [ ] 3.1 Привести пример `min_submissions` в `api/openapi.yaml` (`/results/{id}/stats`) к нормативному значению `20` вместо `10`; код, тесты и applied-миграции не менять
- [ ] 3.2 Проверить, что пример OpenAPI и `ResultStats` согласованы по `similarity_threshold`, `min_submissions` и лимиту `maxItems: 5`

## 4. Проверки

- [x] 4.1 Подтвердить, что код, тесты, контракт и applied-миграции не менялись
- [x] 4.2 Проверить `openspec validate --strict --all --no-interactive` без ошибок
