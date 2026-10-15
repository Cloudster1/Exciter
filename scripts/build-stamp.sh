#!/bin/sh
# Prints what a build says it is, for the image build arguments and the linker.
#   version  semver, as Armature's images carry it: 1.2.3 on the tag v1.2.3,
#            1.2.3+4.gabc1234 four commits past it, and VERSION+dev with no tag
#            in reach, as in CI's shallow clone or outside git
#   commit   the short hash of HEAD, or nothing outside git
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
short=7

case "${1:-}" in
version)
    described=$(git -C "$root" describe --tags --match 'v[0-9]*' --abbrev="$short" 2>/dev/null || true)
    if [ -n "$described" ]; then
        printf '%s\n' "$described" | sed -E 's/^v//; s/-([0-9]+)-g([0-9a-f]+)$/+\1.g\2/'
    else
        printf '%s+dev\n' "$(tr -d '[:space:]' <"$root/VERSION")"
    fi
    ;;
commit)
    git -C "$root" rev-parse --short="$short" HEAD 2>/dev/null || true
    ;;
*)
    echo "Say what to print: $0 version or $0 commit." >&2
    exit 2
    ;;
esac
