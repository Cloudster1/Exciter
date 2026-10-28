#!/usr/bin/env bash
# Runs the exciter-ci image through mk/evidence.mk against this checkout's running stack (#323): results and tags
# land on their requirement, and the trace check exits 0 passing, 1 failing and 2 when Exciter cannot be asked.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
STACK_ENV="$SRC/.cache/stack.env"

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }

[[ -f "$STACK_ENV" ]] || { echo "The stack for this checkout is not running. Start it with make stack-up, then run this again." >&2; exit 2; }
command -v curl >/dev/null || { echo "curl is not installed, so the stack cannot be set up for exciter-ci. Install curl and run this again." >&2; exit 2; }
WEB_URL="$(sed -n 's/^EXCITER_WEB_URL=//p' "$STACK_ENV")"
API="$WEB_URL/api/v1"
# The installation's owner, as the seed makes it; README.md names the same account.
OWNER='{"email":"admin@exciter.test","password":"exciter admin password"}'
# How long the worker may take to land a run, or a replica to show it, and how often to look.
LAND_TRIES=60
LAND_PAUSE=0.5

KEY="CI$(LC_ALL=C tr -dc 'A-Z0-9' < /dev/urandom | head -c 6)"
WORK="tests/.exciter-ci-$KEY"
COMMIT="$(git -C "$SRC" rev-parse HEAD)"
TMP="$(mktemp -d)"
JAR="$TMP/cookies"
TOKEN_ID=""

# api calls the API as the owner, printing the body; the status lands in $TMP/status.
api() {
  local method="$1" path="$2" body="${3:-}"
  curl -s -o "$TMP/body" -w '%{http_code}' -b "$JAR" -c "$JAR" -H "Origin: $WEB_URL" -H 'Content-Type: application/json' \
    -X "$method" ${body:+--data "$body"} "$API$path" > "$TMP/status"
  cat "$TMP/body"
}
status() { cat "$TMP/status"; }

# settled reads a GET through filter until it gives want or LAND_TRIES runs out, printing the last reading: the
# owner's session may reach a replica that has not yet replayed what the evidence token wrote.
settled() {
  local path="$1" filter="$2" want="$3" got=""
  for _ in $(seq "$LAND_TRIES"); do
    got="$(api GET "$path" | jq -r "$filter")"
    [[ "$got" == "$want" ]] && break
    sleep "$LAND_PAUSE"
  done
  echo "$got"
}

cleanup() {
  rm -rf "${SRC:?}/$WORK"
  if [[ -n "$TOKEN_ID" ]]; then api DELETE "/tokens/$TOKEN_ID" >/dev/null; fi
  api PUT "/projects/$KEY/archive" >/dev/null
  api DELETE "/projects/$KEY" >/dev/null
  rm -rf "$TMP"
}
trap cleanup EXIT

# ci runs a target of mk/evidence.mk as a pipeline would, answering the exit code exciter-ci gave make.
ci() {
  local token="$1"
  shift
  EXCITER_TOKEN="$token" EXCITER_PROJECT="$KEY" make -s -C "$SRC" --no-print-directory "$@" > "$TMP/ci.out" 2>&1
  local rc=$?
  if (( rc == 0 )); then echo 0; return; fi
  # make exits 2 whatever its recipe did, and names the recipe's own code.
  sed -n 's/.*\] Error \([0-9]*\)$/\1/p' "$TMP/ci.out" | tail -1
}

echo "== exciter-ci against the stack"

signed_in=""
for _ in $(seq "$LAND_TRIES"); do
  api POST /auth/login "$OWNER" >/dev/null
  [[ "$(status)" == 200 ]] && { signed_in=1; break; }
  sleep "$LAND_PAUSE"
done
[[ -n "$signed_in" ]] || { echo "The owner could not sign in to the stack at $WEB_URL: $(cat "$TMP/body")" >&2; exit 2; }

