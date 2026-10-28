#!/usr/bin/env bash
# Prints a Markdown summary of a gate run from the reports its layers left: outcomes, durations, counts and
# the slowest tests. Usage: scripts/test-summary.sh [reports-dir]; reports may sit in one subdirectory per CI job.
set -euo pipefail

DIR="${1:-reports}"
SLOWEST=10

find_reports() { find "$DIR" -name "$1" -type f 2>/dev/null | sort; }

duration() { printf '%d:%02d' "$(($1 / 60))" "$(($1 % 60))"; }

echo "## Test run"
echo

layer_files=$(find_reports layers.tsv)
if [[ -n "$layer_files" ]]; then
  echo "| Layer | Result | Duration |"
  echo "| --- | --- | --- |"
  # shellcheck disable=SC2086
  cat $layer_files | while IFS=$'\t' read -r layer status secs; do
    [[ "$status" == passed ]] || status="**$status**"
    echo "| $layer | $status | $(duration "$secs") |"
  done
  echo
fi

# One line per suite: name, passed, failed, skipped; then its slowest tests as
# lines of seconds and name. Each reader prints both for its file format.
go_counts='
  [splits("\n") | fromjson? | select(.Test != null and (.Test | contains("/") | not) and (.Action | IN("pass", "fail", "skip")))]
  | (map(select(.Action == "pass")) | length) as $p
  | (map(select(.Action == "fail")) | length) as $f
  | (map(select(.Action == "skip")) | length) as $s
  | "\($p)\t\($f)\t\($s)"'
go_slowest='
  [splits("\n") | fromjson? | select(.Test != null and (.Test | contains("/") | not) and (.Action | IN("pass", "fail")))]
  | sort_by(-.Elapsed) | .[:$n][] | "\(.Elapsed)\t\(.Package | sub(".*/backend/"; "")) \(.Test)"'

vitest_counts='"\(.numPassedTests)\t\(.numFailedTests)\t\(.numPendingTests + .numTodoTests)"'
vitest_slowest='
  [.testResults[] | (.name | sub(".*/web/"; "")) as $file | .assertionResults[]
   | {d: ((.duration // 0) / 1000), name: "\($file) > \(.fullName)"}]
  | sort_by(-.d) | .[:$n][] | "\(.d)\t\(.name)"'

e2e_tests='[.. | objects | select(has("specs")) | .specs[] | . as $spec | .tests[]
  | {status, project: .projectName, name: "\($spec.file) > \($spec.title)",
     d: (([.results[].duration] | add // 0) / 1000)}]'
e2e_counts="$e2e_tests"'
  | "\(map(select(.status == "expected" or .status == "flaky")) | length)\t\(map(select(.status == "unexpected")) | length)\t\(map(select(.status == "skipped")) | length)"'
e2e_slowest="$e2e_tests"' | sort_by(-.d) | .[:$n][] | "\(.d)\t\(.name) [\(.project)]"'

suite_rows=""
slowest_sections=""

add_suite() { # title file raw? counts-filter slowest-filter
  local title="$1" file="$2" raw="$3" counts="$4" slowest="$5" flags=(-r) row
  [[ -n "$raw" ]] && flags+=(-R -s)
  row=$(jq "${flags[@]}" "$counts" "$file" 2>/dev/null) || return 0
  IFS=$'\t' read -r passed failed skipped <<< "$row"
  suite_rows+="| $title | $passed | $failed | $skipped |"$'\n'
  slowest_sections+=$'\n'"<details><summary>Slowest: $title</summary>"$'\n\n'"| Seconds | Test |"$'\n'"| ---: | --- |"$'\n'
  slowest_sections+=$(jq "${flags[@]}" --argjson n "$SLOWEST" "$slowest" "$file" 2>/dev/null \
    | awk -F '\t' '{ gsub(/\|/, "\\|", $2); printf "| %.1f | %s |\n", $1, $2 }')
  slowest_sections+=$'\n\n'"</details>"$'\n'
}

for f in $(find_reports go-unit.json); do add_suite "Go unit" "$f" raw "$go_counts" "$go_slowest"; done
for f in $(find_reports integration.json); do add_suite "Go integration" "$f" raw "$go_counts" "$go_slowest"; done
for f in $(find_reports vitest.json); do add_suite "Vitest" "$f" "" "$vitest_counts" "$vitest_slowest"; done
for f in $(find_reports e2e.json); do add_suite "Playwright" "$f" "" "$e2e_counts" "$e2e_slowest"; done

if [[ -n "$suite_rows" ]]; then
  echo "| Suite | Passed | Failed | Skipped |"
  echo "| --- | ---: | ---: | ---: |"
  printf '%s' "$suite_rows"
  printf '%s' "$slowest_sections"
fi

# What the browser suite reached of the routes and data-action controls, when a whole run was checked (#334).
for f in $(find_reports e2e-reach.md); do echo; cat "$f"; done

# Each coverage figure beside its floor, and per package or folder below it (#334).
coverage_files=$(find_reports 'coverage-*.tsv')
if [[ -n "$coverage_files" ]]; then
  echo
  echo "## Coverage"
  echo
  echo "| Measure | Total | Floor |"
  echo "| --- | ---: | ---: |"
  for f in $coverage_files; do
    name=$(basename "$f" .tsv); name=${name#coverage-}
    awk -F'\t' -v n="$name" '$1 == "total" { t = $2 } $1 == "floor" { fl = $2 } END { printf "| %s | %s%% | %s%% |\n", n, t, fl }' "$f"
  done
  for f in $coverage_files; do
    name=$(basename "$f" .tsv); name=${name#coverage-}
    printf '\n<details><summary>%s by package</summary>\n\n| Package | Covered | Statements |\n| --- | ---: | ---: |\n' "$name"
    awk -F'\t' '$1 != "total" && $1 != "floor" { printf "| %s | %s%% | %s |\n", $1, $2, $3 }' "$f"
    printf '\n</details>\n'
  done
  echo
  echo "A floor in coverage/ only rises. Once a change lifts a total, raise its floor to the new figure, rounded down."
fi
