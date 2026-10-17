# Working on Exciter

The rules every contributor follows are in `CONTRIBUTING.md`, imported here;
this file adds what is for an agent.

@CONTRIBUTING.md

## Before changing anything

Read `.claude/skills/gh/SKILL.md`. Branches are created with
`gh issue develop`; agree on issues and pull requests with the user before
creating them, and the user merges. `.claude/hooks/branch-guard.sh` enforces
the branch rules and, before every push, runs the layers of `./run-tests.sh`
that the pushed files can affect: format, Go, web and the shell tests by
default, and the stack, integration and browser layers with
`EXCITER_GATE=full`. CI runs every layer and blocks the merge, so a quick
local gate says what it skipped. A gate that does not finish blocks the push.

## Armature and Stator

Local checkouts sit beside this one, as `~/repos/Armature`, and Stator from
`praetorianer777/Stator`; read how they do a thing before doing it here.

## Toolchain

Go is pinned to 1.26, as in Armature and Stator: under 1.27 the maintainer's
machine kills every test binary about a second into its run.
