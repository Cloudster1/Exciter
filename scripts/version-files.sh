#!/usr/bin/env bash
# The one list of the files that carry the release version, so release.sh, the release workflow and the tests
# agree on it. Usage: version-files.sh show | check [X.Y.Z] | set X.Y.Z | files
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
# The chart carries the version once #104 adds it; until then there is none to keep in step.
CHART="deploy/charts/exciter/Chart.yaml"
# A package.json carries a version only if somebody gave it one; once it
# does, it has to move with the others.
PACKAGES=(web/package.json)
SEMVER_RE='^[0-9]+\.[0-9]+\.[0-9]+$'

has_chart() { [[ -f "$root/$CHART" ]]; }
has_version() { [[ -f "$root/$1" ]] && grep -q '^  "version":' "$root/$1"; }
chart_field() { sed -n "s/^$1: *\"\{0,1\}\([^\"]*\)\"\{0,1\} *\$/\1/p" "$root/$CHART"; }
package_version() { sed -n 's/^  "version": *"\([^"]*\)",\{0,1\}$/\1/p' "$root/$1"; }

show() {
    printf 'VERSION\t%s\n' "$(tr -d '[:space:]' <"$root/VERSION")"
    if has_chart; then
        printf '%s version\t%s\n' "$CHART" "$(chart_field version)"
        printf '%s appVersion\t%s\n' "$CHART" "$(chart_field appVersion)"
    fi
    local p
    for p in "${PACKAGES[@]}"; do
        if has_version "$p"; then printf '%s\t%s\n' "$p" "$(package_version "$p")"; fi
    done
    return 0
}

case "${1:-}" in
show)
    show
    ;;
check)
    want="${2:-}"
    [[ -n "$want" ]] || want=$(tr -d '[:space:]' <"$root/VERSION")
    bad=$(show | awk -F'\t' -v want="$want" '$2 != want { printf "   %s says %s\n", $1, ($2 == "" ? "nothing" : $2) }')
    if [[ -n "$bad" ]]; then
        echo "The release version files disagree with $want:" >&2
        echo "$bad" >&2
        echo "Run scripts/version-files.sh set $want, or release.sh, which writes them all." >&2
        exit 1
    fi
    ;;
set)
    want="${2:-}"
    [[ "$want" =~ $SEMVER_RE ]] || { echo "Give the version as X.Y.Z, digits only, not '$want'." >&2; exit 2; }
    printf '%s\n' "$want" >"$root/VERSION"
    if has_chart; then
        sed -i -E "s/^version: .*/version: $want/; s/^appVersion: .*/appVersion: \"$want\"/" "$root/$CHART"
    fi
    for p in "${PACKAGES[@]}"; do
        if [[ -f "$root/$p" ]]; then sed -i -E "s/^(  \"version\": *)\"[^\"]*\"/\1\"$want\"/" "$root/$p"; fi
    done
    "$0" check "$want"
    ;;
files)
    echo VERSION
    if has_chart; then echo "$CHART"; fi
    for p in "${PACKAGES[@]}"; do
        if has_version "$p"; then echo "$p"; fi
    done
    ;;
*)
    echo "Say what to do: $0 show, $0 check [X.Y.Z], $0 set X.Y.Z or $0 files." >&2
    exit 2
    ;;
esac
