#!/usr/bin/env bash
# Tests the suite's definition without starting it (#31): Exciter's override merges, its ports stay apart from every
# checkout's own stack, and the names the overrides give each service are the ones suite-seed calls.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
SUITE_MK="$SRC/mk/suite.mk"
SEED="$SRC/backend/cmd/suite-seed/main.go"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }

# Ports outside both ranges a checkout's own stack takes, and below the kernel's ephemeral ports.
OWN_STACKS_FLOOR=10000
OWN_STACKS_CEILING=29999
EPHEMERAL_FLOOR=32768

echo "== suite"

if config="$(ARMATURE_STUB_PORT=30018 SUITE_ARMATURE_WEB_PORT=30020 \
  docker compose -f "$SRC/deploy/docker-compose.yml" -f "$SRC/deploy/suite/exciter.yml" config --format json 2>"$TMP/err")"; then
  pass "compose merges Exciter's suite override"
  [[ "$(jq -r '.services.keycloak.networks.suite.aliases[0]' <<< "$config")" == suite-keycloak ]] \
    && pass "the suite's Keycloak is Exciter's" || fail "Exciter's Keycloak is not suite-keycloak on the suite's network"
else
  fail "compose refuses Exciter's suite override: $(cat "$TMP/err")"
fi

ports="$(grep -oE '(=|:= )3[0-9]{4}\b' "$SUITE_MK" | grep -oE '[0-9]+' | sort -n | uniq)"
dupes="$(grep -oE '[A-Z_]+=3[0-9]{4}' "$SUITE_MK" | cut -d= -f2 | sort | uniq -d)"
bad=""
for p in $ports; do
  (( (p >= OWN_STACKS_FLOOR && p <= OWN_STACKS_CEILING) || p >= EPHEMERAL_FLOOR )) && bad+=" $p"
done
[[ -z "$bad" ]] && pass "every suite port is clear of the checkouts' own stacks and the ephemeral ports" || fail "ports in the way:$bad"
[[ -z "$dupes" ]] && pass "no two services of the suite share a port" || fail "ports given twice: $dupes"

for name in suite-exciter-api suite-armature-api suite-stator-api; do
  if grep -q "$name" "$SEED" && grep -rq "aliases: \[$name\]" "$SRC/deploy/suite"; then
    pass "suite-seed reaches $name, which an override names"
  else
    fail "$name is not both called by suite-seed and named by an override"
  fi
done

# A web on the suite's network would reach any product's api by the name "api", as compose names every service.
if grep -A4 '^  web:' "$SRC"/deploy/suite/*.yml | grep -q 'suite:'; then
  fail "a web joins the suite's network and may proxy to another product's api"
else
  pass "no product's web joins the suite's network"
fi

if (( FAILED )); then echo "FAIL: suite tests"; exit 1; fi
echo "PASS: suite tests"
