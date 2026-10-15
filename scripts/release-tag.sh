#!/usr/bin/env bash
# Tags the merge commit of release/vX.Y.Z as vX.Y.Z, annotated with its CHANGELOG section; again for the same
# commit it does nothing, so a failed release run can be re-run. Usage: release-tag.sh release/vX.Y.Z <commit> [remote]
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
branch="${1:-}"
commit="${2:-}"
remote="${3:-origin}"

if [[ ! "$branch" =~ ^release/v([0-9]+\.[0-9]+\.[0-9]+)$ ]]; then
    echo "'$branch' is no release branch; release.sh names them release/vX.Y.Z." >&2
    exit 2
fi
version="${BASH_REMATCH[1]}"
tag="v$version"
[[ -n "$commit" ]] || { echo "Name the merge commit to tag." >&2; exit 2; }
commit=$(git -C "$root" rev-parse --verify "$commit^{commit}")

default=$(git -C "$root" symbolic-ref --quiet --short "refs/remotes/$remote/HEAD" 2>/dev/null | sed "s|^$remote/||" || true)
default="${default:-main}"
git -C "$root" fetch --quiet "$remote" "+refs/heads/$default:refs/remotes/$remote/$default"
# A tag on a commit the default branch lacks would publish code nobody
# reviewed into it, which is the one thing this flow exists to prevent.
if ! git -C "$root" merge-base --is-ancestor "$commit" "$remote/$default"; then
    echo "$commit is not on $remote/$default; a release is tagged only on the merge commit of its pull request." >&2
    exit 1
fi
if [[ "$(git -C "$root" rev-parse HEAD)" != "$commit" ]]; then
    echo "Check out $commit first: the version files are read from the working tree." >&2
    exit 1
fi
"$root/scripts/version-files.sh" check "$version"
notes=$("$root/scripts/release-notes.sh" "$version" "$root/CHANGELOG.md")

tagged=$(git -C "$root" ls-remote --tags "$remote" "refs/tags/$tag" "refs/tags/$tag^{}" | awk '{ print $1 }' | tail -n1)
if [[ -n "$tagged" ]]; then
    if [[ "$tagged" == "$commit" ]]; then
        echo "$tag already points at $commit."
        exit 0
    fi
    echo "$tag already exists on $remote and points at $tagged, not at $commit. Releases are never moved; release the next version instead." >&2
    exit 1
fi

if ! git -C "$root" rev-parse --quiet --verify "refs/tags/$tag" >/dev/null; then
    git -C "$root" tag -a "$tag" -F - "$commit" <<<"$notes"
elif [[ "$(git -C "$root" rev-parse "$tag^{commit}")" != "$commit" ]]; then
    echo "A local $tag points elsewhere; delete it (git tag -d $tag) and run this again." >&2
    exit 1
fi
git -C "$root" push --quiet "$remote" "refs/tags/$tag"
echo "Tagged $commit as $tag on $remote."
