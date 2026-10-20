#!/usr/bin/env bash
# Takes the suite demo's snapshot (#270): the issues of Armature, Exciter and Stator that carry a user story, as
# backend/internal/demo/snapshot/<tool>.json. Run on the host by make demo-snapshot, with gh signed in.
set -euo pipefail

OUT=${1:-$(cd "$(dirname "$0")/.." && pwd)/backend/internal/demo/snapshot}
TAKEN=$(date -u +%Y-%m-%d)
LIMIT=5000

for tool in gh jq; do
  if ! command -v "$tool" >/dev/null; then
    echo "$tool is not installed. Install it and run make demo-snapshot again." >&2
    exit 1
  fi
done

mkdir -p "$OUT"
# The key and the name are what the three tools call each project; the key also prefixes the issue keys in Armature.
while read -r file repo key name; do
  commit=$(gh api "repos/$repo/commits/HEAD" --jq .sha)
  # gh issue list leaves pull requests out; the order is by number so a snapshot taken again shows only what changed.
  gh issue list -R "$repo" --state all --limit "$LIMIT" --json number,title,state,closedAt,milestone,url,body |
    jq --arg repo "$repo" --arg key "$key" --arg name "$name" --arg commit "$commit" --arg taken "$TAKEN" '{
      repo: $repo, key: $key, name: $name, commit: $commit, taken: $taken,
      issues: [.[] | select(.body | test("^##[ \t]*User story"; "")) | {
        number, title, closed: (.state == "CLOSED"), closedAt, milestone: (.milestone.title // ""), url,
        # Attribution lines an assistant added say nothing about the requirement.
        body: (.body | gsub("\r"; "") | gsub("\n*[^\n]*Generated with \\[Claude Code\\][^\n]*"; ""))
      }] | sort_by(.number)
    }' >"$OUT/$file.json.tmp"
  mv "$OUT/$file.json.tmp" "$OUT/$file.json"
  echo "$repo: $(jq '.issues | length' "$OUT/$file.json") issues at ${commit:0:12}"
done <<'EOF'
armature Cloudster1/Armature ARM Armature
exciter Cloudster1/Exciter EXC Exciter
stator praetorianer777/Stator STA Stator
EOF
