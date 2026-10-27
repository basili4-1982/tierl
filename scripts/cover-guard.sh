#!/usr/bin/env bash
# Страж порога покрытия: COVER_MIN в PR не может быть ниже, чем в main.
#
# Порог только растёт: поднимаешь покрытие — поднимаешь COVER_MIN в том же PR.
# Понижение возможно, но не молча: оно требует явной пометки [cover-lower] в
# сообщении коммита и обоснования в PR — иначе CI красный.
set -euo pipefail

git fetch --no-tags --quiet origin main:refs/remotes/origin/main

read_min() { sed -n 's/^COVER_MIN ?= *//p' "$1" | head -n1 | tr -d '[:space:]'; }

base="$(read_min <(git show origin/main:Makefile))"
head="$(read_min Makefile)"

for pair in "main:$base" "PR:$head"; do
	name="${pair%%:*}"
	value="${pair#*:}"
	if [[ ! "$value" =~ ^[0-9]+$ ]]; then
		echo "cover-guard: не удалось прочитать COVER_MIN ($name): '$value'" >&2
		exit 1
	fi
done

if (( head >= base )); then
	echo "cover-guard: COVER_MIN = $head (main: $base) — ок"
	exit 0
fi

if git log --format=%B "origin/main..HEAD" | grep -q '\[cover-lower\]'; then
	echo "cover-guard: COVER_MIN снижен $base → $head с явной пометкой [cover-lower] — ок"
	exit 0
fi

echo "cover-guard: COVER_MIN в PR = $head, в main = $base." >&2
echo "cover-guard: порог покрытия только растёт; снижение требует пометки [cover-lower] в коммите и обоснования в PR." >&2
exit 1
