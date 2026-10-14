#!/usr/bin/env bash
# The one entry point for the suite: the push hook and CI's jobs run the same layers, so CI tests what a push did.
# Usage: ./run-tests.sh [layer...] | --list, with SHARD=2/4 splitting test-e2e; no layer named runs them all.
set -euo pipefail
cd "$(dirname "$0")"

LAYERS=(shell check-format check-go check-web test-integration test-e2e)
STACK_LAYERS=(test-integration test-e2e)

# Every layer leaves its machine-readable results here and a line with its outcome and duration;
# scripts/test-summary.sh reads them, here at the end and in CI for the run's summary.
REPORTS=reports
LAYER_LOG="$REPORTS/layers.tsv"

if [[ "${1:-}" == --list ]]; then
  printf '%s\n' "${LAYERS[@]}"
  exit 0
fi

selected=("$@")
[[ ${#selected[@]} -gt 0 ]] || selected=("${LAYERS[@]}")
for layer in "${selected[@]}"; do
  [[ " ${LAYERS[*]} " == *" $layer "* ]] \
    || { echo "There is no layer called $layer. Name some of: ${LAYERS[*]}." >&2; exit 2; }
done
wanted() { [[ " ${selected[*]} " == *" $1 "* ]]; }

# Each layer joins the gate in the change that introduces it, through a Makefile target of the same name,
# so this file only decides the order.
has_target() { [[ -f Makefile ]] && make -n "$1" >/dev/null 2>&1; }

# Without make every layer would look absent and the gate would pass having run nothing but the shell tests.
if [[ -f Makefile ]] && ! command -v make >/dev/null && [[ " ${selected[*]} " != " shell " ]]; then
  echo "make is not installed, so no layer but the shell tests could run. Install make (sudo apt install make) and run the gate again." >&2
  exit 2
fi

# Chained rather than relying on set -e, which a call from run_layer turns off.
shell_tests() {
  ./tests/test-release.sh && ./tests/test-ci.sh && ./tests/test-ports.sh && ./tests/test-stack.sh && ./tests/test-suite.sh \
    && ./.claude/hooks/tests/branch-guard-test.sh
}

rm -rf "$REPORTS"
mkdir -p "$REPORTS"

stack_started=""
finish() {
  local rc=$?
  [[ -z "$stack_started" ]] || make stack-down >/dev/null 2>&1 || true
  ./scripts/test-summary.sh "$REPORTS" > "$REPORTS/summary.md" 2>/dev/null || true
  # The hook shows only the last lines of a failed gate, which belong to the failure.
  if (( rc == 0 )); then cat "$REPORTS/summary.md"; else echo "Durations and slowest tests so far: $REPORTS/summary.md"; fi
  exit "$rc"
}
# A gate stopped from outside still takes its stack down.
trap 'exit 143' TERM INT HUP
trap finish EXIT

# A layer that fails still gets its line, so a red run shows where it stopped and how long that took.
run_layer() {
  local name="$1" label="$1" start=$SECONDS status=passed
  shift
  case "$name" in test-e2e | stack-up) label="$name${SHARD:+ $SHARD}" ;; esac
  echo "==> $label"
  "$@" || status=failed
  printf '%s\t%s\t%s\n' "$label" "$status" "$((SECONDS - start))" >> "$LAYER_LOG"
  [[ "$status" == passed ]] || exit 1
}

if wanted shell; then
  run_layer shell shell_tests
fi

for layer in check-format check-go check-web; do
  if wanted "$layer" && has_target "$layer"; then
    run_layer "$layer" make "$layer"
  fi
done

# The stack starts only for a layer that exists, so a gate with no stack layer yet never waits for one.
needs_stack=""
for layer in "${STACK_LAYERS[@]}"; do
  if wanted "$layer" && has_target "$layer"; then needs_stack=1; fi
done

if [[ -n "$needs_stack" ]] && has_target stack-up; then
  # One compose project and port range per checkout, so gates in parallel worktrees never share a database or a port.
  stack_started=1
  run_layer stack-up make stack-up
  for layer in "${STACK_LAYERS[@]}"; do
    if wanted "$layer" && has_target "$layer"; then
      run_layer "$layer" make "$layer"
    fi
  done
fi

echo "All tests passed"
