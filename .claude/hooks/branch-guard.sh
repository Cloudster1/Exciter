#!/usr/bin/env bash
# PreToolUse on Edit|Write|NotebookEdit|Bash: work happens on <type>/<issue>-<slug> branches, never on main,
# and nothing is pushed unless the layers of ./run-tests.sh its changes can affect pass (see gate-layers.sh).
set -uo pipefail
HOOK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

BRANCH_RE='^(feat|fix|chore|docs|refactor|test|perf|ci|build|revert)/[0-9]+-[a-z0-9][a-z0-9._-]*$'
HINT="Work only on issue branches named <type>/<issue>-<slug> (e.g. fix/42-suspect-links). Find or create the GitHub issue first (see the gh skill), then: gh issue develop <N> --name <type>/<N>-<slug> --base main --checkout"

input="$(cat)"
tool="$(jq -r '.tool_name // empty' <<< "$input")"
project="$(realpath -m "${CLAUDE_PROJECT_DIR:-$(jq -r '.cwd // empty' <<< "$input")}")"
cwd="$(jq -r '.cwd // empty' <<< "$input")"
cwd="${cwd:-$project}"

# Worktrees have their own branch and run-tests.sh, so the checkout is resolved from what is acted on;
# the shared common git dir is what tells a worktree of this repo from an unrelated repo.
git_common_dir() { (cd "$1" 2>/dev/null && realpath -m "$(git rev-parse --git-common-dir 2>/dev/null)"); }
project_git_dir="$(git_common_dir "$project")"

# Every invocation leaves a line saying which hook ran, where, and what it decided, so a push that
# came back without the gate can be traced. Of a command's text only the git subcommands are kept.
LOG="${project_git_dir:-$(git_common_dir "$cwd")}"
LOG="${LOG:+$LOG/branch-guard.log}"
decision=allow reason="" subs=() gate_log="" gate_pid="" gate_repo="" gate_notes=()
log_line() {
  [[ -n "$LOG" && -d "${LOG%/*}" ]] || return 0
  local IFS=,
  printf '%s\thook=%s\tproject_dir=%s\tcwd=%s\ttool=%s\tbg=%s\tagent=%s\tdecision=%s%s\tgit=%s%s\n' \
    "$(date -Is)" "$(realpath -m "${BASH_SOURCE[0]}")" "${CLAUDE_PROJECT_DIR:-}" "$cwd" "$tool" \
    "$(jq -r '.tool_input.run_in_background // false' <<< "$input")" \
    "$(jq -r '.agent_id // .agent_type // "-"' <<< "$input")" \
    "$decision" "${reason:+ ($reason)}" "${subs[*]:--}" "$gate_log" \
    >> "$LOG" 2>/dev/null || true
}
trap log_line EXIT
# A hook killed at its timeout must still leave its line.
trap 'decision=killed; stop_gate; exit 143' TERM INT HUP

checkout_for() {
  local dir="$1" top common
  top="$(git -C "$dir" rev-parse --show-toplevel 2>/dev/null)" || return 1
  common="$(git_common_dir "$dir")"
  [[ "$common" == "$project_git_dir" ]] || return 1
  printf '%s' "$top"
}

decide() {
  decision="$1"
  jq -n --arg d "$1" --arg r "$2" '{hookSpecificOutput: {hookEventName: "PreToolUse", permissionDecision: $d, permissionDecisionReason: $r}}'
  exit 0
}
deny() { decide deny "$1"; }
ask() { decide ask "$1"; }

valid() { [[ "$1" =~ $BRANCH_RE ]]; }

# A rebase stopped on a conflict detaches HEAD, which would refuse the very `git add` and `rebase --continue`
# that get out of it; both rebase backends keep the branch in head-name, which rev-parse finds in worktrees too.
current_branch() {
  local repo="$1" branch head_name d
  branch="$(git -C "$repo" symbolic-ref --quiet --short HEAD 2>/dev/null)" && { printf %s "$branch"; return; }
  for d in rebase-merge rebase-apply; do
    head_name="$(cd "$repo" 2>/dev/null && realpath -m "$(git rev-parse --git-path "$d/head-name" 2>/dev/null)")"
    [[ -f "$head_name" ]] || continue
    branch="$(<"$head_name")"
    printf %s "${branch#refs/heads/}"
    return
  done
  printf %s "(detached HEAD)"
}

