#!/usr/bin/env bash
# Tests that CI's jobs between them run every layer run-tests.sh knows and every browser shard once,
# so a layer dropped from the matrix cannot stop gating pull requests without anything turning red.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
CI="$SRC/.github/workflows/ci.yml"

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }

echo "== ci.yml runs the whole gate"

ci_layers=$(sed -n 's/^ *layers: *//p' "$CI" | tr ' ' '\n' | sort)
for layer in $("$SRC/run-tests.sh" --list); do
  if grep -qx "$layer" <<< "$ci_layers"; then
    pass "a job runs $layer"
  else
    fail "no job in ci.yml runs $layer; add it to a job's layers"
  fi
done

for layer in $(sort -u <<< "$ci_layers"); do
  "$SRC/run-tests.sh" --list | grep -qx "$layer" \
    || fail "ci.yml names $layer, which run-tests.sh does not know"
done

dupes=$(uniq -d <<< "$(grep -vx test-e2e <<< "$ci_layers")")
[[ -z "$dupes" ]] && pass "no layer but the browser suite runs twice" \
  || fail "these layers run in more than one job: $dupes"

shards=$(sed -n 's/^ *shard: *//p' "$CI")
e2e_jobs=$(grep -cx test-e2e <<< "$ci_layers")
totals=$(cut -d/ -f2 <<< "$shards" | sort -u)
if [[ $(wc -l <<< "$totals") -ne 1 ]]; then
  fail "the browser jobs disagree on how many parts there are: $(paste -sd' ' <<< "$totals")"
elif [[ "$e2e_jobs" -ne "$totals" || $(wc -l <<< "$shards") -ne "$totals" ]]; then
  fail "$e2e_jobs browser jobs and $(wc -l <<< "$shards") shards for $totals parts; each part needs one job"
else
  missing=""
  for i in $(seq "$totals"); do grep -qx "$i/$totals" <<< "$shards" || missing+=" $i/$totals"; done
  [[ -z "$missing" ]] && pass "the $totals browser jobs run each part once" \
    || fail "no browser job runs part$missing"
fi

echo "== asking whether a layer exists runs none of it"

# run-tests.sh asks make -n, before the stack is up; make runs a recipe line that holds $(MAKE) even then, and a
# layer whose line failed so was skipped as absent while the gate passed (#440).
stub="$(mktemp -d)"
printf '#!/bin/sh\necho "$*" >> "%s/ran"\nexit 1\n' "$stub" > "$stub/docker"
chmod +x "$stub/docker"
for layer in $("$SRC/run-tests.sh" --list) stack-up; do
  [[ "$layer" == shell ]] && continue
  rm -f "$stub/ran"
  if ! PATH="$stub:$PATH" make -n -C "$SRC" "$layer" > /dev/null 2>&1; then
    fail "make -n $layer fails, so run-tests.sh would skip $layer as a layer that does not exist"
  elif [[ -s "$stub/ran" ]]; then
    fail "make -n $layer runs docker $(head -c 80 "$stub/ran"); keep \$(MAKE) off a recipe line that runs anything else"
  else
    pass "make -n $layer runs nothing"
  fi
done
rm -rf "$stub"

echo "== chart-install.yml runs for what an install is built from"

INSTALL="$SRC/.github/workflows/chart-install.yml"
# The paths a trigger lists, one per line.
trigger_paths() { # pull_request|push
  awk -v want="$1" '
    /^  [a-z_]+:/ { in_trigger = ($1 == want ":"); in_paths = 0; next }
    in_trigger && /^    paths:/ { in_paths = 1; next }
    in_trigger && in_paths && /^      - / { sub(/^      - /, ""); print; next }
    in_trigger && in_paths && !/^      / { in_paths = 0 }' "$INSTALL"
}
# Whether a workflow path pattern takes in a file: ** is any path, * is anything within one segment.
covered() { # file patterns...
  local file="$1" pattern regex
  shift
  for pattern in "$@"; do
    regex="$(sed -e 's/[.+?^${}()|]/\\&/g' -e 's/\*\*/\x01/g' -e 's/\*/[^\/]*/g' -e 's/\x01/.*/g' <<< "$pattern")"
    [[ "$file" =~ ^${regex}$ ]] && return 0
  done
  return 1
}
# A file in each source the images copy. backend/ as a whole would run the install for every change to a feature,
# so it stands for the entry points the chart runs, the configuration they load and the schema.
install_inputs() {
  local dockerfile source bin
  for dockerfile in backend web render; do
    sed -n 's/^COPY \([^-][^ ]*\( [^ ]*\)*\) [^ ]*$/\1/p' "$SRC/deploy/Dockerfile.$dockerfile" | tr ' ' '\n'
  done | sort -u | while read -r source; do
    if [[ "$source" == backend/ ]]; then
      for bin in $(grep -rhoE '"/app/[a-z-]+"' "$SRC/deploy/charts/exciter/templates" "$SRC/deploy/Dockerfile.backend" | tr -d '"' | sed 's|/app/||' | sort -u); do
        echo "backend/cmd/$bin/main.go"
      done
      echo backend/internal/config/config.go
      echo backend/migrations/00001_foundation.sql
    elif [[ "$source" == */ ]]; then
      echo "${source}any/file"
    else
      echo "$source"
    fi
  done
}

inputs="$(install_inputs)"
[[ "$(wc -l <<< "$inputs")" -ge 10 ]] && pass "the Dockerfiles name $(wc -l <<< "$inputs") inputs of an install" \
  || fail "only $(wc -l <<< "$inputs") inputs read from the Dockerfiles, so the check below sees too little: $inputs"
for trigger in pull_request push; do
  mapfile -t patterns < <(trigger_paths "$trigger")
  missing=""
  while read -r input; do
    covered "$input" "${patterns[@]}" || missing+=" $input"
  done <<< "$inputs"
  for own in scripts/chart-install-test.sh scripts/chart-install-values.yaml scripts/kind/Dockerfile mk/kind.mk .github/workflows/chart-install.yml; do
    covered "$own" "${patterns[@]}" || missing+=" $own"
  done
  [[ -z "$missing" ]] && pass "its $trigger trigger covers every input of an install and the test itself" \
    || fail "its $trigger trigger leaves out:$missing"
done
covered backend/internal/requirement/service.go $(trigger_paths pull_request) \
  && fail "a feature package alone runs the install, which backend/ as a whole would" \
  || pass "a feature package alone does not run the install"

if [[ $FAILED -eq 0 ]]; then echo "ci.yml tests passed"; else echo "ci.yml tests failed"; fi
exit $FAILED
