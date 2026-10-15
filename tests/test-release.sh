#!/usr/bin/env bash
# Tests for release.sh and scripts/release-tag.sh: the version, the notes, the files, branch and pull request it
# makes, the tag on the merge, and every refusal, against a throwaway repository, a bare remote and a stand-in gh.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"

export GIT_AUTHOR_NAME=Tester GIT_AUTHOR_EMAIL=tester@example.com
export GIT_COMMITTER_NAME=Tester GIT_COMMITTER_EMAIL=tester@example.com

# Background auto maintenance once still wrote into .git/objects while the next case removed the repository,
# and that case then ran in something that was no repository at all.
export GIT_CONFIG_COUNT=2
export GIT_CONFIG_KEY_0=maintenance.auto GIT_CONFIG_VALUE_0=false
export GIT_CONFIG_KEY_1=gc.auto GIT_CONFIG_VALUE_1=0

# A broken fixture makes every later case fail for the wrong reason, so the
# script stops right after the command that broke it has printed its error.
fixture_failed() {
    echo "The test fixture broke while $1. The error above is the cause; fix it and run the script again." >&2
    exit 1
}

W="$(mktemp -d)" || fixture_failed "creating the temporary directory"
trap 'rm -rf "${W}"' EXIT

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }
check() { if [[ "$2" == "$3" ]]; then pass "$1"; else fail "$1: expected [$3], got [$2]"; fi; }

REPO="${W}/repo"

# One prepared repository is copied per case, so each case can state the
# version it expects instead of tracking what its predecessors released.
build_pristine() { # target tagged
    local dir="$1" tagged="$2"
    rm -rf "${dir}" "${dir}.remote" || fixture_failed "removing ${dir}"
    mkdir -p "${dir}" || fixture_failed "creating ${dir}"
    cp "${SRC}/release.sh" "${dir}/release.sh" || fixture_failed "copying release.sh"
    mkdir -p "${dir}/scripts" "${dir}/deploy/charts/exciter" "${dir}/web" || fixture_failed "creating the directories of ${dir}"
    cp "${SRC}/scripts/version-files.sh" "${SRC}/scripts/release-notes.sh" "${SRC}/scripts/release-tag.sh" "${dir}/scripts/" \
        || fixture_failed "copying the release scripts"
    printf '0.1.0\n' > "${dir}/VERSION" || fixture_failed "writing VERSION"
    printf 'apiVersion: v2\nname: exciter\nversion: 0.1.0\nappVersion: "0.1.0"\n' > "${dir}/deploy/charts/exciter/Chart.yaml" \
        || fixture_failed "writing Chart.yaml"
    # The real web/package.json carries no version; this one does, so the test
    # shows that one which does is kept in step.
    printf '{\n  "name": "exciter-web",\n  "version": "0.1.0",\n  "private": true\n}\n' > "${dir}/web/package.json" \
        || fixture_failed "writing web/package.json"
    cat > "${dir}/CHANGELOG.md" <<'EOF' || fixture_failed "writing CHANGELOG.md"
# Changelog

All notable changes to this project are documented here.

## [Unreleased]
EOF
    printf 'release-notes-*.md\n' > "${dir}/.gitignore" || fixture_failed "writing .gitignore"
    git -C "${dir}" init -q -b main || fixture_failed "initialising ${dir}"
    git -C "${dir}" add -A || fixture_failed "staging the files in ${dir}"
    git -C "${dir}" commit -qm "feat: the first feature" || fixture_failed "committing in ${dir}"
    git init -q --bare "${dir}.remote" || fixture_failed "initialising ${dir}.remote"
    git -C "${dir}" remote add origin "${dir}.remote" || fixture_failed "adding the remote to ${dir}"
    git -C "${dir}" push -q -u origin main || fixture_failed "pushing ${dir} to its remote"
    git -C "${dir}" remote set-head origin main || fixture_failed "setting origin/HEAD in ${dir}"
    if [[ "${tagged}" == tagged ]]; then
        git -C "${dir}" tag -a v0.1.0 -m "v0.1.0" || fixture_failed "tagging v0.1.0 in ${dir}"
    fi
}

