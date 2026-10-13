# Decisions

Newest first. Each entry says what was decided and why, so a later change can
tell whether the reason still holds.

## 2026-10-13: One installation holds one organization, and a database with several stops (#84)

Exciter is self-hosted, and an installation serves one team, as Armature and
Stator have decided (#82); more than one team takes more than one
installation. The organization row stays inside for now, so `org_id` and row
security keep working, and `internal/installation` is the one way to its name
and default theme; moving them off `org` changes that package and nothing
that calls it. The seed makes the one organization on a first start, called
`EXCITER_NAME`, under the fixed address `installation.Slug`, and finds it on
every later start whatever it was renamed to.

- **Several are refused, not merged.** Their project keys, statuses,
  workflows and people would collide, and no rule for which one wins would be
  right for everybody. Migration `00400` counts the organizations and, with
  more than one, stops, naming the first ten. No release of Exciter could
  delete an organization, so the refusal names the statement that does. It
  lifts forced row security on `org` to count, so an owner that is not a
  superuser sees the rows too.
- **Everybody signed in belongs.** Somebody signed in without an
  organization saw an empty shell, and every route had to ask whether there
  was a tenant at all. A password sign-in of somebody with no membership is
  refused with "Your account has been switched off. Ask an administrator.",
  and an open session whose membership went ends rather than being refused,
  so the browser goes back to sign in and hears it there. `requireOrg` and
  the `no_organization` error went with the case they covered.

**Reconsider if** somebody with several organizations asks for a way across
better than deleting: an export of one organization's projects into another
installation is the shape that would take.

## 2026-10-13: Browser specs keep to people and keys of their own (#83)

An installation serves one team (#82), so the browser suite stops making an
organization per spec file, as Armature's did (Cloudster1/Armature#209).
Supersedes "Each browser spec file gets a throwaway organization (#9)".

- **People of their own.** Each spec file gets, per worker, an alice who
  administers and a bob who is a member, made in `demo` by the bootstrap owner
  through `POST /users`, signed in with their password and let go when the
  worker stops (`e2e/fixtures/people.ts`). What they choose, the tokens they
  make and what they are granted is nobody else's.
- **Keys and words of their own.** A spec's projects carry keys no other worker
  or run draws (`uniqueKey`), and what every project shares, a label or a link
  type, gets a word of the worker's own. Search is narrowed to the spec's
  project, and the audit log to the target the spec planted.
- **The built-in look, chosen.** The default theme is everybody's. Each spec's
  people choose the built-in theme when they are made, so a default the
  themes spec sets meanwhile never reaches another spec's contrast check.
- **A lane of one worker for what there is one of.** The provider, the default
  theme and the Armature connection are the installation's, so the sso,
  themes, armature and armature-links specs run in a Playwright project of one
  worker, beside the rest rather than after them: Playwright runs the
  projects another depends on whole in every shard, which would have run the
  whole suite twice in CI. Demo's own alice is only the sso spec's now, which
  changes her role and points demo's provider at a dead issuer, and restores
  both.

## 2026-10-13: One remote link per requirement and issue, at its permalink (#25)

Stator puts one remote link per page and issue at the page's address. A
requirement may be linked to the same issue with two relations, say implements
and verifies, and Armature keys remote links by url, so a second link at the
same url would retitle the first. One remote link stands for the pair, titled
with the first relation, and its url is the requirement's permalink
`/r/{id}`, which survives a move to another project and a new project key
where `/p/{key}/r/{key}` would not. Until Armature names a remote link's kind,
requirements appear in its Pages panel told apart by their source alone; the
contract lists that as something Exciter would like, not something it needs.
**Reconsider if** Armature gives remote links a relation of their own.

## 2026-10-13: Project templates live in code (#24)

The issue named a `project_template` table, but the three templates are part
of the product, not of an organization, and a table would only hold a copy of
the code that writes them. They are in `internal/project/templates.go`, and a
project keeps only the key it began from. A template is applied once, in the
transaction that makes the project, so changing one never changes a project
made earlier. The requirement package makes the sections through a seeder the
project service is given, since project cannot import it.
**Reconsider if** organizations want templates of their own, which would
belong in a table.

## 2026-10-13: A key of another project names a row of the file (#23)

The contract said an import refuses a key of another project. But a
project's export carries its own keys, and importing it into an empty
project, which the integration suite proves, needs those keys to say which
row is a parent of which and where each link goes. So a key of another
project names its row within the file and the row makes a new requirement,
while a key of the project itself still changes that requirement, and one of
the project that is not there is still refused. The status of a new
requirement is the one other thing an import may set that no save does; a
database function allows it only for a requirement made in the same
transaction, by somebody who administers its project.
**Reconsider if** people mean to update another project's requirements by
importing into this one.

## 2026-10-13: The database holds a token to its projects (#22)

A token limited to projects could be held there by the service alone, as
Armature does, but every query that forgot to ask would then reach further.
As Stator does for spaces, the transaction carries the token's projects in
`app.token_projects`, and `perm_project_holds`, which every project policy
calls, refuses any other project; `current_actor_administers` refuses the
organization as a whole. The limit is the caller's alone, so a rule asked
about somebody else still answers for them. The API refuses the operations
marked `orgWide` first, with a sentence that says why, by matching the
request against its own router rather than listing paths twice.
**Reconsider if** a project policy stops going through `perm_project_holds`.

## 2026-10-13: A file is kept by its own requirement's revisions (#21)

A revision is never changed, so a file it shows has to stay for as long as the
revision can be read, which is as long as its requirement exists. Deleting
such a file takes it off the list and keeps the row and the bytes; a trigger
refuses deleting the row while a revision of the requirement names it, and a
purge takes the requirement, its revisions and its files together. Only the
file's own requirement's revisions count: a copy points its revision 1 at
files of its own, and a move keeps the requirement and with it the object
key. A save does not check that a picture belongs to the requirement, as
Stator's does not, so a picture pasted from another requirement shows while
that file lasts but keeps nothing.
**Reconsider if** people paste pictures between requirements and lose them.

## 2026-10-13: The database walks every link for a circle (#18)

A link shows only to whoever views both its ends, so a circle can run through
a requirement the writer cannot see. A check that walks only what the writer
sees would let two people, each blind to the other's project, close a circle
of a type that allows none. The trigger on `trace_link` therefore runs as the
owner and walks every live link of the type, under a lock per organization so
two links cannot close one circle together. The service walks first over what
the caller sees, to name the way round; when only the trigger finds the
circle, the refusal names no requirement on it. Whether a project uses a type
and which pairs it allows stay in the service: they are a project's
preference, not something history or coverage relies on.
**Reconsider if** link writes queue behind the organization's lock.

## 2026-10-13: Search judges rights a project at a time (#20)

Row level security runs a restrictive policy on every row a query reads before
a non-leakproof operator such as `@@` may filter, so a search through the
policies alone would check the reader's rights on every requirement of the
organization, however few matched. Stator met the same cost and answered it
with `page_search_rows`; Exciter does the same with `requirement_search_rows`,
a `SECURITY DEFINER` function that finds the words through the index and then
asks `perm_project_holds` once per project the hits lie in. It applies what
the tenant and viewer policies apply, out of the trash, and the integration
suite proves through raw SQL that it returns nothing more.
**Reconsider if** a requirement gains rights of its own beyond its project's.

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

Superseded by "Browser specs keep to people and keys of their own (#83)"; the
test endpoints stay for the integration suite until #90.

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
