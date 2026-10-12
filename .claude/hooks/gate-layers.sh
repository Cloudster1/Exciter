#!/usr/bin/env bash
# Which layers of ./run-tests.sh a push needs, from the files it changes; an unknown path runs every layer.
# Usage: gate-layers.sh [quick|full] < changed-files. Quick leaves the stack layers to CI, which blocks the merge.
set -uo pipefail

mode="${1:-quick}"
[[ "$mode" == quick || "$mode" == full ]] || { echo "gate-layers.sh: the mode is quick or full, not $mode" >&2; exit 2; }

need_go=0 need_web=0 need_integration=0 need_e2e=0 seen=0 other=0
while IFS= read -r path; do
  [[ -n "$path" ]] || continue
  seen=1
  case "$path" in
    *.md | NOTICE | LICENSE | docs/* | CHANGELOG.md | assets/*) ;;
    backend/* | go.work | go.work.sum) need_go=1 need_integration=1 need_e2e=1 ;;
    api/*) need_go=1 need_web=1 need_integration=1 need_e2e=1 ;;
    web/*) need_web=1 need_e2e=1 ;;
    e2e/*) need_e2e=1 ;;
    deploy/* | tests/* | scripts/* | mk/* | .github/* | .claude/* | Makefile | release.sh | VERSION | biome.json | .gitignore) other=1 ;;
    run-tests.sh) other=1 need_go=1 need_web=1 need_integration=1 need_e2e=1 ;;
    *) other=1 need_go=1 need_web=1 need_integration=1 need_e2e=1 ;;
  esac
done

# No files known, such as a push whose base cannot be found, runs everything.
if (( !seen )); then need_go=1 need_web=1 need_integration=1 need_e2e=1; fi

run=(shell check-format)
skipped=()
if (( need_go )); then run+=(check-go); else skipped+=("check-go (no backend or API file changed)"); fi
if (( need_web )); then run+=(check-web); else skipped+=("check-web (no web or API file changed)"); fi
if [[ "$mode" == full ]]; then
  if (( need_integration )); then run+=(test-integration); else skipped+=("test-integration (no backend or API file changed)"); fi
  if (( need_e2e )); then run+=(test-e2e); else skipped+=("test-e2e (no file the browser suite reads changed)"); fi
else
  skipped+=("test-integration and test-e2e (left to CI; EXCITER_GATE=full runs them)")
fi

echo "run: ${run[*]}"
echo "skipped: ${skipped[*]:-none}"
