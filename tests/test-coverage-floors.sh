#!/usr/bin/env bash
# A coverage floor only rises (#334): this fails when a floor in coverage/ is lower than main's, unless a
# commit since main names it in a Floor-lowered trailer, so the reason stays in the history (#457).
set -uo pipefail
cd "$(dirname "$0")/.."

base="$(git merge-base HEAD origin/main 2>/dev/null)" || { echo "skip  no origin/main to compare the floors with"; exit 0; }
lowered="$(git log --format='%(trailers:key=Floor-lowered,valueonly)' "$base..HEAD" | awk '{ print $1 }')"
fail=0
for floor in coverage/*.floor; do
  now="$(tr -d '[:space:]' < "$floor")"
  before="$(git show "$base:$floor" 2>/dev/null | tr -d '[:space:]')"
  if [[ -z "$before" ]]; then echo "ok    $floor is new at $now"; continue; fi
  if awk -v a="$now" -v b="$before" 'BEGIN { exit !(a + 0 < b + 0) }'; then
    name="$(basename "$floor" .floor)"
    if grep -qxF -- "$name" <<< "$lowered"; then
      echo "ok    $floor went down from $before to $now, as a Floor-lowered trailer says"
      continue
    fi
    echo "FAIL  $floor went down from $before to $now; a floor only rises, unless a commit says why in a trailer: Floor-lowered: $name <reason>"
    fail=1
  else
    echo "ok    $floor is $now, not below $before"
  fi
done
(( fail )) && { echo "FAIL: coverage floors"; exit 1; }
echo "PASS: coverage floors"
