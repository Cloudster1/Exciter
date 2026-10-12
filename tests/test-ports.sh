#!/usr/bin/env bash
# Tests for scripts/stack-ports.sh: every checkout's twenty ports stay inside 10000 to 19999, clear of Stator's
# 20000 to 29999 and the kernel's ephemeral range from 32768, and never cover a port another service holds.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
PORTS="$SRC/scripts/stack-ports.sh"

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }

echo "== stack ports"

width="$("$PORTS" --width)"
mapfile -t denied < <("$PORTS" --denied)
mapfile -t bases < <("$PORTS" --all)

bad=""
for base in "${bases[@]}"; do
  last=$((base + width - 1))
  (( base >= 10000 && last <= 19999 )) || bad+=" $base"
  for port in "${denied[@]}"; do
    (( port < base || port > last )) || bad+=" $base(holds $port)"
  done
done
[[ -z "$bad" ]] && pass "all ${#bases[@]} slots lie in 10000 to 19999 and hold no denied port" \
  || fail "these slots break the rule:$bad"

free="$(printf '%s\n' "${bases[@]}" | sort -u | wc -l)"
(( free >= ${#bases[@]} - ${#denied[@]} )) && pass "$free distinct slots; only the denied ones are given up" \
  || fail "only $free distinct slots for ${#bases[@]} hashes"

for port in "${denied[@]}"; do
  slot=$(((port - 10000) / width))
  moved="$("$PORTS" --slot "$slot")"
  [[ "$moved" != $((slot * width + 10000)) ]] && pass "the slot holding $port gives way to $moved" \
    || fail "the slot holding $port is still handed out"
done

a="$("$PORTS" /home/someone/exciter)"
b="$("$PORTS" /home/someone/exciter)"
c="$("$PORTS" /home/someone/exciter/.claude/worktrees/agent-x)"
[[ "$a" == "$b" ]] && pass "a checkout keeps its ports" || fail "the same path gave $a and $b"
[[ "$a" != "$c" ]] && pass "a worktree gets ports of its own" || fail "a checkout and its worktree share $a"

"$PORTS" > /dev/null 2>&1 && fail "a call without a path was accepted" || pass "a call without a path is refused"

grep -q 'scripts/stack-ports.sh' "$SRC/mk/stack.mk" && pass "mk/stack.mk takes its ports from the script" \
  || fail "mk/stack.mk does not use scripts/stack-ports.sh"

if (( FAILED )); then echo "FAIL: port tests"; exit 1; fi
echo "PASS: port tests"
