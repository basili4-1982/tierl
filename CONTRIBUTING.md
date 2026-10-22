# Участие в разработке

Состав работ и порядок — в issue #27. Требования к продукту — `docs/SPEC.md`, решения — `docs/adr/`. Кодекс поведения — `CODE_OF_CONDUCT.md`.

## Ветки и worktree

На каждую задачу — своя ветка и свой git worktree. Общий каталог не переключается между ветками. Корневой каталог репозитория стоит на `main` и нужен для чтения и инфраструктуры.

```bash
git fetch origin
git branch feat/<slug> origin/main
git worktree add ~/GolandProjects/worktrees/tierl-feat-<slug> feat/<slug>
cd ~/GolandProjects/worktrees/tierl-feat-<slug>
pwd   # сверьте перед правками
```

- Каталог: `~/GolandProjects/worktrees/<repo>-<branch>`, ветка с `/` в имени записывается через `-` (`tierl-feat-similarity`).
- Имя ветки: `feat/<slug>`, `fix/<slug>`, `docs/<slug>`, `chore/<slug>`, `test/<slug>`.
- Ветку, уже выгруженную в другом worktree, повторно не выгрузить — создайте новую.
- После мержа: `git worktree remove <path>` и `git worktree prune`.

## Коммиты

Conventional Commits: `feat:`, `fix:`, `docs:`, `chore:`, `test:`, `refactor:`, `security:`; область — по желанию (`feat(auth): ...`). Один коммит — одно законченное изменение. Не коммитьте `.env`, секреты и артефакты сборки.

## Pull request

- Прямых коммитов в `main` нет. Путь: ветка -> PR (base `main`) -> зелёный CI -> merge.
- Один PR — одна задача (issue); в теле `Closes #<номер>`.
- Что писать в описании:
  - что изменилось в поведении;
  - какие команды проверки прогнаны и их результат;
  - есть ли миграции и изменения контракта (`api/openapi.yaml`);
  - для UI — скриншот.
- Контракт API меняется отдельными небольшими PR, чтобы фронтенд и бэкенд подстраивались независимо.
- Общие файлы (роутер, DI в `main.go`) правит только тот PR, которому они нужны; такой PR мержится сразу.

## Миграции

Файлы лежат в `migrations/` и именуются по timestamp UTC, а не по порядковому номеру, чтобы параллельные PR не конфликтовали:

```
20261022091500_create_users.up.sql
20261022091500_create_users.down.sql
```

- У каждой миграции есть `up` и `down`.
- Применённые миграции не редактируются — добавляется новая.
- В PR упомяните миграцию и её влияние на существующие данные.

## Тесты

- Ядро (`internal/domain`, `internal/similarity`, auth-сервисы) покрыто на 95% и выше.
- Тесты запускаются с детектором гонок: `go test -race -cover ./...`.
- Табличные тесты для логики с ветвлением, property-тесты для `similarity`.
- Тесты не ходят во внешние сервисы; почта — через `LogMailer`, БД — через интеграционные тесты с Postgres из compose.
- Баг исправляется вместе с тестом, который воспроизводил его.

## Линтеры и проверки

Перед пушем:

```bash
gofmt -l .            # вывод должен быть пустым
go vet ./...
golangci-lint run
govulncheck ./...
go test -race -cover ./...
```

Для `web/` — линтер и тесты, описанные в `web/package.json`. Конфигурацию линтеров не ослабляйте ради прохождения; исключение объясняйте комментарием `//nolint:<linter> // причина`.

## Безопасность

Уязвимости не раскрываем в issues — см. `SECURITY.md`.

## Фронтенд (`web/`)

```bash
make web-install   # npm ci
make web-test      # lint + typecheck + vitest
make web-build     # сборка в internal/web/static/dist (go:embed)
cd web && npm run dev   # Vite на :5173, /api проксируется на :8080
cd web && npm run gen:api   # типы из api/openapi.yaml -> web/src/api/schema.d.ts
```

Сгенерированный `schema.d.ts` коммитится; после изменения контракта перегенерируйте его. Содержимое `internal/web/static/dist` в git не попадает (кроме `.gitkeep`).
