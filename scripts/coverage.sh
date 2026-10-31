#!/usr/bin/env bash
# Keeps a coverage report's figures for the summary and fails below coverage/<name>.floor (#334).
# Usage: scripts/coverage.sh go <name> <go test -coverprofile file> | web <name> <vitest coverage-summary.json>
set -euo pipefail

kind="$1" name="$2" source="$3"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REPORTS="${REPORTS:-$ROOT/reports}"
floor_file="$ROOT/coverage/$name.floor"
out="$REPORTS/coverage-$name.tsv"

[[ -f "$source" ]] || { echo "No coverage report at $source; run the layer that writes it first." >&2; exit 2; }
mkdir -p "$REPORTS"

case "$kind" in
  go)
    # A block can be listed by more than one test binary; it counts once, as covered when any run reached it.
    awk -F'[: ,]' 'NR > 1 && NF >= 4 {
        key = $1 ":" $2 "," $3
        stmts[key] = $(NF - 1)
        if ($NF > 0) hit[key] = 1
        pkg = $1; sub(/\/[^\/]*$/, "", pkg); sub(/^github.com\/cloudster1\/exciter\/backend\//, "", pkg)
        owner[key] = pkg
      }
      # Cut down, never rounded up, so a total just below its floor cannot read as the floor (#465).
      function down(c, t) { return t ? int(1000 * c / t) / 10 : 0 }
      END {
        for (k in stmts) { p = owner[k]; all[p] += stmts[k]; total += stmts[k]; if (k in hit) { got[p] += stmts[k]; covered += stmts[k] } }
        printf "total\t%.1f\t%d\n", down(covered, total), total
        for (p in all) printf "%s\t%.1f\t%d\n", p, down(got[p], all[p]), all[p] | "sort"
      }' "$source" > "$out"
    ;;
  web)
    # Lines per file, gathered by the folder under src/ a file sits in two levels down, as features/dashboard.
    jq -r '
      . as $all
      | "total\t\(if $all.total.lines.total > 0 then (1000 * $all.total.lines.covered / $all.total.lines.total | floor) / 10 else 0 end)\t\($all.total.lines.total)",
        ([to_entries[] | select(.key != "total")
          | {dir: (.key | sub(".*/web/src/"; "") | split("/") | .[0:2] | map(select(test("\\.") | not)) | join("/") | if . == "" then "src" else . end),
             total: .value.lines.total, covered: .value.lines.covered}]
         | group_by(.dir)[]
         | {dir: .[0].dir, total: (map(.total) | add), covered: (map(.covered) | add)}
         | select(.total > 0)
         | "\(.dir)\t\((1000 * .covered / .total | floor) / 10)\t\(.total)")' "$source" > "$out"
    ;;
  *)
    echo "Unknown kind $kind; give go or web." >&2
    exit 2
    ;;
esac

total="$(awk -F'\t' '$1 == "total" { print $2 }' "$out")"
floor="$(tr -d '[:space:]' < "$floor_file" 2>/dev/null || echo 0)"
printf 'floor\t%s\n' "$floor" >> "$out"
if awk -v t="$total" -v f="$floor" 'BEGIN { exit !(t + 0 < f + 0) }'; then
  echo "$name coverage is $total%, below its floor of $floor% in coverage/$name.floor. Test what the change added, or, if the measurement itself changed, lower the floor to the new total rounded down, in a commit with the trailer Floor-lowered: $name <new floor> <reason> (CONTRIBUTING.md, Testing)."
  exit 1
fi
echo "$name coverage is $total% (floor $floor%)."
