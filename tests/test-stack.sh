#!/usr/bin/env bash
# Tests the stack's definition without starting it: compose accepts the file, nothing is published beyond the
# loopback, and the realm gives Exciter, Armature and Stator each a client with a groups claim, and PKCE where the
# product sends it.
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
  # The bootstrap names the realm and client the stack's Keycloak imports, so a renamed realm or secret fails here.
  bootstrap="$(jq -r '.services.seed.environment | [.EXCITER_BOOTSTRAP_OIDC_ISSUER, .EXCITER_BOOTSTRAP_OIDC_CLIENT_ID, .EXCITER_BOOTSTRAP_OIDC_CLIENT_SECRET] | @tsv' <<< "$config")"
  IFS=$'\t' read -r issuer client secret <<< "$bootstrap"
  realm_secret="$(jq -r --arg c "$client" '.clients[] | select(.clientId == $c) | .secret' "$REALM")"
  [[ "${issuer##*/realms/}" == "$(jq -r .realm "$REALM")" && -n "$realm_secret" && "$secret" == "$realm_secret" ]] \
    && pass "the bootstrap signs in through the realm's own client and secret" \
    || fail "the bootstrap names issuer '$issuer', client '$client', which the realm does not hold with that secret"
  oneshot="$(jq -r '.services | to_entries[] | select(.value.restart == "no") | .key' <<< "$config" | sort | tr '\n' ' ')"
  waited="$(jq -r '[.services[] | (.depends_on // {}) | to_entries[] | select(.value.condition == "service_completed_successfully") | .key] | unique | join(" ")' <<< "$config")"
  [[ "${oneshot% }" == "$waited" ]] && pass "every one-shot service is waited for, so up --wait accepts it" \
    || fail "one-shot services ($oneshot) and the ones something waits for ($waited) differ"
else
  fail "compose refuses the file: $config"
fi

echo "== keycloak realm"
# Armature's sign-in sends no PKCE challenge yet, so its client asks for none; docs/suite.md says so.
for client in exciter armature stator; do
  want=$'S256\t1'
  [[ "$client" == armature ]] && want=$'\t1'
  got="$(jq -r --arg c "$client" '.clients[] | select(.clientId == $c)
    | [.attributes["pkce.code.challenge.method"] // "", ([.protocolMappers[] | select(.config["claim.name"] == "groups")] | length)] | @tsv' "$REALM")"
  [[ "$got" == "$want" ]] && pass "client $client carries the groups claim, with PKCE where its product sends it" \
    || fail "client $client is missing, or lacks the groups claim or the PKCE its product sends: '$got'"
done
[[ "$(jq -r .realm "$REALM")" == dynamo ]] && pass "the realm is dynamo" || fail "the realm is not dynamo"

# A return address whose wildcard follows the host at once, http://localhost*, also matches
# http://localhost.evil.example/, so a sign-in could be sent there with its code. Prints each such address.
wildcard_hosts() { # realm-file
  jq -r '.clients[] | .clientId as $c
    | ((.redirectUris // []) + ((.attributes["post.logout.redirect.uris"] // "") | split("##")))[]
    | select(. != "" and . != "+")
    | select(test("^\\*$") or test("^[a-z][a-z0-9+.-]*://[^/:?#]*\\*"))
    | "\($c) \(.)"' "$1"
}
loose="$(wildcard_hosts "$REALM")"
[[ -z "$loose" ]] && pass "no return address of any client ends in a wildcard straight after its host" \
  || fail "these return addresses match any host that starts the same: $loose"
for client in exciter armature stator; do
  n="$(jq -r --arg c "$client" '[.clients[] | select(.clientId == $c) | .redirectUris[]] | length' "$REALM")"
  [[ "$n" -gt 0 ]] && pass "client $client names its return addresses" || fail "client $client names no return address, so the check above saw none"
done
probe="$(mktemp)"
jq '.clients[0].redirectUris += ["http://localhost*"] | .clients[1].attributes["post.logout.redirect.uris"] = "http://127.0.0.1*##+"' "$REALM" > "$probe"
[[ "$(wildcard_hosts "$probe" | wc -l)" -eq 2 ]] && pass "the check names a wildcard straight after the host, in a return or a logout address" \
  || fail "the check missed a planted wildcard: $(wildcard_hosts "$probe")"
rm -f "$probe"
unknown="$(jq -r '[.groups[].path] as $g | .users[] | .groups[] | select(. as $x | $g | index($x) | not)' "$REALM")"
[[ -z "$unknown" ]] && pass "every user's groups exist" || fail "users name groups that do not exist: $unknown"

if (( FAILED )); then echo "FAIL: stack tests"; exit 1; fi
echo "PASS: stack tests"
