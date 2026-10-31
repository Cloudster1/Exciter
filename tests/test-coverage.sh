#!/usr/bin/env bash
# Tests for scripts/coverage.sh and tests/test-coverage-floors.sh: a total just below its floor fails (#465), and a
# floor goes down only through its own commit's trailer, to the measured total rounded down (#466), on throwaway trees.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"

export GIT_AUTHOR_NAME=Tester GIT_AUTHOR_EMAIL=tester@example.com
export GIT_COMMITTER_NAME=Tester GIT_COMMITTER_EMAIL=tester@example.com
export GIT_CONFIG_COUNT=3
export GIT_CONFIG_KEY_0=maintenance.auto GIT_CONFIG_VALUE_0=false
export GIT_CONFIG_KEY_1=gc.auto GIT_CONFIG_VALUE_1=0
export GIT_CONFIG_KEY_2=init.defaultBranch GIT_CONFIG_VALUE_2=main

fixture_failed() {
    echo "The test fixture broke while $1. The error above is the cause; fix it and run the script again." >&2
    exit 1
}

W="$(mktemp -d)" || fixture_failed "creating the temporary directory"
trap 'rm -rf "${W}"' EXIT

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }
check() { if [[ "$2" == "$3" ]]; then pass "$1"; else fail "$1: expected [$3], got [$2]"; fi; }

echo "== scripts/coverage.sh rounds a total down before comparing it with the floor"

# coverage.sh finds its floors from its own path, so a copy in a tree of its own reads that tree's floor.
TREE="${W}/tree"
mkdir -p "${TREE}/scripts" "${TREE}/coverage" || fixture_failed "creating the coverage tree"
cp "${SRC}/scripts/coverage.sh" "${TREE}/scripts/" || fixture_failed "copying coverage.sh"
echo 76 > "${TREE}/coverage/x.floor"

go_profile() { # covered statements, uncovered statements
    printf 'mode: set\n'
    printf 'github.com/cloudster1/exciter/backend/internal/a/a.go:1.1,2.2 %d 1\n' "$1"
    printf 'github.com/cloudster1/exciter/backend/internal/a/a.go:3.1,4.2 %d 0\n' "$2"
}

run_coverage() { # kind, report
    REPORTS="${TREE}/reports" "${TREE}/scripts/coverage.sh" "$1" x "$2" > "${W}/out" 2>&1
    echo $?
}

go_profile 1899 601 > "${W}/below.cover"
check "75.96% of Go statements fails a floor of 76" "$(run_coverage go "${W}/below.cover")" 1
check "the Go total reads 75.9, rounded down" "$(awk -F'\t' '$1 == "total" { print $2 }' "${TREE}/reports/coverage-x.tsv")" 75.9
check "a Go package reads 75.9, rounded down" "$(awk -F'\t' '$1 == "internal/a" { print $2 }' "${TREE}/reports/coverage-x.tsv")" 75.9

go_profile 1900 600 > "${W}/at.cover"
check "exactly 76.0% of Go statements passes a floor of 76" "$(run_coverage go "${W}/at.cover")" 0
check "the Go total reads 76.0" "$(awk -F'\t' '$1 == "total" { print $2 }' "${TREE}/reports/coverage-x.tsv")" 76.0

web_summary() { # covered lines, total lines, vitest's own rounded percentage
    printf '{"total":{"lines":{"total":%d,"covered":%d,"pct":%s}},"/src/web/src/lib/a.ts":{"lines":{"total":%d,"covered":%d,"pct":%s}}}\n' \
        "$2" "$1" "$3" "$2" "$1" "$3"
}

web_summary 1899 2500 75.96 > "${W}/below.json"
check "75.96% of web lines fails a floor of 76" "$(run_coverage web "${W}/below.json")" 1
check "the web total reads 75.9, rounded down" "$(awk -F'\t' '$1 == "total" { print $2 }' "${TREE}/reports/coverage-x.tsv")" 75.9

web_summary 1900 2500 76 > "${W}/at.json"
check "exactly 76% of web lines passes a floor of 76" "$(run_coverage web "${W}/at.json")" 0

echo "== tests/test-coverage-floors.sh holds each lowering commit to its own trailer and value"

# main carries two floors; each case branches from a fresh copy and commits its own changes.
PRISTINE="${W}/pristine"
mkdir -p "${PRISTINE}/tests" "${PRISTINE}/coverage" || fixture_failed "creating the floors repository"
cp "${SRC}/tests/test-coverage-floors.sh" "${PRISTINE}/tests/" || fixture_failed "copying test-coverage-floors.sh"
(
    cd "${PRISTINE}" && git init -q && echo 86 > coverage/web-lines.floor && echo 35.5 > coverage/go-unit.floor \
        && git add -A && git commit -qm "chore: floors" && git switch -qc fix/1-case
) || fixture_failed "committing main's floors"

