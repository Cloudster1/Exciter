#!/usr/bin/env bash
# Tests for .github/workflows/release.yml, which runs only for a release and so cannot be tried on a pull request:
# actionlint passes, it pushes every image the stack builds, and every step a release depends on is still there.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
WF="${SRC}/.github/workflows/release.yml"
COMPOSE="${SRC}/deploy/docker-compose.yml"

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }
check() { if [[ "$2" == "$3" ]]; then pass "$1"; else fail "$1: expected [$3], got [$2]"; fi; }

echo "== release.yml passes actionlint"
if out=$(make -s -C "${SRC}" check-actions 2>&1); then
    pass "actionlint and shellcheck find nothing in .github/workflows"
else
    fail "actionlint found problems"
    sed 's/^/      /' <<<"${out}"
fi

echo "== release.yml pushes every image the stack builds"
prefix=$(sed -n 's/^  IMAGE_PREFIX: *//p' "${WF}")
check "the images go to the project's ghcr namespace, in lower case" "${prefix}" "ghcr.io/cloudster1/exciter"
pushed=$(sed -n 's/^ *- image: *//p' "${WF}" | sort | paste -sd' ')
built=$(sed -n 's,^ *dockerfile: deploy/Dockerfile\.,,p' "${COMPOSE}" | sort -u | paste -sd' ')
[[ -n "${built}" ]] || fail "docker-compose.yml builds no image this test can read"
check "release.yml pushes exactly the images docker-compose.yml builds" "${pushed}" "${built}"
check "the release summary lists the same images" \
    "$(sed -n 's/^ *for image in \(.*\); do$/\1/p' "${WF}" | tr ' ' '\n' | sort | paste -sd' ')" "${pushed}"

# A matrix entry's Dockerfile and target must exist, or the build fails only on the tag.
while IFS='|' read -r image dockerfile target; do
    [[ -f "${SRC}/${dockerfile}" ]] && pass "${image}: ${dockerfile} exists" \
        || fail "${image}: release.yml builds ${dockerfile}, which does not exist"
    if [[ -n "${target}" ]]; then
        grep -qiE "^FROM .* AS ${target}\$" "${SRC}/${dockerfile}" && pass "${image}: ${dockerfile} has the stage ${target}" \
            || fail "${image}: ${dockerfile} has no stage ${target}"
    fi
done < <(awk '
    /^ *- image: / { if (image) print image "|" file "|" target; image = $3; file = ""; target = "" }
    /^ *dockerfile: / { file = $2 }
    /^ *target: / { target = $2; gsub(/"/, "", target) }
    /^    steps:/ && image { print image "|" file "|" target; image = "" }
' "${WF}")

echo "== release.yml covers every step of a release"
has() { grep -qF -- "$2" "${WF}" && pass "$1" || fail "$1: release.yml no longer contains: $2"; }
has "a merged release pull request starts it" "types: [closed]"
has "only a release branch is tagged" "startsWith(github.event.pull_request.head.ref, 'release/v')"
has "the tag goes on the merge commit" 'scripts/release-tag.sh "$BRANCH" "$MERGE_COMMIT"'
has "a tag pushed by hand starts it" 'tags: ["v*"]'
has "the version files must agree with the tag" 'scripts/version-files.sh check "$version"'
has "the build stamp must say the tag's version" 'stamped=$(scripts/build-stamp.sh version)'
has "the images carry the version" 'VERSION=${{ needs.resolve.outputs.version }}'
has "the images carry the commit" 'COMMIT=${{ needs.resolve.outputs.commit }}'
has "the images are tagged with the version" ':${{ needs.resolve.outputs.version }}'
has "the images are tagged with the commit" ':${{ needs.resolve.outputs.commit }}'
has "the release notes come from the tag message" "%(contents)"
has "Dynamo is told which tool was released" 'tool: "exciter"'
has "Dynamo hears it as tool-released" 'event_type: "tool-released"'
has "a missing Dynamo token is a notice, not a failure" "::notice::DYNAMO_DISPATCH_TOKEN is not set"

if (( FAILED )); then
    echo "release workflow tests failed"
    exit 1
fi
echo "release workflow tests passed"
