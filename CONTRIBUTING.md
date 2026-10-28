# Contributing to Exciter

Thank you for helping. Exciter is requirements management that works beside
[Armature](https://github.com/Cloudster1/Armature), the tracker, and
[Stator](https://github.com/praetorianer777/Stator), the wiki. It mirrors their
stack and patterns on purpose: when in doubt, look at how Stator does it, then
Armature, and do the same. [`docs/architecture.md`](docs/architecture.md)
describes how Exciter is built and [`docs/decisions.md`](docs/decisions.md)
why.

Report a vulnerability privately, as [`SECURITY.md`](SECURITY.md) says, never
in a public issue.

## Getting started

Every toolchain runs in a container, driven by the `Makefile`. The host needs
Docker, make, `jq` and `python3`; `gh` opens pull requests. Do not install Go,
Node, Chromium or a Postgres client on the host: if a tool is missing, add a
make target that runs it in a container.

```sh
make up                 # the stack, and the web address to open
make web-dev            # the web client with hot reload, against the running stack's api
./run-tests.sh          # the whole gate, as CI runs it
make help               # every target
```

The [README](README.md) lists the development accounts and the suite with
Armature and Stator.

## Every change hangs off an issue

1. Find or open an issue. Feature issues follow the template: user story,
   acceptance criteria, tests, implementation, depends on. Changes Exciter
   needs in Armature or Stator are issues there, linked from the Exciter
   issue, which is labelled `blocked` until they ship.
2. Work on a branch named `<type>/<issue>-<slug>`, lowercase ASCII, for
   example `feat/42-suspect-links`. The types are `feat fix chore docs
   refactor test perf ci build revert`.
3. Commit messages are [Conventional Commits](https://www.conventionalcommits.org/):
   lowercase, imperative, a subject of at most 72 characters that says what
   changes for the user rather than which files were touched.
4. Say what changes for the user in `CHANGELOG.md`, under Unreleased.
5. Open a pull request whose description carries `Closes #N`. CI runs every
   layer of the gate and must pass; a maintainer merges.

Releases are cut by the maintainers with `./release.sh`, as
[`docs/releasing.md`](docs/releasing.md) describes.

## Testing

`./run-tests.sh` runs every layer; `./run-tests.sh --list` names them:

- Go unit tests
- Go integration tests against the compose stack (`-tags integration`)
- vitest for the web client
- Playwright in the browser, with an accessibility check on every page
- the shell tests of the tooling itself

A feature is finished when every layer passes, not when the code is written.
The Go unit, Go integration and web layers also measure coverage, and fail
below the floors kept in `coverage/`. A floor only rises: when a change lifts
a total, raise its floor to the new figure, rounded down.
A new layer joins the gate in the change that introduces it, through a make
target of the same name.

Every integration test shares the stack's one installation and the suite runs
one test at a time. Make people with `h.owner` and `h.addPerson` and projects
under `projectKey`, and assert on what the test made, never on totals. A test
that changes something there is only one of puts it back through a keeper
and is listed in `installationWide`
([`docs/architecture.md`](docs/architecture.md), Testing).

Nothing is mocked below the layer under test. A guard is not proven by the
service refusing: try the same thing straight through SQL as `exciter_app` and
watch the database refuse it too. The only stand-ins are `armature-stub` and
`stator-stub`, kept honest by contract tests against the vendored
`api/armature/openapi.json` and `api/stator/openapi.json`.

## The API

`api/openapi.json` is derived from the route table. After changing a route, a
request type or a response type, run `make openapi` and commit the regenerated
document and `web/src/api/schema.d.ts`; a unit test refuses a stale copy. An
operation agreed before it is built carries `pending: true` and answers 501;
`docs/api-contract-m*.md` hold the agreements.

## Style

- Comments say why, never what. Doc comments are at most two lines.
- No en dashes, em dashes, typographic quotes or emoji, in code or in prose.
- Named constants over magic numbers. Frontend ones live in `web/src/config.ts`.
- Errors the user reads are sentences, and say what to do about it.
- User-facing strings live in `web/src/i18n`; everything else, from code to
  issues, is written in English.
- Features are described on their own terms: no other requirements-management
  product is named anywhere in the repository, issues, pull requests or commit
  messages.
- Code adapted from Armature or Stator keeps working like theirs, and
  [`NOTICE`](NOTICE) credits it with the commit it came from.

## Working with Claude Code

The repository carries a [Claude Code](https://claude.com/claude-code) setup:
[`CLAUDE.md`](CLAUDE.md), `.claude/skills/gh` and the hooks in
`.claude/settings.json`. When the repository is opened in Claude Code, its
hooks refuse edits on a branch that is not named `<type>/<issue>-<slug>` and
run the layers of the gate a push can affect before every push. Nothing of it
runs outside Claude Code.
