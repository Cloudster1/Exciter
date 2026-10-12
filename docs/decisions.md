# Decisions

Newest first. Each entry says what was decided and why, so a later change can
tell whether the reason still holds.

## 2026-10-12: The database takes a transition whole (#17)

A reviewer approves without holding edit, so the app role may neither write
the requirement's change log nor its owner for them. Rather than loosen those
policies for approvers, `requirement_transition` writes the status, the log
entry and the owner its post-functions set, reading the post-functions from
the transition's own rules. The service still runs the conditions and
validators first, for the sentences a refusal needs, but what a transition
does to the row is decided where nobody can ask for something else.
**Reconsider if** a post-function needs more than the owner, such as a value
an approver may not write.

## 2026-10-12: A project key can change, and the old one keeps resolving (#10)

Neither Armature nor Stator lets a project or space key change. Exciter does,
because a specification often outlives the name it began with, and every
requirement key carries the project key. A changed key stays behind in
`project_key_alias`, so `CHK-R5` still finds `SYS-R5` in links pasted into
mail, Armature issues and Stator pages. A key is taken while any project has
it now or had it before, so an old key never points at two projects. A move to
another project keeps each requirement's old key in `requirement_old_key`, as
Armature's `issue_old_key` does.
**Reconsider if** aliases pile up enough that freeing old keys matters.

## 2026-10-12: Statuses and workflows belong to a project (#10)

Armature's statuses and workflows are the organization's, assigned through
schemes. Exciter keeps them per project, with no schemes: the project's
administrators own the whole of their process, and templates can set it up
without touching anybody else's. Questions across projects key on the five
status categories, which are fixed, never on status names.

## 2026-10-12: Project roles add up, and reviewers approve without editing (#10)

A grant gives one role per subject in a project, and a person holds the union
of every grant that reaches them. Reviewer and editor are separate roles
rather than steps on one ladder, so a reviewer can approve without being able
to change what they approve, and somebody who should do both holds both. A
reviewer takes only the transitions whose conditions name the reviewer role,
which is how the default workflow's Approve works.

## 2026-10-12: Sections are requirements of their own kind (#10)

A section is a row of `requirement` with `kind = 'section'`: a title, an
optional introduction, a key and revisions, and no type, status, attributes or
links. One table keeps one tree, one rank, one trash, one permalink and one
cycle guard. The price is that sections use up requirement numbers, which only
need to be unique, not dense in meaning.

## 2026-10-12: Retire rather than delete what history names (#10)

Types, attributes, options, statuses and link types that a requirement, a
revision or a link ever used are retired: they stay readable where they were
used and cannot be chosen again. Deleting is only for what nothing ever used.
A revision must read tomorrow as it read the day it was written, which a
deleted option or type would break.

## 2026-10-12: An import runs in the worker (#10)

Armature imports a CSV file within the request. Exciter's imports build a tree
and links in a second pass over files of up to 5000 rows, which can outlast a
request. So an upload only previews and checks. Starting hands the import to
the worker through the outbox, and the client polls its progress. Every
problem the file has is reported before anything is written, so a started
import refuses rows only when the project changed meanwhile.

## 2026-10-12: Each browser spec file gets a throwaway organization (#9)

As in Stator: specs that change what an organization shows, such as its
default theme or somebody's role, cannot share `demo` with specs that judge
the built-in look, and running them one after another only hides that. The
api has two test endpoints, `POST /api/v1/test/orgs` and
`DELETE /api/v1/test/orgs/{slug}`, and `e2e/fixtures/org.ts` makes one
organization per spec file and worker through them, signs alice and bob in to
it and deletes it when the worker stops.

- **Made like `demo`.** The organization gets the seed's people and provider,
  from the same `EXCITER_BOOTSTRAP_*` settings and `seed.Populate`, so it
  signs in exactly as `demo` does. Exciter lets only an administrator
  configure a provider, so the bootstrap administrator is made its owner
  first and the provider is configured in their name, as the seed does.
