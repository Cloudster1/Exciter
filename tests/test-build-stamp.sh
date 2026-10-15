#!/usr/bin/env bash
# Tests for scripts/build-stamp.sh: the version and commit every image and binary is stamped with, on a tag, past
# one, with none and outside git. Runs the real script in throwaway directories; needs bash and git only.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"

export GIT_AUTHOR_NAME=Tester GIT_AUTHOR_EMAIL=tester@example.com
export GIT_COMMITTER_NAME=Tester GIT_COMMITTER_EMAIL=tester@example.com
export GIT_CONFIG_COUNT=2
export GIT_CONFIG_KEY_0=maintenance.auto GIT_CONFIG_VALUE_0=false
export GIT_CONFIG_KEY_1=gc.auto GIT_CONFIG_VALUE_1=0

W="$(mktemp -d)" || exit 1
trap 'rm -rf "${W}"' EXIT
# A directory outside git must not find the checkout this test runs from.
export GIT_CEILING_DIRECTORIES="${W}"

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }
check() { if [[ "$2" == "$3" ]]; then pass "$1"; else fail "$1: expected [$3], got [$2]"; fi; }

D="${W}/checkout"
mkdir -p "${D}/scripts"
cp "${SRC}/scripts/build-stamp.sh" "${D}/scripts/"
printf '0.1.0\n' >"${D}/VERSION"
stamp() { "${D}/scripts/build-stamp.sh" "$1"; }

echo "> outside git"
check "the version says it is a development build" "$(stamp version)" "0.1.0+dev"
check "there is no commit" "$(stamp commit)" ""

echo "> in git with no tag"
git -C "${D}" init -q -b main
git -C "${D}" add -A
git -C "${D}" commit -qm "feat: the first feature"
HEAD="$(git -C "${D}" rev-parse HEAD)"
check "the version says it is a development build" "$(stamp version)" "0.1.0+dev"
check "the commit is the short hash" "$(stamp commit)" "${HEAD:0:7}"

echo "> on a release tag"
git -C "${D}" tag -a v0.1.0 -m v0.1.0
check "the version is the tag without its v" "$(stamp version)" "0.1.0"

echo "> past a release tag"
git -C "${D}" commit -q --allow-empty -m "fix: one more"
HEAD="$(git -C "${D}" rev-parse HEAD)"
check "the version counts the commits since and names the hash" "$(stamp version)" "0.1.0+1.g${HEAD:0:7}"
check "the commit is the new short hash" "$(stamp commit)" "${HEAD:0:7}"

echo "> asked for nothing"
"${D}/scripts/build-stamp.sh" >/dev/null 2>&1
check "it refuses" "$?" "2"

if (( FAILED )); then
    echo "build-stamp.sh tests failed"
    exit 1
fi
echo "build-stamp.sh tests passed"
