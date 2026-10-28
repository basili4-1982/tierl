# CI: почему у PR может не быть проверок

## Симптом

У коммита PR нет ни одного прогона workflow, в интерфейсе — «no checks
reported» или «No checks have been reported for this pull request». При этом
workflow `.github/workflows/ci.yml` включён, а другие PR проверки получают.

## Причина, найденная для PR #91 (TIERL-28)

PR #91 (`c0c91007`) был открыт из ветки, основанной на старом `main`
(`1451b777`), когда `main` уже ушёл вперёд (`4e538553`). Обе ветки меняли
`internal/app/mvp_e2e_test.go`, поэтому при слиянии возникал конфликт, и GitHub
не создавал merge-ref `refs/pull/91/merge`.

Workflow с триггером `pull_request` запускается на merge-ref, а не на head
ветки. Без merge-ref событие не порождает прогон: триггеры, права токена,
раннеры и allow-list здесь ни при чём. Конфликт — не сбой CI, а его следствие:
CI не может собрать проверяемое состояние PR.

## Как проверить

```sh
# merge-ref есть у обычного PR и отсутствует у конфликтующего
git ls-remote origin 'refs/pull/<номер>/*'

gh pr view <номер> --json mergeable,mergeableState,headRefOid,baseRefOid

# воспроизвести конфликт локально по SHА из PR
git merge-tree --write-tree <base-ша> <head-ша>
```

Для #91 `mergeableState` был `dirty`, а `refs/pull/91/merge` отсутствовал.
`go test ./...` на самом коммите `c0c91007` при этом проходил: код был
исправен, не хватало только merge-ref.

## Что делать

1. Обновить ветку PR относительно `main` (`git fetch origin main`, затем
   merge или rebase), разрешить конфликты.
2. Отправить обновлённую ветку — GitHub пересоздаст merge-ref, и
   `pull_request` workflow запустится сам.
3. Если нужен прогон без нового коммита, запустить CI вручную на ветке PR
   (`workflow_dispatch` в `ci.yml`).

Нельзя лечить это отключением проверок, сужением их набора или переключением
`pull_request` на `pull_request_target`: проверять нужно именно результат
слияния с `main`.

## Предупреждение для долгоживущих PR

Конфликт с `main` — единственная штатная ситуация, когда `pull_request`
workflow не стартует вовсе. Если у PR нет проверок, сначала смотрите на
`mergeableState`, а уже потом на настройки Actions, токенов и раннеров.