use_fixture() { # tagged|untagged
    rm -rf "${REPO}" "${REPO}.remote" || fixture_failed "removing the previous case's repository"
    cp -a "${W}/pristine-$1" "${REPO}" || fixture_failed "copying the $1 fixture"
    cp -a "${W}/pristine-$1.remote" "${REPO}.remote" || fixture_failed "copying the $1 fixture's remote"
    git -C "${REPO}" remote set-url origin "${REPO}.remote" || fixture_failed "pointing the copy at its remote"
}

commit() { # subject [body]
    local args=(-m "$1")
    (( $# > 1 )) && args+=(-m "$2")
    git -C "${REPO}" commit -q --allow-empty "${args[@]}"
    git -C "${REPO}" push -q origin main
}

# GitHub is the one thing this test cannot reach, so gh is a stand-in that
# records what it was asked and keeps the body of the pull request.
mkdir -p "${W}/bin" || fixture_failed "creating the directory for the gh stand-in"
cat > "${W}/bin/gh" <<'EOF' || fixture_failed "writing the gh stand-in"
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${GH_LOG}"
[[ -n "${GH_FAIL:-}" ]] && { echo "gh: not signed in" >&2; exit 1; }
while (( $# )); do
    [[ "$1" == --body-file ]] && cp "$2" "${GH_LOG}.body"
    shift
done
echo "https://github.com/Cloudster1/Exciter/pull/999"
EOF
chmod +x "${W}/bin/gh" || fixture_failed "making the gh stand-in executable"
export GH_LOG="${W}/gh.log"

run_release() {
    rm -f "${GH_LOG}" "${GH_LOG}.body"
    (cd "${REPO}" && PATH="${W}/bin:${PATH}" ./release.sh "$@") > "${W}/out.log" 2>&1
}

released_version() { cat "${REPO}/VERSION"; }
tree_state() { git -C "${REPO}" status --porcelain; }

expect_release() { # args...
    if ! run_release "$@"; then
        fail "release.sh exited non-zero"
        sed 's/^/      /' "${W}/out.log"
        return 1
    fi
}

build_pristine "${W}/pristine-tagged" tagged
build_pristine "${W}/pristine-untagged" untagged

# --- Version derivation ---

echo "== release.sh: the next version is inferred from the commits"

use_fixture tagged
commit "fix: repair one thing"
commit "fix: repair another thing"
expect_release && {
    check "a fix-only history bumps the patch" "$(released_version)" "0.1.1"
    check "the deciding rule is printed" \
        "$(grep -c 'a fix -> PATCH' "${W}/out.log")" "1"
    check "the previous version is printed" \
        "$(grep -c 'Previous version: 0.1.0' "${W}/out.log")" "1"
    check "the counts per type are printed" \
        "$(grep -c 'Commits since:    2 (fix: 2)' "${W}/out.log")" "1"
}

use_fixture tagged
commit "fix: repair one thing"
commit "feat: add a reminder setting"
expect_release && {
    check "a feat bumps the minor" "$(released_version)" "0.2.0"
    check "the feat rule is printed" \
        "$(grep -c 'a feat -> MINOR' "${W}/out.log")" "1"
}

use_fixture tagged
commit "feat!: drop the old catalog format"
expect_release && {
    check "a breaking change stays below 1.0.0" "$(released_version)" "0.2.0"
    check "the degraded breaking rule is printed" \
        "$(grep -c 'below 1.0.0 -> MINOR' "${W}/out.log")" "1"
}

use_fixture tagged
commit "refactor: split the schedule engine" "BREAKING CHANGE: stored schedules are rebuilt"
expect_release && \
    check "a BREAKING CHANGE trailer counts as breaking" "$(released_version)" "0.2.0"

use_fixture tagged
for type in chore docs test build ci refactor style perf; do
    commit "${type}: housekeeping for ${type}"
done
expect_release && {
    check "housekeeping alone bumps the patch" "$(released_version)" "0.1.1"
    check "housekeeping alone is called out" \
        "$(grep -c 'nothing user-facing -> PATCH' "${W}/out.log")" "1"
}

use_fixture untagged
commit "feat: add another feature"
expect_release && {
    check "the first release keeps the version in VERSION" \
        "$(released_version)" "0.1.0"
    check "the first-release rule is printed" \
        "$(grep -c 'nothing released yet' "${W}/out.log")" "1"
}

echo "== release.sh: an empty history aborts without touching anything"
use_fixture tagged
BEFORE="$(tree_state)$(git -C "${REPO}" tag -l)"
if run_release; then
    fail "an empty history was released"
else
    pass "an empty history is rejected"
fi
check "the empty history changed nothing" "$(tree_state)$(git -C "${REPO}" tag -l)" "${BEFORE}"
check "the abort names the tag it looked at" \
    "$(grep -c 'No commits since v0.1.0' "${W}/out.log")" "1"

echo "== release.sh: a version given by hand wins over the inferred one"
use_fixture tagged
commit "feat: would infer a minor bump"
expect_release 0.4.2 && {
    check "the given version is used verbatim" "$(released_version)" "0.4.2"
    check "nothing is inferred when a version is given" \
        "$(grep -c 'Working out the next version' "${W}/out.log")" "0"
}

echo "== release.sh: a malformed version is rejected before anything is written"
use_fixture tagged
commit "fix: something"
BEFORE="$(tree_state)$(git -C "${REPO}" tag -l)"
if run_release "0.2.0-rc1"; then
    fail "a malformed version was accepted"
else
    pass "a malformed version is rejected"
fi
check "the rejected run changed nothing" "$(tree_state)$(git -C "${REPO}" tag -l)" "${BEFORE}"

# --- What the release writes ---

echo "== release.sh: version files, changelog, notes, branch and pull request"
use_fixture tagged
commit "feat: add a reminder setting"
commit "fix: stop the timeline flickering (#42)"
commit "chore: bump a dependency"
commit "Update the README by hand"
commit "feat!: drop the old catalog format"
# GitHub reads a skip marker anywhere in the head commit message of a push, so quoting such a subject in
# the notes would skip the release build.
commit "chore: record the catalog source texts as of 2026-09-23 [skip ci]"
expect_release && {
    check "VERSION carries the new version" "$(released_version)" "0.2.0"

    NOTES="${REPO}/release-notes-v0.2.0.md"
    if [[ -f "${NOTES}" ]]; then
        pass "the release notes file is written"
    else
        fail "the release notes file is missing"
    fi

    section_of() { # item -> heading it landed under
        python3 - "${NOTES}" "$1" <<'EOF'
import sys

heading = ""
with open(sys.argv[1]) as f:
    for line in f:
        line = line.rstrip("\n")
        if line.startswith("### "):
            heading = line[4:]
        elif sys.argv[2] in line:
            print(heading, end="")
            break
    else:
        print("<missing>", end="")
EOF
    }

    check "a feature lands under Features" \
        "$(section_of 'add a reminder setting')" "Features"
    check "a fix lands under Bug fixes" \
        "$(section_of 'stop the timeline flickering')" "Bug fixes"
    check "a chore lands under Other" "$(section_of 'bump a dependency')" "Other"
    check "a non-conforming subject is not lost" \
        "$(section_of 'Update the README by hand')" "Other"
    check "an exclamation mark makes a breaking change" \
        "$(section_of 'drop the old catalog format')" "Breaking changes"
    check "breaking changes come first" \
        "$(grep -m1 '^### ' "${NOTES}")" "### Breaking changes"
    check "issue references are kept" \
        "$(grep -c 'stop the timeline flickering (#42)' "${NOTES}")" "1"

    check "a quoted skip-ci marker does not reach the notes" \
        "$(grep -c 'skip ci' "${NOTES}")" "0"
    check "the commit that carried it is still listed" \
        "$(section_of 'record the catalog source texts')" "Other"
    check "no skip-ci marker reaches the release commit" \
        "$(cd "${REPO}" && git log -1 --format='%B' | grep -ci 'skip ci')" "0"

    check "the changelog carries the released version and date" \
        "$(grep -m1 "^## \[0" "${REPO}/CHANGELOG.md")" \
        "## [0.2.0] - $(date -u +%Y-%m-%d)"
    check "an empty Unreleased section is kept for the next round" \
        "$(grep -c '^## \[Unreleased\]$' "${REPO}/CHANGELOG.md")" "1"
    check "the changelog header is not repeated" \
        "$(grep -c '^# Changelog$' "${REPO}/CHANGELOG.md")" "1"

    check "the chart's version moves with VERSION" \
        "$(sed -n 's/^version: //p' "${REPO}/deploy/charts/exciter/Chart.yaml")" "0.2.0"
    check "the chart's appVersion moves with VERSION" \
        "$(sed -n 's/^appVersion: //p' "${REPO}/deploy/charts/exciter/Chart.yaml")" '"0.2.0"'
    check "a package.json that carries a version moves with it" \
        "$(grep -c '"version": "0.2.0"' "${REPO}/web/package.json")" "1"
    check "the version files agree afterwards" \
        "$("${REPO}/scripts/version-files.sh" check 0.2.0 2>&1 && echo agree)" "agree"

    check "the release travels on a branch of its own" \
        "$(git -C "${REPO}" branch --show-current)" "release/v0.2.0"
    check "the release commit subject stays one short line" \
        "$(git -C "${REPO}" log -1 --format=%s)" "release: v0.2.0"
    check "only the release files are committed" \
        "$(git -C "${REPO}" show --name-only --format= HEAD | LC_ALL=C sort | tr '\n' ' ')" \
        "CHANGELOG.md VERSION deploy/charts/exciter/Chart.yaml web/package.json "
    check "the working tree is left clean" "$(tree_state)" ""

    check "release.sh sets no tag; the merge commit gets it" \
        "$(git -C "${REPO}" tag -l 'v0.2.*')$(git -C "${REPO}.remote" tag -l 'v0.2.*')" ""
    check "the release branch is pushed" \
        "$(git -C "${REPO}.remote" rev-parse --verify -q release/v0.2.0)" "$(git -C "${REPO}" rev-parse HEAD)"
    check "main on the remote is left alone" \
        "$(git -C "${REPO}.remote" rev-parse main)" "$(git -C "${REPO}" rev-parse origin/main)"
    check "the release commit sits on top of main" \
        "$(git -C "${REPO}" rev-list --count 'origin/main..HEAD')" "1"

    check "a pull request into main is opened" \
        "$(cut -d' ' -f1-9 "${GH_LOG}" 2>/dev/null)" \
        "pr create --base main --head release/v0.2.0 --title release: v0.2.0"
    check "the pull request carries the notes" \
        "$(grep -c 'add a reminder setting' "${GH_LOG}.body" 2>/dev/null)" "1"
    check "the notes are printed for pasting" \
        "$(grep -c 'add a reminder setting' "${W}/out.log")" "1"

    if [[ -d "${REPO}/dist" || -d "${REPO}/build" ]]; then
        fail "release.sh built an artifact"
    else
        pass "release.sh builds no artifact"
    fi
}

# What the release workflow does once the pull request is merged; the merge
# is done here by hand, as GitHub would.
echo "== release-tag.sh: the merge commit becomes the release"
RELEASE_COMMIT="$(git -C "${REPO}" rev-parse HEAD)"
git -C "${REPO}" switch -q main
check "the release commit is not on main before the merge" \
    "$(cd "${REPO}" && scripts/release-tag.sh release/v0.2.0 "${RELEASE_COMMIT}" >/dev/null 2>&1 && echo tagged || echo refused)" "refused"
git -C "${REPO}" merge -q --no-ff -m "Merge pull request #999 from Cloudster1/release/v0.2.0" release/v0.2.0
git -C "${REPO}" push -q origin main
MERGE_COMMIT="$(git -C "${REPO}" rev-parse HEAD)"
if (cd "${REPO}" && scripts/release-tag.sh release/v0.2.0 "${MERGE_COMMIT}") > "${W}/tag.log" 2>&1; then
    pass "the merge commit is tagged"
else
    fail "the merge commit was not tagged"
    sed 's/^/      /' "${W}/tag.log"
fi
check "the tag on the remote points at the merge commit" \
    "$(git -C "${REPO}.remote" rev-parse -q --verify 'v0.2.0^{commit}')" "${MERGE_COMMIT}"
check "the tag is annotated" "$(git -C "${REPO}.remote" cat-file -t v0.2.0)" "tag"
check "the tag message is the release's changelog section" \
    "$(git -C "${REPO}.remote" tag -l --format='%(contents)' v0.2.0 | grep -c 'add a reminder setting')" "1"
check "the tag message carries no other release" \
    "$(git -C "${REPO}.remote" tag -l --format='%(contents)' v0.2.0 | grep -c '^## ')" "0"
if (cd "${REPO}" && scripts/release-tag.sh release/v0.2.0 "${MERGE_COMMIT}") > "${W}/tag.log" 2>&1; then
    pass "tagging the same merge again is a no-op, so the workflow can be re-run"
else
    fail "tagging the same merge again failed"
    sed 's/^/      /' "${W}/tag.log"
fi
check "a non-release branch is refused" \
    "$(cd "${REPO}" && scripts/release-tag.sh fix/12-something HEAD >/dev/null 2>&1 && echo tagged || echo refused)" "refused"
check "a tag is never moved to another commit" \
    "$(cd "${REPO}" && git commit -q --allow-empty -m 'chore: later' && git push -q origin main && scripts/release-tag.sh release/v0.2.0 HEAD >/dev/null 2>&1 && echo moved || echo refused)" "refused"
git -C "${REPO}" reset -q --hard "${MERGE_COMMIT}"
git -C "${REPO}" push -q -f origin main
check "a merge whose files say another version is refused" \
    "$(cd "${REPO}" && scripts/release-tag.sh release/v0.3.0 HEAD 2>&1 | grep -c 'disagree with 0.3.0')" "1"

echo "== release.sh: a second release prepends to CHANGELOG.md"
commit "fix: repair the second thing"
expect_release && {
    check "the earlier section is still there" \
        "$(grep -c '^## \[0.2.0\]' "${REPO}/CHANGELOG.md")" "1"
    check "the newest section is on top" \
        "$(grep -m1 '^## \[0' "${REPO}/CHANGELOG.md")" \
        "## [0.2.1] - $(date -u +%Y-%m-%d)"
    check "the header is not repeated" \
        "$(grep -c '^# Changelog$' "${REPO}/CHANGELOG.md")" "1"
    check "the previous tag bounds the history" \
        "$(grep -c 'add a reminder setting' "${REPO}/release-notes-v0.2.1.md")" "0"
    check "the version keeps climbing" "$(released_version)" "0.2.1"
}

echo "== release.sh: a hand-written Unreleased section becomes the release"
use_fixture tagged
commit "feat: add something the changelog already describes"
printf '\n### Features\n\n- A sentence written by hand\n' >> "${REPO}/CHANGELOG.md"
git -C "${REPO}" commit -qam "docs: fill in the changelog"
git -C "${REPO}" push -q origin main
expect_release && {
    check "the hand-written entry is released" \
        "$(grep -c 'A sentence written by hand' "${REPO}/release-notes-v0.2.0.md")" "1"
    check "the commit subjects do not duplicate it" \
        "$(grep -c 'add something the changelog already describes' "${REPO}/release-notes-v0.2.0.md")" "0"
    check "the Unreleased section is emptied" \
        "$(sed -n '/^## \[Unreleased\]$/,/^## \[0/p' "${REPO}/CHANGELOG.md" | grep -c '^- ')" "0"
}

echo "== release.sh: a changelog argument overrides everything generated"
use_fixture tagged
commit "feat: add something generated"
expect_release 0.2.0 "Handwritten note" && {
    check "the manual changelog reaches the notes" \
        "$(cat "${REPO}/release-notes-v0.2.0.md")" "Handwritten note"
    check "the generated notes are not used" \
        "$(grep -c 'add something generated' "${REPO}/CHANGELOG.md")" "0"
}

# --- Refusals and the dry run ---

echo "== release.sh: --dry-run changes nothing"
use_fixture tagged
commit "feat: add a reminder setting"
BEFORE_TREE="$(tree_state)"
BEFORE_TAGS="$(git -C "${REPO}" tag -l)"
BEFORE_HEAD="$(git -C "${REPO}" rev-parse HEAD)"
expect_release --dry-run && {
    check "the dry run leaves the working tree clean" "$(tree_state)" "${BEFORE_TREE}"
    check "the dry run creates no tag" "$(git -C "${REPO}" tag -l)" "${BEFORE_TAGS}"
    check "the dry run creates no commit" \
        "$(git -C "${REPO}" rev-parse HEAD)" "${BEFORE_HEAD}"
    if compgen -G "${REPO}/release-notes-*.md" > /dev/null; then
        fail "the dry run wrote the notes file"
    else
        pass "the dry run wrote no notes file"
    fi
    check "the dry run prints the version it would release" \
        "$(grep -c 'Dry run: Exciter v0.2.0' "${W}/out.log")" "1"
    check "the dry run prints the notes it would write" \
        "$(grep -c 'add a reminder setting' "${W}/out.log")" "1"
    check "the dry run names the branch it would open" \
        "$(grep -c 'Branch:    release/v0.2.0' "${W}/out.log")" "1"
    check "the dry run names every file it would write" \
        "$(grep -c 'Files:     VERSION deploy/charts/exciter/Chart.yaml web/package.json CHANGELOG.md' "${W}/out.log")" "1"
    check "the dry run creates no branch" "$(git -C "${REPO}" branch --list 'release/*')" ""
    check "the dry run opens no pull request" "$(cat "${GH_LOG}" 2>/dev/null)" ""
}

echo "== release.sh: what stands in the way of a release is refused"
use_fixture tagged
commit "feat: add a reminder setting"
printf 'local edit\n' >> "${REPO}/CHANGELOG.md"
if run_release; then
    fail "a release swept an uncommitted edit along"
else
    pass "a working tree with uncommitted changes is refused"
fi
check "the dirty refusal created no branch" "$(git -C "${REPO}" branch --list 'release/*')" ""
git -C "${REPO}" checkout -q -- CHANGELOG.md

git -C "${REPO}" push -q origin main:release/v0.2.0
if run_release; then
    fail "a release reused a release branch that exists"
else
    pass "an existing release branch is refused"
fi
check "the refusal names the branch" "$(grep -c 'release/v0.2.0 already exists' "${W}/out.log")" "1"
git -C "${REPO}" push -q origin --delete release/v0.2.0

if run_release 0.1.0; then
    fail "a version that was already tagged was released again"
else
    pass "a version that was already tagged is refused"
fi
check "the refusal names the tag" "$(grep -c 'v0.1.0 already exists' "${W}/out.log")" "1"
check "the tag refusal changed nothing" "$(tree_state)$(git -C "${REPO}" branch --list 'release/*')" ""

echo "== release.sh: without gh the release still lands on its branch"
use_fixture tagged
commit "fix: something"
if GH_FAIL=1 run_release; then
    pass "a pull request that cannot be opened does not fail the release"
else
    fail "a pull request that cannot be opened failed the release"
    sed 's/^/      /' "${W}/out.log"
fi
check "the release branch is pushed anyway" \
    "$(git -C "${REPO}.remote" rev-parse --verify -q release/v0.1.1 >/dev/null && echo pushed)" "pushed"
check "the command to open it by hand is printed" \
    "$(grep -c 'gh pr create --base main --head release/v0.1.1' "${W}/out.log")" "1"

echo "== version-files.sh: the files that carry the version must agree"
check "this repository's version files agree" \
    "$("${SRC}/scripts/version-files.sh" check 2>&1 && echo agree)" "agree"
use_fixture tagged
sed -i 's/^appVersion: .*/appVersion: "0.1.1"/' "${REPO}/deploy/charts/exciter/Chart.yaml"
check "a chart appVersion that disagrees is refused" \
    "$("${REPO}/scripts/version-files.sh" check 2>&1 | grep -c 'Chart.yaml appVersion says 0.1.1')" "1"
use_fixture tagged
sed -i 's/"version": "0.1.0"/"version": "0.0.9"/' "${REPO}/web/package.json"
check "a package.json that disagrees is refused" \
    "$("${REPO}/scripts/version-files.sh" check 2>&1 | grep -c 'web/package.json says 0.0.9')" "1"
check "a version that is no semver is never written" \
    "$("${REPO}/scripts/version-files.sh" set 1.2 >/dev/null 2>&1 && echo written || echo refused)$(cat "${REPO}/VERSION")" "refused0.1.0"
use_fixture tagged
rm "${REPO}/deploy/charts/exciter/Chart.yaml"
check "without a chart the others still carry the version" \
    "$("${REPO}/scripts/version-files.sh" files | paste -sd' ')" "VERSION web/package.json"
check "and a release writes them" \
    "$("${REPO}/scripts/version-files.sh" set 0.1.1 >/dev/null 2>&1 && "${REPO}/scripts/version-files.sh" show | cut -f2 | paste -sd' ')" "0.1.1 0.1.1"

echo "== release.sh: a checkout behind its remote is refused"
use_fixture tagged
commit "feat: a commit the remote has"
git -C "${REPO}" reset -q --hard HEAD~1
BEFORE="$(tree_state)$(git -C "${REPO}" tag -l)"
if run_release; then
    fail "a release from a stale checkout was cut"
else
    pass "a release from a stale checkout is refused"
fi
check "the refusal says how far behind the checkout is" \
    "$(grep -c 'commit(s) behind origin/main' "${W}/out.log")" "1"
check "the refused run changed nothing" "$(tree_state)$(git -C "${REPO}" tag -l)" "${BEFORE}"

echo "== release.sh: a release off the default branch is refused"
use_fixture tagged
commit "fix: something"
git -C "${REPO}" switch -q -c fix/12-something
if run_release; then
    fail "a release from a feature branch was cut"
else
    pass "a release from a feature branch is refused"
fi
check "the refusal names the branch releases come from" \
    "$(grep -c 'releases are cut from' "${W}/out.log")" "1"

# --- Untrusted input ---

# Commit subjects are attacker-shaped input: they reach the version parsing and the notes through the
# environment, never interpolated into a command line or a Python source string.
echo "== release.sh: a hostile commit message executes nothing"
use_fixture tagged
HOSTILE='feat: "quoted" and `backticks` and $(touch '"${W}"'/pwned-sub) and ${W}'
commit "${HOSTILE}" "'); import os; os.system('touch ${W}/pwned-py'); ('

a body line, then another"
commit "fix: a commit with a hostile trailer" \
    "BREAKING CHANGE: \"quoted\" and \$(touch ${W}/pwned-body)"
expect_release && {
    NOTES="${REPO}/release-notes-v0.2.0.md"
    if compgen -G "${W}/pwned-*" > /dev/null; then
        fail "a commit message was executed: $(echo "${W}"/pwned-*)"
    else
        pass "a commit message is never executed"
    fi
    check "the subject survives quoting intact" \
        "$(grep -cF '"quoted" and `backticks`' "${NOTES}")" "1"
    check "the trailer survives quoting intact" \
        "$(grep -cF '"quoted" and $(touch' "${NOTES}")" "1"
    check "the trailer still makes a breaking change" \
        "$(grep -m1 '^### ' "${NOTES}")" "### Breaking changes"
    check "the release still lands" "$(released_version)" "0.2.0"
}

if (( FAILED )); then
    echo "release.sh tests failed"
    exit 1
fi
echo "release.sh tests passed"
