# Proposal

## Why

Аудит `docs/audits/TIERL-47.md` зафиксировал два разных факта PR 108 «Pool
smoke 6: repo из проекта»:

- `gap`: заявленная проверка TT-148 (воркер без `REPO` в env, репозиторий из
  аренды) не реализована — в репозитории нет воркера пула, lease-клиента и
  smoke-теста;
- `excess`: единственный оставленный артефакт — файл `pool-smoke.txt` со
  строкой `smoke`, который не следует ни из одного требования tierl и не
  реализует TT-148.

Этот change закрывает только excess: удаляет файл-маркер. Gap TT-148
восстанавливается отдельным change'ем `restore-pool-smoke-check`; удаление
маркера его не заменяет.

## What Changes

- Удаляется `pool-smoke.txt` из корня репозитория как избыточный артефакт
  внешнего worker-pool harness.
- Проверяется, что на файл нет ссылок в CI, Makefile, документации и коде.
- Продуктовые спеки и код не меняются: это уборка tooling-артефакта.
- Требование TT-148 и задачи на его реализацию в этот change не входят — они в
  `restore-pool-smoke-check`.

## Capabilities

### New Capabilities

Нет. Удаление stray-файла не добавляет способности продукта; требование
TT-148 восстанавливается в capability `worker-pool` отдельным change'ем.

### Modified Capabilities

Нет. Ни один якорь (`accounts`, `tierlists`, `submissions`, `results`,
`catalog`, `reports`, `sharing`, `author-profile`, `item-images`, `self-host`)
не описывает worker-pool harness и переменную `REPO`. `self-host` относится к
запуску и деплою самого tierl, поэтому привязка к нему была бы ложной.

## Impact

- Удаляется только `pool-smoke.txt`; код, тесты, контракт, миграции и живые
  спеки OpenSpec не затрагиваются.
- Gap TT-148 остаётся открытым до реализации задач в
  `restore-pool-smoke-check`; эскалация владельцу harness зафиксирована там же.
- Аудит: `docs/audits/TIERL-47.md` фиксирует вердикт `gap` и отдельный
  excess-артефакт `pool-smoke.txt`.