# True for `git merge --ff-only <upstream>` / `git reset --hard <upstream>` where <upstream> is exactly the
# tracking branch of the checked-out branch.
syncs_with_upstream() {
  local repo="$1" branch="$2"; shift 2
  local upstream target="" mode=0
  upstream="$(git -C "$repo" rev-parse --abbrev-ref --symbolic-full-name "$branch@{upstream}" 2>/dev/null)" || return 1
  [[ -n "$upstream" ]] || return 1
  for a in "$@"; do
    case "$a" in
      --ff-only|--hard) mode=1 ;;
      -*) return 1 ;;
      *) [[ -n "$target" ]] && return 1; target="$a" ;;
    esac
  done
  (( mode )) && [[ "$target" == "$upstream" ]]
}
is_branch() { git -C "$repo" show-ref --verify --quiet "refs/heads/$1"; }

# Stands in for quoted text whose value only the shell knows.
QUOTED=$'\x1f'
UNRESOLVABLE_RE='[$`*?[{]'

# Prints where cd $2 from $1 lands, or fails when only the shell could tell.
# A failed cd leaves the shell where it was, so with $3 set $2 must exist.
resolve_dir() {
  local base="$1" arg="$2" must_exist="${3:-}"
  [[ "$arg" == *"$QUOTED"* || "$arg" =~ $UNRESOLVABLE_RE ]] && return 1
  case "$arg" in
    "~"|"~/"*) arg="$HOME${arg:1}" ;;
    "~"*) return 1 ;;
    /*) ;;
    *) [[ -n "$base" ]] || return 1; arg="$base/$arg" ;;
  esac
  arg="$(realpath -m "$arg")"
  [[ -z "$must_exist" || -d "$arg" ]] || return 1
  printf '%s' "$arg"
}

# Moves $dir the way cd, pushd or popd would; an empty $dir means lost track.
follow_cd() {
  local verb="$1" to=""
  shift
  while [[ "${1:-}" =~ ^-[LPe@n]+$ ]]; do shift; done
  [[ "${1:-}" == -- ]] && shift
  case "$verb" in
    popd)
      if (( $# == 0 && ${#stack[@]} )); then to="${stack[-1]}"; unset 'stack[-1]'; fi
      ;;
    *)
      if (( $# == 0 )); then
        [[ "$verb" == cd ]] && to="$HOME"
      elif [[ "$verb" == cd && "$1" == - ]]; then
        to="$prev"
      elif [[ ! "$1" =~ ^[+-][0-9]+$ ]]; then
        to="$(resolve_dir "$dir" "$1" 1)"
      fi
      [[ "$verb" == pushd ]] && stack+=("$dir")
      ;;
  esac
  prev="$dir"
  dir="$to"
}

# Redirections are not arguments: "git merge --ff-only origin/main 2>&1" would otherwise look like a merge
# with two targets to every rule that inspects the argument list.
strip_redirections() {
  local a skip_operand=0
  args=()
  for a in "$@"; do
    if (( skip_operand )); then skip_operand=0; continue; fi
    case "$a" in
      [0-9]*'>'*|'>'*|'<'*)
        [[ "$a" =~ (\>|\<)$ ]] && skip_operand=1
        continue
        ;;
    esac
    args+=("$a")
  done
}

# The gate runs in a process group of its own, so stopping it stops the make and docker it started,
# then takes its stack down: a gate killed from outside would otherwise leave its containers running.
stop_gate() {
  [[ -n "$gate_pid" ]] || return 0
  kill -TERM -- "-$gate_pid" 2>/dev/null || kill -TERM "$gate_pid" 2>/dev/null
  sleep 2
  kill -KILL -- "-$gate_pid" 2>/dev/null
  [[ -n "$gate_repo" ]] && (cd "$gate_repo" && make stack-down) >/dev/null 2>&1
  return 0
}

# What a push changes against where the branch left main; a base that cannot be found gives no files,
# which runs every layer.
changed_files() {
  local repo="$1" base
  base="$(git -C "$repo" merge-base origin/main HEAD 2>/dev/null)" || return 0
  git -C "$repo" diff --name-only "$base"..HEAD 2>/dev/null
}

run_tests() {
  local repo="$1" log start rc mode limit plan layers skipped flag watchdog
  log="$(mktemp)"
  flag="$(mktemp -u)"
  start=$SECONDS
  mode="${EXCITER_GATE:-quick}"
  [[ "$mode" == full ]] || mode=quick
  plan="$(changed_files "$repo" | "$HOOK_DIR/gate-layers.sh" "$mode")"
  layers="$(sed -n 's/^run: //p' <<< "$plan")"
  skipped="$(sed -n 's/^skipped: //p' <<< "$plan")"
  # Below the hook's own timeout, which kills it silently and lets the push through: a gate that has
  # not finished by then did not pass.
  limit="${EXCITER_GATE_LIMIT:-$([[ "$mode" == full ]] && echo 1700 || echo 1200)}"
  # A gate killed at the hook's timeout writes no closing line, so its start gets a line of its own.
  gate_log=$'\t'"gate=$repo"
  decision=gating log_line
  # Waiting on a background job, unlike a foreground one, lets the TERM trap fire mid-gate.
  gate_repo="$repo"
  # shellcheck disable=SC2086 # the layers are words
  (cd "$repo" && exec setsid ./run-tests.sh $layers) > "$log" 2>&1 &
  gate_pid=$!
  (
    end=$((SECONDS + limit))
    while kill -0 "$gate_pid" 2>/dev/null; do
      if (( SECONDS >= end )); then : > "$flag"; stop_gate; break; fi
      sleep 2
    done
  ) &
  watchdog=$!
  wait "$gate_pid"
  rc=$?
  kill "$watchdog" 2>/dev/null
  wait "$watchdog" 2>/dev/null
  gate_pid=""
  gate_log+=$'\t'"gate_secs=$((SECONDS - start))"$'\t'"gate_rc=$rc"
  if [[ -e "$flag" ]]; then
    rm -f "$flag" "$log"
    gate_log+=$'\t'"gate_timeout=$limit"
    deny "Push blocked: the gate did not finish within $((limit / 60)) minutes, so nothing is known about this push. Run the layers yourself (./run-tests.sh $layers), or push with EXCITER_GATE_LIMIT=<seconds> set higher; with the machine busy, ask the user to push with '! git push', which skips the gate, and let CI judge."
  fi
  if (( rc != 0 )); then
    local tail_out
    tail_out="$(tail -n 60 "$log")"
    rm -f "$log"
    deny "Push blocked: ./run-tests.sh $layers failed. Fix the failures, commit, and push again.
$tail_out"
  fi
  rm -f "$log"
  # Said aloud, so nobody believes a shorter gate was the whole suite.
  gate_notes+=("Gate passed: $layers. Skipped: $skipped.")
}

case "$tool" in
  Edit|Write|NotebookEdit)
    path="$(jq -r '.tool_input.file_path // .tool_input.notebook_path // empty' <<< "$input")"
    [[ -n "$path" ]] || { reason="no path"; exit 0; }
    path="$(realpath -m "$path")"
    # Files outside this repo (scratchpad, memory) are not project work.
    repo="$(checkout_for "$(dirname "$path")")" || { reason="outside repo"; exit 0; }
    branch="$(current_branch "$repo")"
    valid "$branch" || deny "Refusing to edit $path on branch '$branch'. $HINT"
    ;;

  Bash)
    cmd="$(jq -r '.tool_input.command // empty' <<< "$input")"
    [[ -n "$cmd" ]] || { reason="no command"; exit 0; }
    # A cd earlier in the command decides where later segments run, so a cwd outside this repo does
    # not settle the matter on its own.
    dir="$cwd" prev="" lost="" stack=() gates=()
    if repo="$(checkout_for "$cwd")"; then branch="$(current_branch "$repo")"; else repo="" branch=""; fi

    # Heredoc bodies and quoted strings are data (commit messages, issue bodies), not commands: blank them
    # before splitting into segments. A quoted plain word such as a path keeps its value so cd can follow it.
    segments="$(perl -0pe '
      s/(<<-?\s*([\x27"]?)(\w+)\2[^\n]*\n).*?^\s*\3[ \t]*$/$1/gms;
      sub plain { my $s = shift; $s =~ m{\A[\w.\/\@%+=:,-]+\z} ? $s : "\x1f" }
      s{"((?:[^"\\]|\\.)*)"}{plain($1)}gse;
      s{\x27([^\x27]*)\x27}{plain($1)}gse;
      s/\s*(?:&&|\|\||;|\||\n)\s*/\n/g;
    ' <<< "$cmd")"

    # Walk the segments in order so "git switch -c fix/1-x && git commit" is judged against the branch
    # the commit will actually land on.
    while IFS= read -r seg; do
      read -ra t <<< "$seg"
      # Strip env assignments and wrappers (time git push, env -i git commit, ...).
      k=0
      while [[ "${t[k]:-}" =~ ^[A-Za-z_][A-Za-z0-9_]*= ]] \
        || [[ "${t[k]:-}" =~ ^(time|command|exec|nohup|nice|stdbuf|env|sudo|doas)$ ]]; do
        [[ "${t[k]:-}" == env ]] && while [[ "${t[k+1]:-}" == -* ]]; do ((k++)); done
        ((k++))
      done
      t=("${t[@]:k}")
      (( ${#t[@]} )) || continue

      case "${t[0]}" in
        cd|pushd|popd)
          strip_redirections "${t[@]:1}"
          follow_cd "${t[0]}" "${args[@]}"
          [[ -n "$dir" ]] || lost="${t[*]}"
          continue
          ;;
        gh)
          # Outside this repo a merge is none of our business, but a cd the guard lost track of may
          # have led back into it.
          if [[ "${t[1]:-} ${t[2]:-}" == "pr merge" ]] && { [[ -z "$dir" ]] || checkout_for "$dir" >/dev/null; }; then
            ask "Merging a PR is the user's decision."
          fi
          continue
          ;;
        git) ;;
        *) continue ;;
      esac

      target="$dir"
      i=1
      while [[ "${t[i]:-}" == -* ]]; do
        if [[ "${t[i]}" == -C ]]; then
          target="$(resolve_dir "$target" "${t[i+1]:-.}")" || lost="git -C ${t[i+1]:-}"
        fi
        [[ "${t[i]}" == -C || "${t[i]}" == -c ]] && ((i++))
        ((i++))
      done
      sub="${t[i]:-}"
      [[ "$sub" =~ ^[a-z][a-z-]*$ ]] && subs+=("$sub")
      strip_redirections "${t[@]:i+1}"
      if [[ -z "$target" ]]; then
        case "$sub" in
          add|mv|rm|restore|apply|commit|merge|rebase|cherry-pick|revert|reset|am|push)
            deny "Refusing 'git $sub' after '${lost//$QUOTED/\"...\"}': the guard cannot tell which checkout that leads to. Run git from the checkout itself without cd, or name it with git -C <absolute path>."
            ;;
        esac
        continue
      fi
      # -C into another checkout is judged by that checkout's branch. Worktrees live under .claude/worktrees
      # inside the repo directory, so the path alone does not say which checkout it belongs to: ask git.
      target_repo="$(checkout_for "$target")" || continue
      if [[ "$target_repo" != "$repo" ]]; then
        repo="$target_repo"
        branch="$(current_branch "$repo")"
      fi

      case "$sub" in
        checkout|switch)
          created=0
          for ((j = 0; j < ${#args[@]}; j++)); do
            # A short-flag cluster counts too: -qc, -qb, --quiet -c, ...
            if [[ "${args[j]}" =~ ^-[a-zA-Z]*[bBcC]$ ]]; then
              new="${args[j+1]:-}"
              valid "$new" || deny "Branch name '$new' is not allowed. $HINT"
              branch="$new"
              created=1
            fi
          done
          if (( !created )); then
            # The branch is the first argument that names one, so a flag before it ("git checkout -q main")
            # does not hide it and leave the rest of the command judged against the old branch.
            for a in "${args[@]}"; do
              [[ "$a" == -* ]] && continue
              if is_branch "$a"; then branch="$a"; fi
              break
            done
          fi
          ;;
        branch)
          if [[ "${#args[@]}" -ge 1 && "${args[0]}" != -* ]]; then
            valid "${args[0]}" || deny "Branch name '${args[0]}' is not allowed. $HINT"
          fi
          ;;
        add|mv|rm|restore|apply|commit|merge|rebase|cherry-pick|revert|reset|am)
          # Catching up with the remote is not working on main, and refusing it leaves main stale; only a
          # fast-forward or a reset to exactly the branch's own upstream is allowed.
          if [[ "$sub" == merge || "$sub" == reset ]] && syncs_with_upstream "$repo" "$branch" "${args[@]}"; then
            continue
          fi
          valid "$branch" || deny "Refusing 'git $sub' on branch '$branch'. $HINT"
          ;;
        push)
          for a in "${args[@]}"; do
            [[ "$a" =~ (^|:|/)(main|master)$ || "$a" == --all || "$a" == --mirror ]] \
              && deny "Pushing to main is not allowed; push the issue branch and open a PR instead."
          done
          valid "$branch" || deny "Refusing to push from branch '$branch'. $HINT"
          [[ " ${gates[*]} " == *" $repo "* ]] || gates+=("$repo")
          ;;
      esac
    done <<< "$segments"

    for g in "${gates[@]}"; do run_tests "$g"; done
    if (( ${#gate_notes[@]} )); then
      jq -n --arg m "${gate_notes[*]}" '{systemMessage: $m}'
    fi
    ;;
esac
exit 0
