#!/usr/bin/env bash
# Prints the notes of one release, the body of its "## [X.Y.Z]" section in CHANGELOG.md, which release.sh wrote.
# Usage: release-notes.sh X.Y.Z [changelog]
set -euo pipefail

version="${1:-}"
changelog="${2:-$(cd "$(dirname "$0")/.." && pwd)/CHANGELOG.md}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "Give the version as X.Y.Z, digits only, not '$version'." >&2; exit 2; }

notes=$(awk -v head="## [$version]" '
    index($0, head) == 1 { inside = 1; next }
    inside && /^## / { exit }
    inside { print }
' "$changelog" | sed '/./,$!d')

if [[ -z "$notes" ]]; then
    echo "$changelog has no section for $version; release.sh writes one, so release from its pull request." >&2
    exit 1
fi
printf '%s\n' "$notes"
