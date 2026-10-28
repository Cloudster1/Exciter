#!/usr/bin/env bash
# A coverage floor only rises (#334): this fails when a floor in coverage/ is lower than main's.
set -uo pipefail
cd "$(dirname "$0")/.."

base="$(git merge-base HEAD origin/main 2>/dev/null)" || { echo "skip  no origin/main to compare the floors with"; exit 0; }
fail=0
for floor in coverage/*.floor; do
  now="$(tr -d '[:space:]' < "$floor")"
  before="$(git show "$base:$floor" 2>/dev/null | tr -d '[:space:]')"
  if [[ -z "$before" ]]; then echo "ok    $floor is new at $now"; continue; fi
  if awk -v a="$now" -v b="$before" 'BEGIN { exit !(a + 0 < b + 0) }'; then
    echo "FAIL  $floor went down from $before to $now; a floor only rises"
    fail=1
  else
    echo "ok    $floor is $now, not below $before"
  fi
done
(( fail )) && { echo "FAIL: coverage floors"; exit 1; }
echo "PASS: coverage floors"
