#!/usr/bin/env bash
set -euo pipefail

# Exciter release script
# Usage: ./release.sh [version] [changelog] [--dry-run]
#   ./release.sh                        version inferred from the commit history
#   ./release.sh 0.2.0                  version given by hand, notes generated
#   ./release.sh 0.2.0 "Added ICS"      version and changelog given by hand
#   ./release.sh --dry-run              print version and notes, change nothing
# It opens the pull request release/vX.Y.Z; merging it tags the merge commit
# vX.Y.Z, and the tag publishes the release (docs/releasing.md).

# VERSION is what the next version is worked out from; scripts/version-files.sh
# knows every other file that carries it.
VERSION_FILE="VERSION"
VERSION_FILES="scripts/version-files.sh"
CHANGELOG_FILE="CHANGELOG.md"
cd "$(dirname "$0")"

DRY_RUN=0
ARGS=()
for arg in "$@"; do
    case "${arg}" in
        --dry-run) DRY_RUN=1 ;;
        -h|--help)
            sed -n '4,11p' "$0" | sed 's|^# \?||'
            exit 0
            ;;
        *) ARGS+=("${arg}") ;;
    esac
done
if (( ${#ARGS[@]} > 2 )); then
    echo "Usage: $0 [version] [changelog] [--dry-run]"
    exit 1
fi
VERSION="${ARGS[0]:-}"
MANUAL_CHANGELOG="${ARGS[1]:-}"

if [[ -n "${VERSION}" && ! "${VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "Version must be X.Y.Z (digits only), got: ${VERSION}"
    exit 1
fi

CURRENT_VERSION=$(sed -n '1{/^[0-9]\+\.[0-9]\+\.[0-9]\+$/p}' "${VERSION_FILE}" 2>/dev/null || true)
if [[ -z "${CURRENT_VERSION}" ]]; then
    echo "${VERSION_FILE} must hold a single 'X.Y.Z' line"
    exit 1
fi

# A release built from a stale checkout tags code the remote does not have: the
# tag pushes, the branch is rejected, and the workflow builds the old commit.
BRANCH=$(git rev-parse --abbrev-ref HEAD)
DEFAULT_BRANCH=$(git symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null | sed 's|^origin/||')
DEFAULT_BRANCH="${DEFAULT_BRANCH:-main}"
if [[ "${BRANCH}" != "${DEFAULT_BRANCH}" ]]; then
    echo "Releasing from '${BRANCH}', but releases are cut from '${DEFAULT_BRANCH}'."
    echo "   git switch ${DEFAULT_BRANCH} && git merge --ff-only origin/${DEFAULT_BRANCH}"
    exit 1
fi

UPSTREAM=$(git rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null || true)
if [[ -n "${UPSTREAM}" ]]; then
    git fetch --quiet "${UPSTREAM%%/*}" || echo "   warning: could not reach ${UPSTREAM%%/*}, checking against the last fetch"
    BEHIND=$(git rev-list --count "HEAD..${UPSTREAM}")
    AHEAD=$(git rev-list --count "${UPSTREAM}..HEAD")
    if (( BEHIND > 0 )); then
        echo "${BRANCH} is ${BEHIND} commit(s) behind ${UPSTREAM}."
        echo "   Releasing now would tag code the remote does not have. Missing:"
        git log --oneline "HEAD..${UPSTREAM}" | sed 's/^/     /'
        echo ""
        echo "   git fetch origin && git merge --ff-only ${UPSTREAM}"
        exit 1
    fi
    if (( AHEAD > 0 )); then
        echo "warning: ${BRANCH} is ${AHEAD} commit(s) ahead of ${UPSTREAM}; they are released as part of this version."
    fi
else
    echo "warning: no upstream for ${BRANCH}; cannot check whether this is what the remote has."
fi

PREV_TAG=$(git describe --tags --abbrev=0 2>/dev/null || true)
RANGE="HEAD"
[[ -n "${PREV_TAG}" ]] && RANGE="${PREV_TAG}..HEAD"
# \x1f separates the fields of a commit, \x1e the commits: neither can occur in
# a subject or body, unlike any printable delimiter.
RAW_LOG=$(git log --no-merges --format="%H%x1f%s%x1f%b%x1e" "${RANGE}" 2>/dev/null || true)

# Without a version argument the number is derived from those commits, and the reasoning is printed
# before anything is written so the bump can be checked.
if [[ -z "${VERSION}" ]]; then
    # Values reach Python through the environment, never interpolated: a commit
    # subject is attacker-shaped input and would otherwise be executed.
    VERSION=$(
        RAW_LOG="${RAW_LOG}" \
        PREV_TAG="${PREV_TAG}" \
        CURRENT_VERSION="${CURRENT_VERSION}" \
        python3 - <<'PY'
import os
import re
import sys

HEADER = re.compile(
    r'^(?P<type>[A-Za-z]+)(?:\((?P<scope>[^)]*)\))?(?P<bang>!)?:\s+(?P<desc>.+)$')
TRAILER = re.compile(r'^BREAKING[ -]CHANGE:\s*')
NO_RELEASE = ('chore', 'docs', 'test', 'build', 'ci', 'refactor', 'style', 'perf')


def parts(version):
    return tuple(int(n) for n in version.split('.'))


raw = os.environ.get('RAW_LOG', '')
prev_tag = os.environ.get('PREV_TAG', '')
current = os.environ['CURRENT_VERSION']

# The tag can be ahead of VERSION (or the other way round); the next version
# has to be above both, or a release reuses a number that was already published.
base = parts(current)
if prev_tag:
    tagged = re.match(r'^v?([0-9]+\.[0-9]+\.[0-9]+)$', prev_tag)
    if tagged:
        base = max(base, parts(tagged.group(1)))

counts = {}
breaking = []
for record in raw.split('\x1e'):
    if not record.strip():
        continue
    fields = record.strip('\n').split('\x1f')
    subject = fields[1] if len(fields) > 1 else ''
    body = fields[2] if len(fields) > 2 else ''
    match = HEADER.match(subject)
    ctype = match.group('type').lower() if match else ''
    if ctype == 'release':
        continue
    counts[ctype or 'other'] = counts.get(ctype or 'other', 0) + 1
    if (match and match.group('bang')) or any(
            TRAILER.match(line.strip()) for line in body.splitlines()):
        breaking.append(subject)

total = sum(counts.values())
major, minor, patch = base

if not total:
    rule = None
elif not prev_tag:
    # Nothing has ever been released, so the number in VERSION is the one
    # that was chosen for the first release; bumping it would skip it.
    rule = 'nothing released yet -> the version in VERSION'
elif breaking and major == 0:
    # A 0.x MAJOR bump would declare 1.0; while below 1.0.0 a breaking change
    # is degraded to MINOR, as semver itself suggests.
    rule = 'a breaking change, but the project is below 1.0.0 -> MINOR'
    minor, patch = minor + 1, 0
elif breaking:
    rule = 'a breaking change -> MAJOR'
    major, minor, patch = major + 1, 0, 0
elif counts.get('feat'):
    rule = 'a feat -> MINOR'
    minor, patch = minor + 1, 0
elif counts.get('fix'):
    rule = 'a fix -> PATCH'
    patch += 1
elif all(ctype in NO_RELEASE for ctype in counts):
    rule = ('only %s and nothing user-facing -> PATCH'
            % ', '.join(sorted(counts)))
    patch += 1
else:
    rule = 'changes without a release type -> PATCH'
    patch += 1

summary = ', '.join('%s: %d' % (t, n) for t, n in sorted(counts.items())) or 'none'
log = sys.stderr
print('Working out the next version (no version given)', file=log)
print('   Previous version: %d.%d.%d (%s)'
      % (base[0], base[1], base[2], prev_tag or 'no tag yet'), file=log)
print('   Commits since:    %d (%s)' % (total, summary), file=log)
if breaking:
    print('   Breaking:         %d (%s)'
          % (len(breaking), '; '.join(breaking)), file=log)

if rule is None:
    print('   Rule:             no commits since %s - nothing to release'
          % (prev_tag or 'the first commit'), file=log)
    print('No commits since %s; nothing was changed.'
          % (prev_tag or 'the first commit'), file=log)
    sys.exit(2)

print('   Rule:             %s' % rule, file=log)
print('   Next version:     %d.%d.%d' % (major, minor, patch), file=log)
print('%d.%d.%d' % (major, minor, patch))
PY
    ) || exit 1
    INFERRED=1
else
    INFERRED=0
fi

TAG="v${VERSION}"
NOTES_FILE="release-notes-${TAG}.md"
# Main takes no direct commits, so the bump travels as a pull request of its
# own; the tag is set on its merge commit, never on a commit main lacks.
RELEASE_BRANCH="release/${TAG}"
REMOTE="${UPSTREAM%%/*}"
REMOTE="${REMOTE:-origin}"

if (( ! DRY_RUN )); then
    # The commit stages only the files this script writes, but edits waiting
    # in them would ride along unseen.
    if [[ -n "$(git status --porcelain --untracked-files=no)" ]]; then
        echo "The working tree has uncommitted changes; commit or discard them first:"
        git status --short --untracked-files=no | sed 's/^/     /'
        exit 1
    fi
    if git rev-parse --quiet --verify "refs/tags/${TAG}" >/dev/null \
        || git ls-remote --exit-code --tags "${REMOTE}" "refs/tags/${TAG}" >/dev/null 2>&1; then
        echo "${TAG} already exists; a published version is never released again."
        exit 1
    fi
    if git rev-parse --quiet --verify "refs/heads/${RELEASE_BRANCH}" >/dev/null \
        || git ls-remote --exit-code --heads "${REMOTE}" "refs/heads/${RELEASE_BRANCH}" >/dev/null 2>&1; then
        echo "${RELEASE_BRANCH} already exists. Merge or close its pull request and delete the branch,"
        echo "   or release another version."
        exit 1
    fi
    git switch -q -c "${RELEASE_BRANCH}"
fi

if [[ -n "${MANUAL_CHANGELOG}" ]]; then
    RAW_LOG=""
fi

# Same reason as above: everything reaches Python through the environment.
NOTES_SOURCE=$(
VERSION="${VERSION}" \
RAW_LOG="${RAW_LOG}" \
MANUAL_CHANGELOG="${MANUAL_CHANGELOG}" \
RELEASE_DATE="$(date -u +%Y-%m-%d)" \
NOTES_FILE="${NOTES_FILE}" \
CHANGELOG_FILE="${CHANGELOG_FILE}" \
DRY_RUN="${DRY_RUN}" \
python3 - <<'PY'
import os
import re
import sys

version = os.environ['VERSION']
release_date = os.environ['RELEASE_DATE']
manual = os.environ.get('MANUAL_CHANGELOG', '')
raw = os.environ.get('RAW_LOG', '')
notes_path = os.environ['NOTES_FILE']
changelog_path = os.environ['CHANGELOG_FILE']
dry_run = os.environ.get('DRY_RUN') == '1'

BREAKING, FEATURES, FIXES, OTHER = (
    'Breaking changes', 'Features', 'Bug fixes', 'Other')
HEADER = re.compile(
    r'^(?P<type>[A-Za-z]+)(?:\((?P<scope>[^)]*)\))?(?P<bang>!)?:\s+(?P<desc>.+)$')
TRAILER = re.compile(r'^BREAKING[ -]CHANGE:\s*(?P<desc>.*)$')

# GitHub skips every workflow of a push whose head commit message carries one of these, including the tag
# the release is built from, so a quoted commit subject must never carry one into the notes.
SKIP_CI = re.compile(
    r'\s*(\[(skip|no)[ -]ci\]|\[(ci|actions)[ -]skip\]'
    r'|\[skip[ -]actions\]|\*\*\*NO_CI\*\*\*)',
    re.IGNORECASE,
)
UNRELEASED = re.compile(r'^## \[Unreleased\]\s*$', re.IGNORECASE)

KEEP_A_CHANGELOG_HEADER = """# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the versioning [Semantic Versioning](https://semver.org/).
"""


def collect(log):
    sections = {BREAKING: [], FEATURES: [], FIXES: [], OTHER: []}
    for record in log.split('\x1e'):
        if not record.strip():
            continue
        fields = record.strip('\n').split('\x1f')
        sha, subject = fields[0], fields[1] if len(fields) > 1 else ''
        body = fields[2] if len(fields) > 2 else ''
        match = HEADER.match(subject)
        ctype = match.group('type').lower() if match else ''
        if ctype == 'release':
            continue
        if match:
            text = match.group('desc')
            if match.group('scope'):
                text = '%s: %s' % (match.group('scope'), text)
        else:
            # Not a Conventional Commit - kept verbatim rather than dropped.
            text = subject
        text = SKIP_CI.sub('', text).strip()
        trailer = None
        for line in body.splitlines():
            found = TRAILER.match(line.strip())
            if found:
                trailer = found.group('desc').strip() or text
                break
        if trailer:
            trailer = SKIP_CI.sub('', trailer).strip()
        if (match and match.group('bang')) or trailer:
            sections[BREAKING].append((trailer or text, sha[:7]))
        elif ctype == 'feat':
            sections[FEATURES].append((text, sha[:7]))
        elif ctype == 'fix':
            sections[FIXES].append((text, sha[:7]))
        else:
            sections[OTHER].append((text, sha[:7]))
    return sections


def markdown(sections):
    out = []
    for title in (BREAKING, FEATURES, FIXES, OTHER):
        if not sections[title]:
            continue
        out.append('### %s' % title)
        out.append('')
        out += ['- %s (%s)' % item for item in sections[title]]
        out.append('')
    if not out:
        return '- No changes recorded since the previous release.'
    return '\n'.join(out).strip()


def split_changelog(text):
    """Preamble, Unreleased body and older sections; a hand-written Unreleased section becomes the release,
    since it is phrased better than any commit subject."""
    lines = text.split('\n')
    heads = [i for i, line in enumerate(lines) if line.startswith('## ')]
    if not heads:
        return text.strip('\n'), '', ''
    first = heads[0]
    preamble = '\n'.join(lines[:first]).strip('\n')
    if not UNRELEASED.match(lines[first]):
        return preamble, '', '\n'.join(lines[first:]).strip('\n')
    end = heads[1] if len(heads) > 1 else len(lines)
    return (preamble,
            '\n'.join(lines[first + 1:end]).strip('\n'),
            '\n'.join(lines[end:]).strip('\n'))


preamble, unreleased, older = '', '', ''
if os.path.exists(changelog_path):
    with open(changelog_path) as f:
        preamble, unreleased, older = split_changelog(f.read())
preamble = preamble or KEEP_A_CHANGELOG_HEADER.strip('\n')

if manual:
    body, source = manual.strip(), 'given on the command line'
elif unreleased:
    body, source = unreleased, 'taken from the Unreleased section'
else:
    body, source = markdown(collect(raw)), 'generated from the commits'

print(source)

if dry_run:
    print('Release notes that would be written to %s:\n' % notes_path,
          file=sys.stderr)
    print('\n'.join('   ' + line for line in body.split('\n')), file=sys.stderr)
    raise SystemExit(0)

with open(notes_path, 'w') as f:
    f.write(body.rstrip('\n') + '\n')

section = '## [%s] - %s\n\n%s' % (version, release_date, body.rstrip('\n'))
document = '\n\n'.join(part for part in
                       (preamble, '## [Unreleased]', section, older) if part)
with open(changelog_path, 'w') as f:
    f.write(document.rstrip('\n') + '\n')
PY
)

if (( DRY_RUN )); then
    echo "Dry run: Exciter ${TAG}"
else
    echo "Releasing Exciter ${TAG}"
fi
echo "   Version:   ${VERSION} ($( (( INFERRED )) && echo "inferred from the commit history" || echo "given on the command line"))"
echo "   Notes:     ${NOTES_FILE} (${NOTES_SOURCE})"
echo "   Branch:    ${RELEASE_BRANCH}, with a pull request into ${DEFAULT_BRANCH}"
echo "   Files:     $("./${VERSION_FILES}" files | paste -sd' ') ${CHANGELOG_FILE}"
if (( DRY_RUN )); then
    echo ""
    echo "   Nothing was written: no version strings, no ${CHANGELOG_FILE}, no branch, no commit, no tag."
    exit 0
fi
echo "   ${CHANGELOG_FILE}"
echo "   ${NOTES_FILE}"
echo ""

echo "Updating the version files..."
"./${VERSION_FILES}" set "${VERSION}"
"./${VERSION_FILES}" show | sed 's/^/   /; s/\t/: /'

# Nothing is built here: release.yml builds the images from the tag, so what
# is published is exactly what CI built, and releasing needs no local toolchain.

echo ""
echo "Committing on ${RELEASE_BRANCH}..."
# Only the files this release rewrote: "git add -A" would sweep whatever else
# is in the working tree into the release commit.
mapfile -t CARRIERS < <("./${VERSION_FILES}" files)
git add "${CARRIERS[@]}" "${CHANGELOG_FILE}"
# The notes are multi-line, so they belong in the body; the subject has to
# stay one short line.
git commit -q -m "release: ${TAG}" -m "$(cat "${NOTES_FILE}")"

echo ""
echo "Pushing ${RELEASE_BRANCH} to ${REMOTE}..."
git push -q -u "${REMOTE}" "${RELEASE_BRANCH}"

PR_BODY="$(mktemp)"
trap 'rm -f "${PR_BODY}"' EXIT
{
    echo "Releases Exciter ${TAG}. Merging this pull request tags its merge commit"
    echo "${TAG}, and the tag builds and publishes the images and the GitHub"
    echo "release (docs/releasing.md)."
    echo ""
    cat "${NOTES_FILE}"
} > "${PR_BODY}"
PR_COMMAND=(gh pr create --base "${DEFAULT_BRANCH}" --head "${RELEASE_BRANCH}" --title "release: ${TAG}" --body-file "${PR_BODY}")
if command -v gh >/dev/null 2>&1 && PR_URL=$("${PR_COMMAND[@]}"); then
    echo "   ${PR_URL}"
else
    echo "   warning: the pull request could not be opened. Open it by hand, with the notes below as its body:"
    echo "      gh pr create --base ${DEFAULT_BRANCH} --head ${RELEASE_BRANCH} --title \"release: ${TAG}\" --body-file ${NOTES_FILE}"
fi

echo ""
echo "Release notes (${NOTES_FILE}):"
echo ""
sed 's/^/   /' "${NOTES_FILE}"
echo ""
echo "Done! Next steps:"
echo ""
echo "   1. Merge the pull request once CI is green (a merge commit)."
echo "   2. release.yml tags the merge commit ${TAG}, builds and pushes the images,"
echo "      publishes the GitHub release and tells Dynamo."
echo "   3. git switch ${DEFAULT_BRANCH} && git merge --ff-only ${REMOTE}/${DEFAULT_BRANCH}"
echo ""
