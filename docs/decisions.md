# Decisions

Newest first. Each entry says what was decided and why, so a later change can
tell whether the reason still holds.

## 2026-10-14: Review and control, agreed before it is built (#32)

`docs/api-contract-m3.md` settles #33 to #38 before any of them is built, as
M1 and M2 were.

- **A review signs off a revision that is.** Stator's page review approves a
  draft and publishes it; an Exciter revision is saved already, so a review
  pins the current one, an approval counts only while it is still current,
  and a new revision supersedes what was pending. Approval rules borrow
  Stator's shape: people or groups, a count, per type.
- **Suspect is computed, never stored.** Links already remember the revision
  of each end they were confirmed against, and a type already says which end
  it watches (#18); a view compares, and clearing moves the remembered
  revisions on with a record of who, against what and why.
- **Baselines copy nothing.** An item names a revision, taken in one
  REPEATABLE READ snapshot, and never changes; a requirement's diff is #16's.
- **Coverage counts the status stored for everyone** (#27), so it never asks
  Armature as the viewer.
- **The migration ranges moved up by a hundred**, to 00500 and on, because
  the issues' 00400 to 00470 went to the one-installation work and M2.

## 2026-10-14: A removal Armature announces is a person's only for a wanted link (#29)

"Armature removal wins" (#1) needs to know who removed Exciter's link from an
issue, and Armature announces every removal alike, the worker's own included.
The worker takes a link off only when Exciter no longer wants it there: the
trace was removed, the requirement went to the trash or was purged, or the
link is broken or detached. So `issue.remote_link.removed` detaches a link
only while its requirement is live, the link neither broken nor detached,
and the sync record names the removed link's id. The worker's removal can be
announced before its own transaction forgets the record; the requirement's
state, committed before the worker ran, still tells the two apart.

## 2026-10-14: The tables are the installation's (#108)

The last of #91's three steps, as Armature's #218 took it: what was left of
the organization was names. `org` is `installation`, with the name and the
default theme and its id kept; `org_join_request` is `join_request`; a
membership is a role on the account; the identity provider and the Armature
connection are one row each; no table carries `org_id`, and
`current_org_id()` and `installation_org_id()` are gone.

- **A role on `app_user`, not a table.** `role`, `role_source` and
  `joined_at`, NULL for somebody who asked to join. Every rule of
  `org_member` holds on the columns: only an administrator gives or changes
  a role, never an owner, through the app role; the provider moves no
  owner; the last active administrator stays. What the keys to the
  membership did is a trigger now: a token, a grant or a requirement's owner
  names a member (`held_by_a_member`), and taking a role away, which only
  the admin role may, takes those along (`app_user_left`).
- **Deleting an account is no longer the app role's.** Any member could
  delete anybody's account straight through SQL, membership and all; the
  app role lost DELETE on `app_user`, which no code used.
- **One row is a unique index on `(true)`**, as Armature holds its
  installation: an upsert names it with `ON CONFLICT ((true))`. A group role
  needs the provider and an Armature token the connection, and removing
  either takes them, by trigger, as their keys did.
- **Files keep `org/<installation id>/`.** An attachment's key was a column
  generated from `org_id`; a trigger gives it the same key now, so every
  stored file still resolves, and a tombstone may name only the
  installation's files.
- **The way back loses little.** Down gives every role its membership row,
  dated when the role was given, the installation its slug `exciter`, and
  every table its `org_id`; when a membership last changed was not kept.

## 2026-10-14: The code stops carrying the organization (#107)

With the schema no longer asking (#106), the organization in the code was
only weight: `internal/tenant`, the `app.org_id` setting, `events.Event.OrgID`
and an `org_id` in every insert and filter. They went, as Armature's #218
removes them.

- **A transaction says only who acts.** `inTx` sets the acting person, and a
  token's projects, whenever the context has somebody; there is no
  organization to refuse a transaction without. What that refusal guarded is
  the permission policies' to guard, and a transaction acting for nobody
  reaches nothing walled, which a test shows through the service and one
  straight through SQL.
- **What was sealed with the organization's id still opens.** The provider's
  client secret, Armature tokens and the webhook secret are sealed with the
  installation's id, the same bytes as before, read with the connection or
  through `installation.ID`. Theme files keep their prefix for the same
  reason. Armature's cache keys lost the organization, since a token id is
  already unique; entries under the old keys age out.
- **The principal is the person and their role.** It carries the
  installation's name for the shell and the role, empty for somebody who no
  longer belongs, which the middleware refuses as switched off. Admitting a
  join request answers `{userId, role}`.
- **`current_org_id()` answers the installation** (00414) rather than a
  setting nobody sets any more, so what still calls it outside the code keeps
  working until #108 drops it.
- **Audit retention runs once,** not once per organization.
- **A unit test holds the line:** no product Go file names `org_id` or
  `current_org_id`. The tests' own SQL still names the column, which #108
  drops with the tables.

## 2026-10-14: Row security keeps the walls, not the organization (#106)

The tenant policies kept one organization apart from none, so they went: each
table's became one permissive policy for every role, `USING (true)`, and the
admin role's bypass went with it as redundant. Armature dropped row security
altogether at the same step; Exciter keeps it, because its walls are its 109
restrictive permission policies, all for `exciter_app`, and FORCE keeps them
on the table owner too.

- **The admin role passes through the open policy**, as every role does; the
  permission policies never name it. `BYPASSRLS` would need a superuser to
  grant at every install and would exempt the role from what a later wall
  might want of it. Where the admin role alone reads, the app role holds no
  privilege (`oidc_login`, sessions, identities) or a policy that is not open
  (`app_user`, members only).
- **What the tenant policies hid by accident is now said on purpose.** A
  connection acting for nobody reads the installation and its people, which
  are everyone's, and nothing walled. The app role may no longer make or
  remove an organization, nor read a sign-in in flight, which it never needed.
- **Keys are the installation's.** Every key, foreign key and index was made
  again without `org_id`, under its name or its name without the word org,
  from the catalog, with a check first that names rows that would share a new
  key; up then down gives back the same catalog.
- **Functions keep their rules.** 58 functions lost the organization from
  their filters, joins, locks and inserts; three that took it as an argument
  were made again without it. Rows they write take `org_id` from a default,
  `installation_org_id()`, moved here from #107 so no function has to name
  the column.
- **What still names it** until #108: `current_org_id()`, the organization's
  own seed, the session's column, the provider's and the Armature
  connection's one-row keys, the foreign keys to the organization, and an
  attachment's object key, which #108 settles. A test lists them and fails
  when one stops naming it.

## 2026-10-14: Integration tests keep apart in one installation (#90)

The integration suite gave every test an organization of its own, which the
schema cannot offer once `org_id` goes (#91). Now every test works in the
stack's installation and keeps apart by what it makes, as Armature's #217
decided.

- **One at a time, as before.** The suite never called `t.Parallel`, so a
  test meets another's leftovers rather than its changes. Keys carry a mark
  of the test's, people unique addresses, and the end of a test removes its
  projects, newest first since a moved requirement names the project it left,
  and then its people. A cleanup that fails now fails its test instead of
  leaving rows for the next one.
- **Shared settings are put back.** Tests that change the link types for all
  projects, the default theme, the Armature connection, the identity provider
  or the installation's name snapshot it and restore it; a list names each
  with its reason and a test holds the list to the code. A run leaves the
  installation as it found it.
- **What only two organizations proved went.** `TestTenantIsolation`, the
  cross-organization raw-SQL cases and the API's "another organization" cases
  were deleted. Where they also proved a wall inside an installation, the
  stranger became a guest no grant names, which the database refuses the same
  way. The cases that still hold while tables carry `org_id`, nothing visible
  without the installation and every way around the policies refused, stay in
  `tenant_context_test.go`.
- **Databases of their own** for what one installation cannot show: the
  upgrade guard, a first start, and the last active administrator, which the
  shared installation always has more than one of.
- **The test endpoints went.** `POST` and `DELETE /test/orgs`,
  `internal/testorg` and `EXCITER_TEST_ENDPOINTS` served the browser suite
  until #83 and nothing since; they went, and with them the sign-in seam
  `Server.SignInOrg` of #85.

**Reconsider if** the suite grows slow enough to want `t.Parallel`: the
list of installation-wide tests is then the list of those that run alone.

## 2026-10-14: One installation is one team's requirements (#89)

Exciter is self-hosted, and an installation serves one team, as Armature and
Stator decided for theirs. Organizations were a layer such an installation
never used: naming one at sign-in, switching between them, a provider and a
slug each, an owner rule per organization, and one copy per organization of
settings there is really one of. They are gone from everything people and the
API see (#82): the first start makes the installation, a sign-in lands in it,
letting somebody go switches them off, the Armature connection is an address,
and the product says "everyone", "all projects" or "Exciter". People who must
not see each other's work run an installation each.

Several organizations in one database are refused at the upgrade rather than
merged (#84). Their project keys, statuses, workflows, link types, roles and
people would collide, every rule for which one wins would be wrong for
somebody, and a migration would be choosing for them with nobody watching.
Refusing leaves the choice with the operator, and the refusal names the
statement that deletes the others.

Inside, the product went first and the schema follows. The one organization
row still holds the installation's name and default theme, the tables still
carry `org_id`, and forced row level security on `current_org_id()` still
scopes every query, now to the one organization there is. That kept every
step shippable with all four test layers green. The integration suite keeps
its tests apart by organization until it isolates them another way (#90), and
the columns and tenant policies go with the schema ticket (#91); the
restrictive permission policies, which are Exciter's guards, stay.

This supersedes what the older entries say about organizations as part of
the product. An entry that calls labels, statuses, workflows, link types or
themes "the organization's", or takes a lock per organization, was written
while that was the installation's name, and its argument stands.

**Reconsider if** one installation routinely needs a group inside it that the
others must not see at all: that is a wall around people and settings, the
way a project is a wall around work, not organizations coming back.

## 2026-10-14: The screen says everyone, all projects or Exciter (#88)

An installation is one team's, so a setting that reaches everybody is "for
everyone", a link type that every project may use is "for all projects", and
a refusal reads "Only an administrator ...". Where the subject is the
installation itself, the copy names Exciter, as Armature's #215 names
Armature.

- **The theme source is `everyone`.** `GET /themes/active` answered
  `organization` for the default; nothing stores it, so it changed without a
  migration.
- **Checked, not remembered.** A vitest check walks both catalogues, calling
  every function with sample arguments, and refuses "organization" and
  "Organisation"; a Go unit test does the same for the API document's
  summaries, descriptions, tags and enums and the assistant's tool help, after
  Armature's.
- **What still says it.** Keys such as `ofOrg` and `data-*` hooks keep their
  names, as do the test-only endpoints, which #90 removes, and the code's own
  names, which #91 removes with the tenant layer.

## 2026-10-14: The Armature connection is an address (#87)

Armature decided, as Exciter did, that an installation is one team's tracker,
so the slug of an Armature organization no longer says anything the address
does not. The connection is its base URL alone: `armature_connection.org_slug`
went in 00403, and with it the form's field, the input and the slug in
`armature_endpoint()`. A new address is what forgets every member's token.

- **The learned organization stays the check.** Armature at 7920a98 still
  answers `principal.org` on `/auth/me`, so the first token that checks out
  teaches its id and every later token must match it, which keeps a token of
  some other Armature behind the same address from being stored.
- **A token that names no organization is let through.** Armature's #218
  drops the organization from its schema; once `/auth/me` stops reporting one,
  the address is all there is to go by, and refusing every token then would
  break the connection on Armature's upgrade.
- **The webhook is `POST /armature/webhook`.** Still pending until #29; the
  secret and the replay keys stay with the one installation row until #91.
- **The stub keeps its tenants.** They are a test device, picked by the token,
  so parallel tests stay apart; nothing in the product names one.
- **The M2 ranges moved** to 00420, 00430 and 00440, after the epic's 00400 to
  00419.

## 2026-10-13: Letting somebody go switches them off, and an active administrator stays (#86)

People belong to the installation, so there is no membership to take away
that leaves an account standing elsewhere. `DELETE /users/{userID}` keeps its
address and switches the account off, as Armature's #211 does: sessions end
and tokens are deleted at once, the account, its role, its groups and its work
stay, and `PATCH /users/{userID}` with `active: true` brings the person back.
A switched-off sign-in hears "Your account has been switched off. Ask an
administrator.", the same as somebody with no membership; the separate
`account_inactive` answer went.

- **The last owner rule became the last active administrator rule.** An
  installation is entered again through any administrator, owner or admin,
  so that is what the database keeps, on a role change, a membership going
  and an account switched off. The check takes an advisory lock, so two
  administrators switching each other off at once take turns and the second
  is refused. The service refuses the same first, with `last_administrator`.
- **Owners stay owners.** The bootstrap makes them through the admin role,
  and the app role neither makes nor unmakes one (a trigger, since a policy
  that hid owner rows would also hide them from `SELECT ... FOR UPDATE`).
  Only an owner switches another owner off, a rule of the service, as in
  Armature.
- **Nobody leaves through the app role.** It may no longer delete a
  membership; only the admin role, for tidying up, does.
- **Tokens are the person's.** Administrators see and revoke everybody's at
  `/tokens/everyone`, on the page `/settings/all-tokens`, which replace
  `/org/tokens` and `/settings/organization-tokens` without a redirect, as
  nothing shipped with them.

## 2026-10-13: A sign-in lands in the installation, and keeps where it opened (#85)

Nothing switches a session any more: the sign-in page asks for no
organization, single sign-on starts at `/auth/oidc/start` with the
installation's provider, and `POST /auth/switch-org`, the switchers and the
organizations on `/auth/me` went. With them went what decided where a session
might go: `session_reaches`, its widening to providers of the same issuer and
to organizations the person owns, `session_stays_home` and the organization a
proof was made for.

- **The session keeps the organization it opened in.** Armature resolves a
  session to the person's first membership. Exciter's integration suite lets
  the same Keycloak people into organizations of its own beside the seeded
  installation, so the oldest membership would land a test's sign-in in the
  wrong place. `current_org_id` is written once when the session opens and a
  trigger, `session_keeps_its_opening`, refuses moving it; #91 drops it.
- **A seam for the suite, until #90.** `Server.SignInOrg` names whose provider
  single sign-on starts with; nil, as everywhere but the integration suite,
  is the installation's, the oldest organization, as the seed finds it.
- **No redirect.** Armature redirects its old address for a release; no
  release of Exciter shipped with one, so it is simply gone.

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
test endpoints went with #90.

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
