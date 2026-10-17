---
name: gh
description: GitHub workflow for this repo, read before any code change. Creating issues, issue branches, running tests, opening PRs. Use this whenever you want to change code, commit, push or open a pull request.
---

# GitHub workflow

Repo: `Cloudster1/Exciter`, default branch: `main`

## Rules

- Every change needs an issue and a branch named `<type>/<issue>-<slug>`.
  Types: `feat fix chore docs refactor test perf ci build revert`.
- Never push to `main`. Never merge yourself; that is the user's decision.
- Agree on issues and PRs with the user before creating them.
- Always call `gh` non-interactively: `--title` / `--body-file`, never open the editor.
- `.claude/hooks/branch-guard.sh` enforces this; a violation is blocked, not commented on.
- Issues carry a milestone (`M0 Foundation`, `M1 Requirements core`, `M2 Armature integration`,
  `M3 Review and control`, `M4 Stator integration`, `M5 Traceability`, `M6 Document view`, `M7 MCP integration`,
  `M8 Review sessions and control` or `Backlog`), an `area/*` label, a `type/*` label
  and `mvp` or `backlog`. `blocked` marks an issue waiting on work outside this repository.
- Issue bodies follow the feature template: User story, Acceptance criteria, Tests, Implementation,
  Depends on.

## Language

Everything in the repository is written in English: issues, pull requests, commit messages, code,
comments and documentation. User-facing strings live in `web/src/i18n`.

No other requirements-management product is named anywhere: not in code, docs, issues, PRs or
commit messages. Features are described on their own terms.

## Armature and Stator

Changes Exciter needs in Armature or Stator go through issues and pull requests in
`Cloudster1/Armature` and `praetorianer777/Stator`. Link them from the Exciter issue and label the
Exciter issue `blocked` until they ship.

## Recipe

```bash
gh auth status                       # preflight

gh issue list --limit 20             # does the issue already exist?
gh issue create --title "..." --body-file /tmp/body.md --label area/backend --label type/feature --label mvp --milestone "M1 Requirements core"

gh issue develop 42 --name feat/42-suspect-links --base main --checkout

# ... work ...

./run-tests.sh                       # everything; the hook runs the quick subset before every push (EXCITER_GATE=full for all)
git push -u origin HEAD

gh pr create --title "..." --body-file /tmp/pr.md   # body contains "Closes #42"
gh pr checks --watch
gh run view --log-failed             # when CI is red
```

## Releases

`release.sh` is the user's job. From an agent session, read only:
`gh release list`, `gh release view v0.1.0`.

It opens a pull request from `release/vX.Y.Z`, the one branch name without an
issue that the guard accepts; merging it makes `release.yml` tag the merge
commit and publish. Never create a tag or a `release/` branch by hand. See
`docs/releasing.md`.

## Pitfalls

- `gh issue develop` creates the branch on the remote and checks it out; no manual
  `git switch -c` needed.
- Branch slugs are lowercase ASCII: `feat/42-suspect-links`, not `feat/42-Suspect-Links`.
- Commit messages are Conventional Commits, lowercase, imperative, subject at most 72 characters, and
  describe the **effect** rather than the files touched.
- A push with `run_in_background` spends the whole gate before it is reported as started, then
  finishes in seconds; that is the gate having passed, not skipped. Every guard decision and gate
  duration is in `$(git rev-parse --git-common-dir)/branch-guard.log`.
- Parallel work happens in worktrees under `.claude/worktrees/`; each gets its own compose
  project and ports from `run-tests.sh`, so gates never collide.
