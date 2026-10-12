#!/usr/bin/env bash
# Runs a `go test -json` command and keeps its events for the run's summary, printing what plain `go test` would.
# Usage: scripts/go-test.sh reports/go-unit.json docker run ... go test -json ./...
set -uo pipefail

report="$1"
shift
mkdir -p "$(dirname "$report")"

"$@" | tee "$report" | jq -R --unbuffered -rj '
  fromjson? | select((.Action == "output" and .Test == null) or .Action == "build-output") | .Output'
rc=$?

jq -Rrsj '
  [splits("\n") | fromjson?] as $events
  | [$events[] | select(.Action == "fail" and .Test != null) | "\(.Package) \(.Test)"] as $failed
  | $events[] | select(.Action == "output" and .Test != null and ("\(.Package) \(.Test)" | IN($failed[])))
  | .Output' "$report"
exit "$rc"