- **Compiled in, routed only when asked.** The image the suite runs is the
  image that ships, so the endpoints are in it, but routed only when
  `EXCITER_TEST_ENDPOINTS` is on; otherwise they answer 404. Every call
  carries `EXCITER_TEST_ENDPOINTS_TOKEN` in `X-Exciter-Test-Token`, so an
  endpoint switched on by accident is still closed, and the api refuses to
  start with them on in production. Deleting refuses any organization the
  endpoint did not make, which it marks in `org.settings`.
- **Not in the document.** They are left out of `api/openapi.json`: the
  document is what clients are generated from, and the web client would
  otherwise carry types for calls it must never make. A table of their own in
  `internal/httpapi/test_handlers.go` describes them, and a unit test holds
  the router to both tables and the document to the public one alone.

## 2026-10-12: The logo is a resized picture and the icons are a crop of it (#56)

Exciter's mark was a hand-drawn E on the accent. The picture now given is a
detailed illustration with small text, clear at a few hundred pixels and a
smudge at sixteen. It is handled as Stator handles its own:

- **Two kinds of image from one source.** The whole picture, resized to 128,
  256 and 512 pixels in WebP, is the logo, with `srcset` so a screen takes the
  one it needs. The favicon, touch icon, manifest icons and the sidebar's mark
  are a square cut of the gopher's face, which is what still reads small.
- **Made by a target, from a container.** `make branding` runs ImageMagick in
  an Alpine image and writes `web/public`, so nothing is installed on the host
  and `assets/branding/logo.jpg` stays the one thing to replace. The made
  files are committed, so a build needs no ImageMagick.
- **Served from the site's own origin.** `img-src 'self'` already lets them
  load, and nginx already names the manifest's type.

## 2026-10-12: Requirement keys are `CHK-R42` (#1)

A requirement's key is its project key, `-R` and a number per project:
`^([A-Z][A-Z0-9]{1,9})-R([1-9][0-9]*)$`. Armature's commit and mail scanners,
its query language and Stator's issue chips all look for a project key, a
hyphen and a digit, so none of them mistakes a requirement for an issue, and
the same project key can be used in Armature and Exciter without ambiguity.
Keys such as `SYS-12`, a type prefix and a number, are therefore not allowed.
A unit test feeds Exciter keys through copies of their patterns (#13).

## 2026-10-12: A revision is a normative save (#1)

A save writes an immutable revision when title, statement, rationale, type or
attributes change. Status, owner, labels and the place in the tree are logged
but never revisioned, so changing them neither voids an approval nor makes a
link suspect. Links are not part of a revision either: a revision of B for
every link to B would make everything below B suspect. Baselines capture the
link set instead. Editing an approved requirement is allowed; the new revision
supersedes the approval and the workflow takes the status back.

## 2026-10-12: The status category of a linked issue is stored for everyone (#1)

Stator shows Armature data only as the viewing person sees it. Coverage needs
to know whether an implementing issue is done for every reader, so Exciter
stores the status category of each linked issue, updated by Armature's
webhooks, and fetches summary and assignee live as the viewer.
**Reconsider if** Armature adds a way to answer status for an organization
without a person's token.

## 2026-10-12: Stack ports from 10000, and Armature removal wins (#1)

Each checkout's stack takes a slot of 20 ports from 10000 to 19999, derived
from its path. Stator uses 20000 to 29999 and the kernel hands out ephemeral
ports from 32768, so neither collides; slots holding 16686 (Armature's
Jaeger), 11211, 15672 or 10250 are skipped.

When someone removes Exciter's link from an issue in Armature, the trace link
is marked detached and not pushed again: the person's decision in Armature
wins over the sync.

## 2026-10-12: The tooling is Stator's (#1)

Exciter ports Stator's gate, hooks, release script and CI rather than
Armature's, because Stator's are the newer of the two and already split the
gate between a quick local run and CI. Stator is under Apache-2.0 since
`a5661a6`; `NOTICE` names the commit each ported file came from. A layer
joins the gate in the change that introduces it, so the gate is green from
the first commit.
