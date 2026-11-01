#!/usr/bin/env bash
# A coverage floor only rises (#334): this fails when a floor in coverage/ is lower than main's, unless every commit
# since main that lowers it says so in its own trailer, Floor-lowered: <name> <new value> <reason> (#457, #466).
# That a lowered floor sits at the measured total is held by scripts/coverage.sh, which runs where the total exists.
set -uo pipefail
cd "$(dirname "$0")/.."

# COVERAGE_FLOORS_BASE lets the shell tests point the check at a throwaway repository with no origin.
base="${COVERAGE_FLOORS_BASE:-$(git merge-base HEAD origin/main 2>/dev/null)}"
[[ -n "$base" ]] || { echo "skip  no origin/main to compare the floors with"; exit 0; }

value_at() { git show "$1:$2" 2>/dev/null | tr -d '[:space:]'; }
below() { awk -v a="$1" -v b="$2" 'BEGIN { exit !(a + 0 < b + 0) }'; }

# A trailer counts for the commit that carries it, and only when it names this floor at the value that commit sets.
trailer_lowers() { # commit name value
  git log -1 --format='%(trailers:key=Floor-lowered,valueonly)' "$1" \
    | awk -v n="$2" -v v="$3" '$1 == n && $2 ~ /^[0-9.]+$/ && $2 + 0 == v + 0 && NF >= 3 { found = 1 } END { exit !found }'
}

fail=0
# A deleted floor would otherwise read as 0 to coverage.sh and never come up in the loop below.
for floor in $(git ls-tree --name-only "$base" coverage/ | grep '\.floor$'); do
  [[ -f "$floor" ]] || { echo "FAIL  $floor is missing, but main has it at $(value_at "$base" "$floor"); restore the file"; fail=1; }
done
for floor in coverage/*.floor; do
  name="$(basename "$floor" .floor)"
  now="$(tr -d '[:space:]' < "$floor")"
  before="$(value_at "$base" "$floor")"
  if [[ -z "$before" ]]; then echo "ok    $floor is new at $now"; continue; fi
  if ! below "$now" "$before"; then echo "ok    $floor is $now, not below $before"; continue; fi

  bad=0 drops=0
  for commit in $(git log --format=%H "$base..HEAD" -- "$floor"); do
    set_to="$(value_at "$commit" "$floor")"
    parent="$(value_at "$commit^" "$floor")"
    [[ -n "$parent" ]] && below "$set_to" "$parent" || continue
    drops=$((drops + 1))
    if ! trailer_lowers "$commit" "$name" "$set_to"; then
      echo "FAIL  $floor went from $parent to $set_to in ${commit:0:7}, which does not say why in a trailer: Floor-lowered: $name $set_to <reason>"
      bad=1
    fi
  done
  if (( drops == 0 )); then
    echo "FAIL  $floor went down from $before to $now, but no commit since main lowers it; lower it in a commit with the trailer Floor-lowered: $name $now <reason>"
    bad=1
  fi

  if (( bad )); then fail=1; else echo "ok    $floor went down from $before to $now, as each lowering commit's Floor-lowered trailer says"; fi
done
(( fail )) && { echo "FAIL: coverage floors"; exit 1; }
echo "PASS: coverage floors"
