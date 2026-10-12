#!/usr/bin/env bash
# Tests the stack's definition without starting it: compose accepts the file, nothing is published beyond the
# loopback, and the realm gives Exciter, Armature and Stator each a client with PKCE and a groups claim.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE="$SRC/deploy/docker-compose.yml"
REALM="$SRC/deploy/keycloak/realm.json"

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }

echo "== compose stack"

if config="$(docker compose -f "$COMPOSE" config --format json 2>&1)"; then
  pass "compose accepts deploy/docker-compose.yml"
  open="$(jq -r '.services | to_entries[] | .key as $s | (.value.ports // [])[] | select(.host_ip != "127.0.0.1") | "\($s):\(.published)"' <<< "$config")"
  [[ -z "$open" ]] && pass "every published port is bound to 127.0.0.1" || fail "published beyond the loopback: $open"
  missing="$(jq -r '.services | to_entries[] | select(.value.healthcheck == null and .key != "mailpit" and .value.restart != "no") | .key' <<< "$config")"
  [[ -z "$missing" ]] && pass "every long-running service but mailpit, whose image has one, declares a health check" \
    || fail "no health check: $missing"
  oneshot="$(jq -r '.services | to_entries[] | select(.value.restart == "no") | .key' <<< "$config" | sort | tr '\n' ' ')"
  waited="$(jq -r '[.services[] | (.depends_on // {}) | to_entries[] | select(.value.condition == "service_completed_successfully") | .key] | unique | join(" ")' <<< "$config")"
  [[ "${oneshot% }" == "$waited" ]] && pass "every one-shot service is waited for, so up --wait accepts it" \
    || fail "one-shot services ($oneshot) and the ones something waits for ($waited) differ"
else
  fail "compose refuses the file: $config"
fi

echo "== keycloak realm"
for client in exciter armature stator; do
  got="$(jq -r --arg c "$client" '.clients[] | select(.clientId == $c)
    | [.attributes["pkce.code.challenge.method"], ([.protocolMappers[] | select(.config["claim.name"] == "groups")] | length)] | @tsv' "$REALM")"
  [[ "$got" == $'S256\t1' ]] && pass "client $client uses PKCE and carries the groups claim" \
    || fail "client $client is missing or lacks PKCE or the groups claim: '$got'"
done
[[ "$(jq -r .realm "$REALM")" == suite-dev ]] && pass "the realm is suite-dev" || fail "the realm is not suite-dev"
unknown="$(jq -r '[.groups[].path] as $g | .users[] | .groups[] | select(. as $x | $g | index($x) | not)' "$REALM")"
[[ -z "$unknown" ]] && pass "every user's groups exist" || fail "users name groups that do not exist: $unknown"

if (( FAILED )); then echo "FAIL: stack tests"; exit 1; fi
echo "PASS: stack tests"
