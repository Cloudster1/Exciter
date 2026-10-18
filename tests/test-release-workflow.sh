#!/usr/bin/env bash
# Tests for .github/workflows/release.yml, which runs only for a release: actionlint passes, it pushes the images the
# stack builds and the chart runs, under the chart's names, the packed chart has the version, and every step is there.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
WF="${SRC}/.github/workflows/release.yml"
COMPOSE="${SRC}/deploy/docker-compose.yml"
VALUES="${SRC}/deploy/charts/exciter/values.yaml"

W="$(mktemp -d)" || exit 1
trap 'rm -rf "${W}"' EXIT

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
# An image whose tag is empty runs at the chart's appVersion, which only a release publishes.
released=$(sed -n 's/^  \([a-z]*\): { repository: \([^,]*\), tag: "" }$/\1 \2/p' "${VALUES}")
[[ -n "${released}" ]] || fail "values.yaml lists no image with an empty tag this test can read"
while read -r name repository; do
    [[ -n "${name}" ]] || continue
    check "the chart pulls ${name} from where release.yml pushes it" "${repository}" "${prefix}-${name}"
done <<<"${released}"
# A tool image is one a pipeline runs, not the chart, such as exciter-ci (#200).
TOOL_IMAGES="ci"
run=$(tr ' ' '\n' <<<"${pushed}" | grep -vxF -f <(tr ' ' '\n' <<<"${TOOL_IMAGES}") | paste -sd' ')
check "release.yml pushes exactly the images the chart runs at its appVersion, and the tools" \
    "${run}" "$(cut -d' ' -f1 <<<"${released}" | sort | paste -sd' ')"
for tool in ${TOOL_IMAGES}; do
    grep -qE "^  ${tool}: " "${VALUES}" && fail "the chart runs ${tool}, which is a pipeline's tool" || pass "the chart leaves ${tool} to a pipeline"
done
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
has "the chart is packaged" "make chart-package"
has "the chart is pushed" "make chart-push"
has "the chart goes to an OCI repository" "CHART_REPOSITORY: oci://"
has "the chart is attached to the release" 'gh release upload "$TAG" "dist/exciter-$VERSION.tgz"'
has "the release notes come from the tag message" "%(contents)"
has "Dynamo is told which tool was released" 'tool: "exciter"'
has "Dynamo hears it as tool-released" 'event_type: "tool-released"'
has "a missing Dynamo token is a notice, not a failure" "::notice::DYNAMO_DISPATCH_TOKEN is not set"

echo "== the packaged chart carries the release version"
version=$(tr -d '[:space:]' <"${SRC}/VERSION")
if out=$(make -s -C "${SRC}" chart-package CHART_DIST="${W}" 2>&1); then
    pass "make chart-package packs the chart"
    chart_yaml=$(tar -xzOf "${W}/exciter-${version}.tgz" exciter/Chart.yaml 2>/dev/null)
    check "the package is named for VERSION" "$([[ -n "${chart_yaml}" ]] && echo found)" "found"
    check "its version is VERSION" "$(sed -n 's/^version: //p' <<<"${chart_yaml}")" "${version}"
    check "its appVersion is VERSION" "$(sed -n 's/^appVersion: //p' <<<"${chart_yaml}" | tr -d '"')" "${version}"
    check "it carries the subchart, so installing it needs no other repository" \
        "$(tar -tzf "${W}/exciter-${version}.tgz" | grep -c '^exciter/charts/seaweedfs/Chart.yaml$')" "1"
else
    fail "make chart-package failed"
    sed 's/^/      /' <<<"${out}"
fi

if (( FAILED )); then
    echo "release workflow tests failed"
    exit 1
fi
echo "release workflow tests passed"
