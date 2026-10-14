# Working on Exciter

Exciter is requirements management that works hand in hand with
[Armature](https://github.com/Cloudster1/Armature), the tracker, and
[Stator](https://github.com/praetorianer777/Stator), the wiki. It mirrors their
stack and patterns on purpose; when in doubt, look at how Stator does it (the
newer of the two), then Armature, and do the same. Local checkouts:
`~/repos/Armature`, and Stator from `praetorianer777/Stator`. See
`docs/architecture.md` and `docs/decisions.md`.

## Every change hangs off an issue

Read `.claude/skills/gh/SKILL.md` before changing anything. Branches are
`<type>/<issue>-<slug>`, created with `gh issue develop`; changes land via pull
request with `Closes #N`, and the user merges. `.claude/hooks/branch-guard.sh`
enforces this and, before every push, runs the layers of `./run-tests.sh` that
the pushed files can affect: format, Go, web and the shell tests by default,
and the stack, integration and browser layers with `EXCITER_GATE=full`. CI runs
every layer and blocks the merge, so a quick local gate says what it skipped.
A gate that does not finish blocks the push.

## Nothing is installed on this machine

Every toolchain runs in a container, driven by the `Makefile`. Never install
Go, Node, Chromium or a Postgres client on the host. If a tool is missing, add
a target that runs it in a container.

Go is pinned to 1.26, as in Armature and Stator: under 1.27 on this host every
test binary is killed about a second into its run.

## Testing

Four layers, all run by `./run-tests.sh`: Go unit tests, Go integration tests
against the compose stack (`-tags integration`), vitest, and Playwright, plus
the shell tests of the tooling itself. A layer joins the gate in the change
that introduces it, through a Makefile target of the same name. A feature is
finished when every layer passes, not when the code is written.

Every integration test shares the stack's one installation and the suite runs
one test at a time: make people with `h.owner` and `h.addPerson`, projects
under `projectKey`, and assert on what the test made, never on totals. A test
that changes something there is one of puts it back through a keeper and is
listed in `installationWide` (`docs/architecture.md`, Testing).

The API is described by `api/openapi.json`, derived from the route table.
After changing a route, a request type or a response type, run `make openapi`
and commit the regenerated document and `web/src/api/schema.d.ts`; a unit test
refuses a stale copy. An operation agreed before it is built carries
`pending: true` and answers 501; `docs/api-contract-m*.md` hold the agreements.

Nothing is mocked below the layer under test. A guard is not proven by the
service refusing: try the same thing straight through SQL as `exciter_app` and
watch the database refuse it too. The only stand-ins are `armature-stub` and
`stator-stub`, kept honest by contract tests against the vendored
`api/armature/openapi.json` and `api/stator/openapi.json`.

## Style

- Comments say why, never what. Doc comments at most two lines.
- No en dashes, em dashes, typographic quotes or emoji, in code or in prose.
- Named constants over magic numbers. Frontend ones live in `web/src/config.ts`.
- Errors the user reads are sentences, and name what to do about it.
- No other requirements-management product is named anywhere in the
  repository, issues, pull requests or commit messages.
- Code adapted from Armature or Stator keeps working like theirs; `NOTICE`
  credits it with the commit it came from.