api POST /projects "{\"key\":\"$KEY\",\"name\":\"exciter-ci\"}" >/dev/null
api POST "/projects/$KEY/requirements" '{"title":"Brakes engage within 100 ms"}' >/dev/null
api POST "/projects/$KEY/requirements" '{"title":"Brake lights follow the pedal"}' >/dev/null
made="$(api POST /tokens "{\"name\":\"exciter-ci $KEY\",\"scopes\":[\"evidence\"]}")"
TOKEN_ID="$(jq -r '.token.id // empty' <<< "$made")"
SECRET="$(jq -r '.token.secret // empty' <<< "$made")"
[[ -n "$SECRET" ]] || { echo "The stack gave no evidence token: $made" >&2; exit 2; }

# The reports and sources are paths in the checkout, which the ci image reads; git lists them, being untracked.
mkdir -p "$SRC/$WORK/reports" "$SRC/$WORK/src"
cat > "$SRC/$WORK/reports/junit.xml" <<EOF
<?xml version="1.0"?>
<testsuites><testsuite name="brakes">
<testcase classname="brakes" name="engage [verifies $KEY-R1]"/>
<testcase classname="lights" name="follow the pedal [verifies $KEY-R2]"><failure message="the lights stayed off"/></testcase>
</testsuite></testsuites>
EOF
printf '// [implements %s-R1]\npackage brakes\n' "$KEY" > "$SRC/$WORK/src/brakes.go"
chmod -R a+rX "$SRC/$WORK"

if make -s -C "$SRC" --no-print-directory ci-image > "$TMP/image.out" 2>&1; then
  pass "make ci-image builds the image a pipeline pulls"
else
  fail "make ci-image does not build: $(tail -5 "$TMP/image.out")"
fi

code="$(ci "$SECRET" test-results REPORTS_GLOB="$WORK/reports/*.xml" COMMIT="$COMMIT" BRANCH=main REPOSITORY=exciter/local)"
if [[ "$code" == 0 ]] && grep -q "2 test cases read, 2 name a requirement" "$TMP/ci.out"; then
  pass "make test-results uploads the report, waits for it to land and exits 0"
else
  fail "make test-results exits $code: $(cat "$TMP/ci.out")"
fi
outcome="$(settled "/requirements/$KEY-R1/test-results" '[.results[] | "\(.branch) \(.outcome)"] | join(",")' "main passed")"
[[ "$outcome" == "main passed" ]] && pass "the run shows on the requirement it verifies" || fail "$KEY-R1's results read '$outcome'"

code="$(ci "$SECRET" code-scan DIR="$WORK/src" COMMIT="$COMMIT" BRANCH=main REPOSITORY=exciter/local)"
[[ "$code" == 0 ]] && pass "make code-scan uploads the tags and exits 0" || fail "make code-scan exits $code: $(cat "$TMP/ci.out")"
link="$(settled "/requirements/$KEY-R1/code-links" '[.links[] | "\(.path | split("/") | last):\(.line) \(.relation)"] | join(",")' "brakes.go:1 implements")"
[[ "$link" == "brakes.go:1 implements" ]] && pass "the tag shows on the requirement it implements" || fail "$KEY-R1's code reads '$link'"

code="$(ci "$SECRET" trace-check BRANCH=main FAIL=failing QUERY="key = $KEY-R1")"
[[ "$code" == 0 ]] && grep -q "PASS" "$TMP/ci.out" && pass "make trace-check exits 0 on a check that passes" \
  || fail "a passing check exits $code: $(cat "$TMP/ci.out")"
code="$(ci "$SECRET" trace-check BRANCH=main FAIL=failing)"
[[ "$code" == 1 ]] && grep -q "failing $KEY-R2" "$TMP/ci.out" && pass "make trace-check exits 1 on a failing test, naming it" \
  || fail "a failing check exits $code: $(cat "$TMP/ci.out")"
code="$(ci exciter_pat_wrong trace-check BRANCH=main)"
[[ "$code" == 2 ]] && grep -q "Exciter refused" "$TMP/ci.out" && pass "make trace-check exits 2 on a wrong token" \
  || fail "a wrong token exits $code: $(cat "$TMP/ci.out")"

if (( FAILED )); then echo "FAIL: exciter-ci against the stack"; exit 1; fi
echo "PASS: exciter-ci against the stack"
