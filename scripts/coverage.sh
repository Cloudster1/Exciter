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

# Main's floor, so a lowered one is held to the total measured here; the shell layer runs before any total exists (#466).
# COVERAGE_FLOORS_BASE lets the shell tests name the base in a throwaway repository with no origin.
base="${COVERAGE_FLOORS_BASE:-$(git -C "$ROOT" merge-base HEAD origin/main 2>/dev/null || true)}"
before=""
[[ -z "$base" ]] || before="$(git -C "$ROOT" show "$base:coverage/$name.floor" 2>/dev/null | tr -d '[:space:]' || true)"

if [[ ! -f "$floor_file" && -n "$before" ]]; then
  echo "coverage/$name.floor is missing, but main holds $name to $before%. Restore the file; a floor goes down only as CONTRIBUTING.md, Testing, says."
  exit 1
fi
floor="$(tr -d '[:space:]' < "$floor_file" 2>/dev/null || echo 0)"
printf 'floor\t%s\n' "$floor" >> "$out"
if awk -v t="$total" -v f="$floor" 'BEGIN { exit !(t + 0 < f + 0) }'; then
  echo "$name coverage is $total%, below its floor of $floor% in coverage/$name.floor. Test what the change added, or, if the measurement itself changed, lower the floor to the new total rounded down, in a commit with the trailer Floor-lowered: $name <new floor> <reason> (CONTRIBUTING.md, Testing)."
  exit 1
fi

if [[ -n "$before" ]] && awk -v f="$floor" -v b="$before" 'BEGIN { exit !(f + 0 < b + 0) }'; then
  # Cut down to the floor's own decimals, so a floor of 86 matches 86.08 and one of 35.5 matches 35.57.
  expected="$(awk -v t="$total" -v f="$floor" 'BEGIN { d = index(f, ".") ? length(f) - index(f, ".") : 0; s = 10 ^ d; printf "%.*f", d, int(t * s + 1e-9) / s }')"
  if ! awk -v e="$expected" -v b="$before" 'BEGIN { exit !(e + 0 < b + 0) }'; then
    echo "coverage/$name.floor is lowered from main's $before to $floor, but $name measures $total%, which needs no lower floor. Set it to $expected, the total rounded down, and drop its Floor-lowered trailer."
    exit 1
  elif awk -v f="$floor" -v e="$expected" 'BEGIN { exit !(f + 0 != e + 0) }'; then
    echo "coverage/$name.floor is lowered from main's $before to $floor, but $name measures $total%. A lowered floor sits at the measured total rounded down: set it to $expected, and give that value in the commit's trailer, Floor-lowered: $name $expected <reason>."
    exit 1
  fi
fi
echo "$name coverage is $total% (floor $floor%)."