REPO="${W}/repo"
fresh() { rm -rf "${REPO}" && cp -a "${PRISTINE}" "${REPO}" || fixture_failed "copying the floors repository"; }
floor_commit() { # value, message
    (cd "${REPO}" && echo "$1" > coverage/web-lines.floor && git commit -qam "$2") || fixture_failed "committing a floor of $1"
}
floors_check() { # exit code of the check against main
    (cd "${REPO}" && COVERAGE_FLOORS_BASE="$(git rev-parse main)" REPORTS="${REPO}/reports" ./tests/test-coverage-floors.sh > "${W}/out" 2>&1)
    echo $?
}

fresh
floor_commit 87 "test: cover more"
check "a raised floor passes" "$(floors_check)" 0

fresh
floor_commit 85 "build: count lines differently"
check "a drop with no trailer fails" "$(floors_check)" 1

fresh
floor_commit 85 "build: count lines differently

Floor-lowered: go-unit 85 the provider counts fewer lines"
check "a drop whose trailer names another floor fails" "$(floors_check)" 1

fresh
floor_commit 85 "build: count lines differently

Floor-lowered: web-lines 80 the provider counts fewer lines"
check "a drop whose trailer names another value fails" "$(floors_check)" 1

fresh
floor_commit 85 "build: count lines differently

Floor-lowered: web-lines 85"
check "a drop whose trailer gives no reason fails" "$(floors_check)" 1

fresh
floor_commit 85 "build: count lines differently

Floor-lowered: web-lines 85 the provider counts fewer lines"
check "a drop with a matching trailer and value passes" "$(floors_check)" 0

floor_commit 84 "feat: something unrelated"
check "a second, untrailed drop after a trailed one fails" "$(floors_check)" 1

fresh
(cd "${REPO}" && git rm -q coverage/web-lines.floor && git commit -qm "chore: drop the web floor") || fixture_failed "deleting a floor"
check "a floor deleted since main fails" "$(floors_check)" 1

echo "== scripts/coverage.sh holds a floor lowered since main to the measured total rounded down"

# A repository whose main holds web-lines at 86, so the floor in the working tree can sit below it.
LOWERED="${W}/lowered"
mkdir -p "${LOWERED}/scripts" "${LOWERED}/coverage" || fixture_failed "creating the lowered repository"
cp "${SRC}/scripts/coverage.sh" "${LOWERED}/scripts/" || fixture_failed "copying coverage.sh"
(cd "${LOWERED}" && echo 86 > coverage/web-lines.floor && git init -q && git add -A && git commit -qm "chore: floors") \
    || fixture_failed "committing main's floor"

lowered_coverage() { # floor in the working tree, or "none", then covered lines of 1000
    if [[ "$1" == none ]]; then rm -f "${LOWERED}/coverage/web-lines.floor"; else echo "$1" > "${LOWERED}/coverage/web-lines.floor"; fi
    web_summary "$2" 1000 0 > "${W}/lowered.json"
    (cd "${LOWERED}" && COVERAGE_FLOORS_BASE="$(git rev-parse HEAD)" REPORTS="${LOWERED}/reports" \
        ./scripts/coverage.sh web web-lines "${W}/lowered.json" > "${W}/out" 2>&1)
    echo $?
}

check "a floor lowered to the measured total rounded down passes" "$(lowered_coverage 85 854)" 0
check "a floor lowered below the measured total rounded down fails" "$(lowered_coverage 84 853)" 1
check "the failure names the value to set" "$(grep -o 'set it to [0-9.]*' "${W}/out")" "set it to 85"
check "a floor lowered where the total needs none fails" "$(lowered_coverage 85 863)" 1
check "the failure says to set it back up" "$(grep -o 'needs no lower floor. Set it to [0-9.]*' "${W}/out")" "needs no lower floor. Set it to 86"
check "a floor lowered to 0 fails, whatever its trailer says" "$(lowered_coverage 0 853)" 1
check "a floor kept at main's passes above the total rounded down" "$(lowered_coverage 86 871)" 0
check "a floor raised above main's passes below the total rounded down" "$(lowered_coverage 87 885)" 0
check "a floor missing where main has one fails" "$(lowered_coverage none 871)" 1

echo
if (( FAILED )); then echo "FAIL: coverage"; exit 1; fi
echo "PASS: coverage"
