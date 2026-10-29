#!/usr/bin/env bash
# Which layers of ./run-tests.sh passed on which committed tree, so a push need not run them again.
# Usage, in the checkout: gate-passes.sh tree | record <tree> <layer>... | covers <layer>...
set -uo pipefail

LAYERS=" shell check-format check-go check-web test-integration test-e2e "

# Per checkout, so a worktree keeps its own record.
file="$(git rev-parse --git-path exciter-gate-passed 2>/dev/null)" || exit 1

head_tree() { git rev-parse -q --verify 'HEAD^{tree}' 2>/dev/null; }

# The layers ran over the working tree, so only a clean one says anything about a commit.
clean_tree() {
  [[ -z "$(git status --porcelain 2>/dev/null)" ]] || return 1
  head_tree
}

case "${1:-}" in
tree)
  clean_tree || true
  ;;
record)
  tree="${2:-}"
  shift 2 || exit 0
  [[ -n "$tree" && "$(clean_tree)" == "$tree" ]] || exit 0
  {
    grep -s "^$tree"$'\t' "$file" || true
    for layer; do
      # A shard of the browser suite is not the suite.
      if [[ "$LAYERS" == *" $layer "* ]]; then printf '%s\t%s\n' "$tree" "$layer"; fi
    done
  } | sort -u > "$file.tmp" && mv "$file.tmp" "$file"
  ;;
covers)
  shift
  tree="$(head_tree)" || exit 1
  (( $# )) || exit 1
  for layer; do
    grep -qxF "$tree"$'\t'"$layer" "$file" 2>/dev/null || exit 1
  done
  ;;
*)
  echo "gate-passes.sh: say tree, record <tree> <layer>... or covers <layer>..., not '${1:-}'" >&2
  exit 2
  ;;
esac
